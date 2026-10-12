package main

// nvim の telescope (horizontal, prompt_position = top, sorting_strategy = ascending) の配置と
// catppuccin-mocha の配色に揃えたピッカー。外枠は herdr の popup の枠を使う

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/sahilm/fuzzy"
)

// catppuccin-mocha
var (
	base     = lipgloss.Color("#1e1e2e")
	text     = lipgloss.Color("#cdd6f4")
	overlay0 = lipgloss.Color("#6c7086")
	surface0 = lipgloss.Color("#313244")
	surface2 = lipgloss.Color("#585b70")
	blue     = lipgloss.Color("#89b4fa")
	flamingo = lipgloss.Color("#f2cdcd")
	green    = lipgloss.Color("#a6e3a1")
	yellow   = lipgloss.Color("#f9e2af")
	red      = lipgloss.Color("#f38ba8")
	peach    = lipgloss.Color("#fab387")
	subtext0 = lipgloss.Color("#a6adc8")

	titleStyle = lipgloss.NewStyle().Foreground(text)
	dimStyle   = lipgloss.NewStyle().Foreground(overlay0)
)

type match struct {
	item    item
	indexes []int
}

type model struct {
	items         []item
	query         []rune
	pos           int // query 内のカーソル位置(rune 単位)
	matches       []match
	cursor        int
	offset        int
	width, height int
	chosen        *item
	previews      map[string]string // id ごとに取得済みのペインの画面

	// goto は herdr の goto と同じく、最初は j/k で選び / で検索に入る。ランチャーは常に検索中
	gotoMode  bool
	searching bool
	status    string // goto の b/w/i/d で絞り込む agent_status。空なら全部
}

type previewMsg struct{ key, content string }

type searchTexts []item

func (t searchTexts) String(i int) string { return t[i].searchText() }
func (t searchTexts) Len() int            { return len(t) }

func pick(items []item, gotoMode bool) (*item, error) {
	// catppuccin の色が 256 色に丸められないようにする
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := model{items: items, previews: map[string]string{}, gotoMode: gotoMode, searching: !gotoMode}
	m.filter()
	// goto は今いるペインを選んだ状態で開く
	if gotoMode {
		for i, mt := range m.matches {
			if mt.item.focused {
				m.cursor = i
			}
		}
	}
	result, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, err
	}
	return result.(model).chosen, nil
}

func (m *model) filter() {
	var items searchTexts
	for _, it := range m.items {
		if m.status == "" || it.status == m.status {
			items = append(items, it)
		}
	}
	m.matches = m.matches[:0]
	if len(m.query) == 0 {
		for _, it := range items {
			m.matches = append(m.matches, match{item: it})
		}
	} else {
		for _, f := range fuzzy.FindFrom(string(m.query), items) {
			m.matches = append(m.matches, match{item: items[f.Index], indexes: f.MatchedIndexes})
		}
	}
	m.cursor, m.offset = 0, 0
}

func (m model) Init() tea.Cmd { return m.loadPreview() }

// 選択中の候補のペインの画面をまだ取得していなければ、裏で取得する
func (m model) loadPreview() tea.Cmd {
	if len(m.matches) == 0 || m.previews == nil {
		return nil
	}
	it := m.matches[m.cursor].item
	if _, ok := m.previews[it.id]; ok || it.preview == nil {
		return nil
	}
	return func() tea.Msg {
		content, err := it.preview()
		if err != nil {
			content = err.Error()
		}
		return previewMsg{it.id, content}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case previewMsg:
		m.previews[msg.key] = msg.content
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.matches) > 0 {
				m.chosen = &m.matches[m.cursor].item
			}
			return m, tea.Quit
		case tea.KeyUp, tea.KeyCtrlP:
			m.move(-1)
		case tea.KeyDown, tea.KeyCtrlN:
			m.move(1)
		case tea.KeyEsc:
			// goto の検索中は検索を抜けて j/k の操作に戻る
			if !m.gotoMode || !m.searching {
				return m, tea.Quit
			}
			m.searching = false
		default:
			if m.searching {
				m.editQuery(msg)
			} else {
				m.navigate(msg)
			}
		}
		return m, m.loadPreview()
	}
	return m, nil
}

// herdr の goto の検索していないときの操作
func (m *model) navigate(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyLeft:
		m.jumpGroup(-1)
		return
	case tea.KeyRight:
		m.jumpGroup(1)
		return
	case tea.KeyRunes:
	default:
		return
	}
	switch string(msg.Runes) {
	case "j":
		m.move(1)
	case "k":
		m.move(-1)
	case "/":
		m.searching = true
		m.pos = len(m.query)
	case "b", "w", "i", "d", "a":
		m.status = map[string]string{"b": "blocked", "w": "working", "i": "idle", "d": "done", "a": ""}[string(msg.Runes)]
		m.filter()
	}
}

// 前後のワークスペースの最初のペインへ移動する。端では折り返さない
func (m *model) jumpGroup(d int) {
	if len(m.matches) == 0 {
		return
	}
	group := func(i int) string { return m.matches[i].item.group }
	i := m.cursor
	if d > 0 {
		for i < len(m.matches)-1 && group(i+1) == group(m.cursor) {
			i++
		}
		if i < len(m.matches)-1 {
			m.cursor = i + 1
		}
		return
	}
	for i > 0 && group(i-1) == group(m.cursor) {
		i--
	}
	if i == 0 {
		return
	}
	i--
	for i > 0 && group(i-1) == group(i) {
		i--
	}
	m.cursor = i
}

// プロンプトは readline / emacs と同じキーで編集する
func (m *model) editQuery(msg tea.KeyMsg) {
	q, p := m.query, m.pos
	switch {
	case msg.Type == tea.KeyCtrlA || msg.Type == tea.KeyHome:
		m.pos = 0
	case msg.Type == tea.KeyCtrlE || msg.Type == tea.KeyEnd:
		m.pos = len(q)
	case msg.Type == tea.KeyCtrlB || msg.Type == tea.KeyLeft:
		m.pos = max(p-1, 0)
	case msg.Type == tea.KeyCtrlF || msg.Type == tea.KeyRight:
		m.pos = min(p+1, len(q))
	case msg.Alt && msg.Type == tea.KeyRunes && string(msg.Runes) == "b":
		m.pos = wordStart(q, p)
	case msg.Alt && msg.Type == tea.KeyRunes && string(msg.Runes) == "f":
		m.pos = wordEnd(q, p)
	case msg.Alt && msg.Type == tea.KeyRunes && string(msg.Runes) == "d":
		m.setQuery(splice(q, p, wordEnd(q, p), nil), p)
	case msg.Type == tea.KeyCtrlW || (msg.Alt && msg.Type == tea.KeyBackspace):
		start := wordStart(q, p)
		m.setQuery(splice(q, start, p, nil), start)
	case msg.Type == tea.KeyCtrlH || msg.Type == tea.KeyBackspace:
		if p > 0 {
			m.setQuery(splice(q, p-1, p, nil), p-1)
		}
	case msg.Type == tea.KeyCtrlD || msg.Type == tea.KeyDelete:
		if p < len(q) {
			m.setQuery(splice(q, p, p+1, nil), p)
		}
	case msg.Type == tea.KeyCtrlU:
		m.setQuery(splice(q, 0, p, nil), 0)
	case msg.Type == tea.KeyCtrlK:
		m.setQuery(splice(q, p, len(q), nil), p)
	case msg.Type == tea.KeySpace:
		m.setQuery(splice(q, p, p, []rune{' '}), p+1)
	case msg.Type == tea.KeyRunes && !msg.Alt:
		m.setQuery(splice(q, p, p, msg.Runes), p+len(msg.Runes))
	}
}

func (m *model) setQuery(q []rune, pos int) {
	m.query, m.pos = q, pos
	m.filter()
}

// q[from:to] を r に置き換えた新しいスライスを返す
func splice(q []rune, from, to int, r []rune) []rune {
	out := make([]rune, 0, len(q)-(to-from)+len(r))
	out = append(out, q[:from]...)
	out = append(out, r...)
	return append(out, q[to:]...)
}

// カーソルの前の単語の先頭(空白を飛ばしてから、空白でない文字を遡る)
func wordStart(q []rune, p int) int {
	for p > 0 && q[p-1] == ' ' {
		p--
	}
	for p > 0 && q[p-1] != ' ' {
		p--
	}
	return p
}

// カーソルの後の単語の末尾
func wordEnd(q []rune, p int) int {
	for p < len(q) && q[p] == ' ' {
		p++
	}
	for p < len(q) && q[p] != ' ' {
		p++
	}
	return p
}

func (m *model) move(d int) {
	if len(m.matches) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+d, 0), len(m.matches)-1)
}

// herdr の popup の枠を外枠として使い、中は区切り線だけで分ける
//
//	 > query          │ title
//	──────────────────┤ desc
//	 > result         │──────
//	   result         │ screen
func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	// 狭いときはプレビューを出さない
	leftWidth := m.width
	if m.width >= 60 {
		leftWidth = m.width / 2
	}
	// 横線の行は、縦の区切り線と ┤ でつなげる
	var left []string
	rules := map[int]bool{}
	addRule := func() {
		rules[len(left)] = true
		left = append(left, dimStyle.Render(strings.Repeat("─", leftWidth)))
	}
	// 位置がずれないよう、入力欄は goto でも常に出す。goto の操作の説明と件数は下に控えめに出す
	left = append(left, " "+m.promptLine(leftWidth-2))
	addRule()
	footerHeight := 0
	if m.gotoMode {
		footerHeight = 1
	}
	resultsHeight := max(m.height-len(left)-footerHeight, 0)
	results := m.resultLines(leftWidth-1, resultsHeight)
	for i := range resultsHeight {
		line := ""
		if i < len(results) {
			line = " " + results[i]
		}
		left = append(left, line)
	}
	if m.gotoMode {
		left = append(left, " "+m.footerLine(leftWidth-2))
	}
	if leftWidth == m.width {
		return strings.Join(fitLines(left, leftWidth, m.height), "\n")
	}

	rightWidth := m.width - leftWidth - 1
	var right []string
	if len(m.matches) > 0 {
		it := m.matches[m.cursor].item
		right = append(right, " "+titleStyle.Bold(true).Render(it.title))
		right = append(right, previewLines(it, rightWidth-2)...)
		if content, ok := m.previews[it.id]; ok {
			right = append(right, dimStyle.Render(strings.Repeat("─", rightWidth)))
			right = append(right, screenLines(content, m.height-len(right))...)
		}
	}
	left, right = fitLines(left, leftWidth, m.height), fitLines(right, rightWidth, m.height)
	lines := make([]string, m.height)
	for i := range lines {
		divider := "│"
		if rules[i] {
			divider = "┤"
		}
		lines[i] = left[i] + dimStyle.Render(divider) + right[i]
	}
	return strings.Join(lines, "\n")
}

// 各行を width ちょうどに切り詰めるか空白で埋め、height 行にする。
// ペインの画面の色が隣に漏れないよう、行末で色をリセットする
func fitLines(lines []string, width, height int) []string {
	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], width, "") + "\x1b[0m"
		}
		out[i] = line + strings.Repeat(" ", max(width-ansi.StringWidth(line), 0))
	}
	return out
}

func (m model) promptLine(width int) string {
	// goto で / を押す前は入力できないので、> を暗くする
	prefix := lipgloss.NewStyle().Foreground(flamingo)
	if !m.searching {
		prefix = prefix.Foreground(surface2)
	}
	prompt := prefix.Render("> ")
	if m.searching {
		prompt += m.queryWithCursor()
	} else {
		prompt += titleStyle.Render(string(m.query))
	}
	// goto の件数は footerLine で右端に出す
	if m.gotoMode {
		return prompt
	}
	return spread(prompt, m.count(), width)
}

func (m model) count() string {
	return dimStyle.Render(fmt.Sprintf("%d / %d", len(m.matches), len(m.items)))
}

// goto の一番下の行。操作の説明を出し、右端に絞り込み中の状態と件数を出す
func (m model) footerLine(width int) string {
	help := "/ search  b/w/i/d filter  a all  ←/→ workspace  enter jump"
	if m.searching {
		help = "esc done  ctrl-n/p move  enter jump"
	}
	faint := lipgloss.NewStyle().Foreground(surface2)
	right := faint.Render(fmt.Sprintf("%d / %d", len(m.matches), len(m.items)))
	if m.status != "" {
		right = lipgloss.NewStyle().Foreground(statusColor(m.status)).Render("● "+m.status) + "  " + right
	}
	return spread(faint.Render(help), right, width)
}

// left を左端、right を右端に置く。入りきらなければ right を省く
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// カーソル位置の文字を反転させて描画する
func (m model) queryWithCursor() string {
	under := " "
	if m.pos < len(m.query) {
		under = string(m.query[m.pos])
	}
	after := ""
	if m.pos < len(m.query) {
		after = string(m.query[m.pos+1:])
	}
	return titleStyle.Render(string(m.query[:m.pos])) +
		lipgloss.NewStyle().Foreground(base).Background(text).Render(under) +
		titleStyle.Render(after)
}

func (m *model) resultLines(width, height int) []string {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}
	var lines []string
	for i := m.offset; i < len(m.matches) && i < m.offset+height; i++ {
		lines = append(lines, resultLine(m.matches[i], i == m.cursor, width))
	}
	return lines
}

// herdr の status_indicators = "dots" と同じく、エージェントの状態を色付きの点で表す
func statusColor(status string) lipgloss.Color {
	switch status {
	case "working":
		return yellow
	case "blocked":
		return red
	case "done":
		return green
	default: // idle, unknown
		return overlay0
	}
}

// 種類の列は sidebar と同じく claude を peach、codex を blue にする
func kindColor(kind string) lipgloss.Color {
	switch kind {
	case "claude":
		return peach
	case "codex":
		return blue
	case "command":
		return green
	default:
		return subtext0
	}
}

// 選択行は背景色を行末まで伸ばすため、文字ごとに背景色付きで描画する
func resultLine(mt match, selected bool, width int) string {
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(surface0)
		}
		return s
	}
	base := bg(lipgloss.NewStyle().Foreground(text))
	hit := bg(lipgloss.NewStyle().Foreground(blue).Bold(true))
	caret := base.Render("  ")
	if selected {
		caret = bg(lipgloss.NewStyle().Foreground(flamingo)).Render("> ")
	}
	matched := map[int]bool{}
	for _, i := range mt.indexes {
		matched[i] = true
	}
	var b strings.Builder
	b.WriteString(caret)
	if mt.item.status != "" {
		b.WriteString(bg(lipgloss.NewStyle().Foreground(statusColor(mt.item.status))).Render("● "))
	} else {
		b.WriteString(base.Render("  "))
	}
	offset := 0 // label 内のバイト位置。fuzzy の一致位置はバイト単位
	for _, c := range mt.item.columns {
		style := bg(lipgloss.NewStyle().Foreground(c.color))
		for i, r := range c.text {
			if matched[offset+i] {
				b.WriteString(hit.Render(string(r)))
			} else {
				b.WriteString(style.Render(string(r)))
			}
		}
		offset += len(c.text)
	}
	line := ansi.Truncate(b.String(), width, "")
	if pad := width - ansi.StringWidth(line); pad > 0 {
		line += base.Render(strings.Repeat(" ", pad))
	}
	return line
}

// ペインの画面は末尾の空行を除き、入りきらなければ下側(入力欄のある側)を残す
func screenLines(content string, height int) []string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if height <= 0 {
		return nil
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, l := range lines {
		lines[i] = " " + l
	}
	return lines
}

func previewLines(it item, width int) []string {
	var lines []string
	add := func(style lipgloss.Style, s string) {
		for _, l := range strings.Split(ansi.Wrap(s, width, " /"), "\n") {
			lines = append(lines, " "+style.Render(l))
		}
	}
	add(titleStyle, it.desc)
	if it.status != "" {
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(statusColor(it.status)).Render("● "+it.status))
	}
	if it.command != "" {
		cwd, _ := os.Getwd()
		lines = append(lines, "")
		add(lipgloss.NewStyle().Foreground(green), "$ "+it.command)
		add(dimStyle, "in "+shortenHome(cwd))
	}
	return lines
}
