package main

// herdr の CLI でペインの一覧を取得し、選んだペインへ移動する

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type workspace struct {
	ID    string `json:"workspace_id"`
	Label string `json:"label"`
}

type tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type pane struct {
	ID          string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
	Title       string `json:"terminal_title_stripped"`
	Cwd         string `json:"cwd"`
	Focused     bool   `json:"focused"`
}

type rect struct {
	X, Y, Width, Height int
}

func herdrBin() string {
	if bin := os.Getenv("HERDR_BIN_PATH"); bin != "" {
		return bin
	}
	return "herdr"
}

func herdr(args ...string) error {
	out, err := exec.Command(herdrBin(), args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("herdr %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// herdr の JSON 応答の result を out に読み込む
func herdrJSON(out any, args ...string) error {
	b, err := exec.Command(herdrBin(), args...).Output()
	if err != nil {
		return fmt.Errorf("herdr %s: %w", strings.Join(args, " "), err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	return json.Unmarshal(resp.Result, out)
}

// 現在のペインを移動先の候補にする。取得に失敗したときは候補に出さない。
// herdr はワークスペースの順にペインを返すので、その順のまま並べる
func paneItems() []item {
	var (
		ws struct {
			Workspaces []workspace `json:"workspaces"`
		}
		ts struct {
			Tabs []tab `json:"tabs"`
		}
		ps struct {
			Panes []pane `json:"panes"`
		}
		wg sync.WaitGroup
	)
	wg.Go(func() { herdrJSON(&ws, "workspace", "list") })
	wg.Go(func() { herdrJSON(&ts, "tab", "list") })
	wg.Go(func() { herdrJSON(&ps, "pane", "list") })
	wg.Wait()

	wsLabel := map[string]string{}
	for _, w := range ws.Workspaces {
		wsLabel[w.ID] = w.Label
	}
	tabLabel := map[string]string{}
	for _, t := range ts.Tabs {
		tabLabel[t.ID] = t.Label
	}
	branches := gitBranches(ps.Panes)

	// ワークスペース名とタブ名の列幅を揃える
	wsWidth, tabWidth := 0, 0
	for _, p := range ps.Panes {
		wsWidth = max(wsWidth, ansi.StringWidth(wsLabel[p.WorkspaceID]))
		tabWidth = max(tabWidth, ansi.StringWidth(tabLabel[p.TabID]))
	}

	// overlay で開いた場合はランチャー自身もペインなので除き、開く前にいたペインを現在のペインとする
	self, here := os.Getenv("HERDR_PANE_ID"), launchedFrom()
	var items []item
	for _, p := range ps.Panes {
		if p.ID == self {
			continue
		}
		if here != "" {
			p.Focused = p.ID == here
		}
		kind, status := "terminal", ""
		if p.Agent != "" {
			kind, status = p.Agent, p.AgentStatus
		}
		ws, tb, name := wsLabel[p.WorkspaceID], tabLabel[p.TabID], paneName(p)
		desc := fmt.Sprintf("%s › %s › %s%s\ncwd: %s", ws, tb, name, current(p.Focused), shortenHome(p.Cwd))
		if b := branches[p.Cwd]; b != "" {
			desc += "\nbranch: " + b
		}
		items = append(items, item{
			id:     p.ID,
			kind:   kind,
			status: status,
			title:  name,
			columns: []column{
				kindColumn(kind),
				{pad(ws, wsWidth) + "  ", text},
				{pad(tb, tabWidth) + "  ", subtext0},
				{name, text},
			},
			extra:   branches[p.Cwd] + " " + p.Cwd,
			group:   p.WorkspaceID,
			focused: p.Focused,
			desc:    desc,
			run:     func() error { return afterClose("focus-pane", p.ID, p.TabID, p.Agent) },
			preview: func() (string, error) { return readPane(p.ID) },
		})
	}
	return items
}

// 各ペインの cwd のブランチを並行して調べる。git リポジトリでなければ空
func gitBranches(panes []pane) map[string]string {
	cwds := map[string]bool{}
	for _, p := range panes {
		cwds[p.Cwd] = true
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		branches = map[string]string{}
	)
	for cwd := range cwds {
		wg.Go(func() {
			out, err := exec.Command("git", "-C", cwd, "branch", "--show-current").Output()
			if err != nil {
				return
			}
			mu.Lock()
			branches[cwd] = strings.TrimSpace(string(out))
			mu.Unlock()
		})
	}
	wg.Wait()
	return branches
}

// ランチャーを開いたときにフォーカスしていたペイン
func launchedFrom() string {
	var ctx struct {
		FocusedPaneID string `json:"focused_pane_id"`
	}
	json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx)
	return ctx.FocusedPaneID
}

func current(focused bool) string {
	if focused {
		return " (current)"
	}
	return ""
}

// エージェントがいれば作業内容、いなければラベルかディレクトリ名で表す
func paneName(p pane) string {
	if p.Agent != "" && p.Title != "" {
		return p.Title
	}
	if p.Label != "" {
		return p.Label
	}
	return p.Cwd[strings.LastIndex(p.Cwd, "/")+1:]
}

// エージェントのペインは agent focus で直接移動できる。
// それ以外はペイン ID で移動する CLI がないため、タブへ移動してから上下左右の移動でたどり着く
func focusPane(p pane) error {
	if p.Agent != "" {
		return herdr("agent", "focus", p.ID)
	}
	if err := herdr("tab", "focus", p.TabID); err != nil {
		return err
	}
	for range 10 {
		l, err := layoutOf(p.ID)
		if err != nil {
			return err
		}
		focused := l.FocusedPaneID
		if focused == p.ID {
			return nil
		}
		rects := map[string]rect{}
		for _, lp := range l.Panes {
			rects[lp.ID] = lp.Rect
		}
		if err := herdr("pane", "focus", "--direction", direction(rects[focused], rects[p.ID]), "--pane", focused); err != nil {
			return err
		}
	}
	return errors.New("could not reach pane " + p.ID)
}

type paneLayout struct {
	FocusedPaneID string `json:"focused_pane_id"`
	Panes         []struct {
		ID   string `json:"pane_id"`
		Rect rect   `json:"rect"`
	} `json:"panes"`
}

func layoutOf(paneID string) (paneLayout, error) {
	var resp struct {
		Layout paneLayout `json:"layout"`
	}
	err := herdrJSON(&resp, "pane", "layout", "--pane", paneID)
	return resp.Layout, err
}

// ペインに今見えている画面を色付きで取得する
func readPane(paneID string) (string, error) {
	b, err := exec.Command(herdrBin(), "pane", "read", paneID, "--source", "visible", "--format", "ansi").Output()
	if err != nil {
		return "", fmt.Errorf("herdr pane read %s: %w", paneID, err)
	}
	return strings.ReplaceAll(string(b), "\r", ""), nil
}

// from から to へ向かう方向。左右に重なっていなければ左右、重なっていれば上下に動く
func direction(from, to rect) string {
	switch {
	case to.X+to.Width <= from.X:
		return "left"
	case to.X >= from.X+from.Width:
		return "right"
	case to.Y+to.Height <= from.Y:
		return "up"
	default:
		return "down"
	}
}

func shortenHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
