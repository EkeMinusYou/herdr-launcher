// launcher は herdr の popup でコマンドや移動先のペインを選び、開いたときの cwd (--cwd) で実行する。
// --goto ではペインだけを herdr の goto と同じ操作で選ぶ
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type item struct {
	id      string // プレビューのキャッシュに使う
	kind    string // command / terminal / claude などのエージェント名
	status  string // herdr の agent_status。コマンドと terminal では空
	title   string // プレビューの枠のタイトル
	columns []column
	extra   string // 一覧には出さないが絞り込みの対象にする文字列 (ブランチやパス)
	group   string // goto で前後のワークスペースへ移動するための単位
	focused bool
	desc    string
	command string // プレビューに出す実行コマンド。移動先の候補では空
	run     func() error
	preview func() (string, error) // プレビューに出すペインの画面。nil なら出さない
}

// 一覧の 1 列。縦を揃えるため text は列幅まで空白で埋めておく
type column struct {
	text  string
	color lipgloss.Color
}

func (it item) label() string {
	var b strings.Builder
	for _, c := range it.columns {
		b.WriteString(c.text)
	}
	return b.String()
}

// 一覧に出す文字列から始めるので、一致した位置をそのまま一覧の強調に使える
func (it item) searchText() string { return it.label() + " " + it.extra }

const kindWidth = 8

// 表示幅 (全角は 2) で width まで空白を足す
func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

func kindColumn(kind string) column { return column{pad(kind, kindWidth) + "  ", kindColor(kind)} }

func newCommand(title, desc, command string, run func() error) item {
	return item{
		id:      "command:" + title,
		kind:    "command",
		title:   title,
		columns: []column{kindColumn("command"), {title, text}},
		desc:    desc,
		command: command,
		run:     run,
	}
}

var commands = []item{
	newCommand("lazygit", "Open lazygit in a popup.", "lazygit", func() error { return openPane("lazygit") }),
	newCommand("neovim", "Open nvim in a popup.", "nvim", func() error { return openPane("nvim") }),
	newCommand("gh browse", "Open the repository in the browser.", "gh browse", func() error { return runCommand("gh", "browse") }),
	newCommand("gh pr view --web", "Open the pull request for the current branch in the browser.", "gh pr view --web", func() error { return runCommand("gh", "pr", "view", "--web") }),
}

func main() {
	// ランチャーが閉じた後に afterClose から起動される
	if len(os.Args) == 4 && os.Args[1] == "open-pane" {
		if err := openPaneAfterClose(os.Args[2], os.Args[3]); err != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 5 && os.Args[1] == "focus-pane" {
		waitForClose()
		if err := focusPane(pane{ID: os.Args[2], TabID: os.Args[3], Agent: os.Args[4]}); err != nil {
			os.Exit(1)
		}
		return
	}

	// --goto は herdr の goto (prefix+g) の代わりにペインだけを選ぶ
	gotoMode := len(os.Args) == 2 && os.Args[1] == "--goto"
	var chosen *item
	var err error
	if gotoMode {
		chosen, err = pick(paneItems(), true)
	} else {
		chosen, err = pick(append(commands, paneItems()...), false)
	}
	if err == nil && chosen == nil {
		return
	}
	if err == nil {
		err = chosen.run()
	}
	if err != nil {
		// エラーを読めるようにしてから閉じる
		fmt.Fprintf(os.Stderr, "\n%v\npress enter to close", err)
		bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// popup は同時に 1 つしか開けず、overlay で開いた場合は閉じるときに開く前のペインへフォーカスが戻される。
// どちらでも効くよう、lazygit などを開いたりペインへ移動したりする処理はランチャーが閉じてから行う。
// 閉じると同じセッションのプロセスは止められるので、自分自身を新しいセッションで起動して任せる
func afterClose(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func openPane(entrypoint string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return afterClose("open-pane", entrypoint, cwd)
}

// overlay で開いた場合の HERDR_PANE_ID はランチャー自身のペイン。それが消えるまで待つ (popup では空)
func waitForClose() {
	self := os.Getenv("HERDR_PANE_ID")
	if self == "" {
		return
	}
	for range 60 {
		if herdr("pane", "get", self) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func openPaneAfterClose(entrypoint, cwd string) error {
	waitForClose()
	var err error
	// popup は同時に 1 つしか開けないため、開けなければ少し待って再試行する
	for range 20 {
		if err = herdr("plugin", "pane", "open", "--plugin", "launcher", "--entrypoint", entrypoint, "--cwd", cwd, "--focus"); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}
