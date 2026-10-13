package main

// A picker laid out like nvim's telescope (horizontal, prompt_position = top, sorting_strategy = ascending).
// Colors live in theme.go; the outer frame is the herdr popup's own border.

import (
	"fmt"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

type match struct {
	item    item
	indexes []int
}

type model struct {
	items         []item
	query         []rune
	pos           int // cursor position in query, in runes
	matches       []match
	cursor        int
	offset        int
	width, height int
	chosen        *item
	previews      map[string]string // pane screens fetched so far, by item id

	// With --start select, like herdr's goto, j/k move and / starts a search. With search, it is always searching.
	selectFirst bool
	searching   bool
	narrow      string // agent_status narrowed with b/w/i/d, or "commands" with c. Empty shows everything.

	// What --sources includes. Keys that only make sense for a source are enabled and hinted only then.
	hasCommands bool
	hasPanes    bool
}

type previewMsg struct{ key, content string }

func pick(items []item, opts options) (*item, error) {
	// Keep #rrggbb colors from being rounded to 256 colors.
	lipgloss.SetColorProfile(termenv.TrueColor)

	selectFirst := opts.start == "select"
	m := model{
		items:       items,
		previews:    map[string]string{},
		selectFirst: selectFirst,
		searching:   !selectFirst,
		hasCommands: slices.Contains(opts.sources, "commands"),
		hasPanes:    slices.Contains(opts.sources, "panes"),
	}
	m.filter()
	if opts.cursor == "current" {
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
	var items []item
	for _, it := range m.items {
		if m.matchesFilter(it) {
			items = append(items, it)
		}
	}
	m.matches = m.matches[:0]
	if len(m.query) == 0 {
		for _, it := range items {
			m.matches = append(m.matches, match{item: it})
		}
	} else {
		m.matches = search(items, string(m.query))
	}
	m.cursor, m.offset = 0, 0
}

func (m model) matchesFilter(it item) bool {
	switch m.narrow {
	case "":
		return true
	case "commands":
		return it.kind == "command"
	default:
		return it.status == m.narrow
	}
}

func (m model) Init() tea.Cmd { return m.loadPreview() }

// Fetches the selected item's pane screen in the background unless it is cached.
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
			// With --start select, esc while searching goes back to j/k navigation.
			if !m.selectFirst || !m.searching {
				return m, tea.Quit
			}
			m.searching = false
		default:
			switch {
			case m.searching:
				m.editQuery(msg)
			case msg.Type == tea.KeyRunes && string(msg.Runes) == "q":
				return m, tea.Quit
			default:
				m.navigate(msg)
			}
		}
		return m, m.loadPreview()
	}
	return m, nil
}

// Keys when not searching with --start select (the same as herdr's goto).
func (m *model) navigate(msg tea.KeyMsg) {
	switch {
	case msg.Type == tea.KeyLeft && m.hasPanes:
		m.jumpGroup(-1)
		return
	case msg.Type == tea.KeyRight && m.hasPanes:
		m.jumpGroup(1)
		return
	case msg.Type != tea.KeyRunes:
		return
	}
	key := string(msg.Runes)
	switch key {
	case "j":
		m.move(1)
	case "k":
		m.move(-1)
	case "/":
		m.searching = true
		m.pos = len(m.query)
	default:
		if narrow, ok := m.narrowKeys()[key]; ok {
			m.narrow = narrow
			m.filter()
		}
	}
}

// Narrowing keys. Only panes have a status, and c only matters when commands and panes are mixed.
func (m model) narrowKeys() map[string]string {
	keys := map[string]string{}
	if m.hasPanes {
		keys["b"], keys["w"], keys["i"], keys["d"] = "blocked", "working", "idle", "done"
	}
	if m.hasCommands && m.hasPanes {
		keys["c"] = "commands"
	}
	if len(keys) > 0 {
		keys["a"] = ""
	}
	return keys
}

// Moves to the first pane of the previous/next workspace without wrapping around.
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

// The prompt is edited with readline/emacs keys.
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

// Returns a new slice with q[from:to] replaced by r.
func splice(q []rune, from, to int, r []rune) []rune {
	out := make([]rune, 0, len(q)-(to-from)+len(r))
	out = append(out, q[:from]...)
	out = append(out, r...)
	return append(out, q[to:]...)
}

// Start of the word before the cursor (skip spaces, then non-spaces backwards).
func wordStart(q []rune, p int) int {
	for p > 0 && q[p-1] == ' ' {
		p--
	}
	for p > 0 && q[p-1] != ' ' {
		p--
	}
	return p
}

// End of the word after the cursor.
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

// Uses the herdr popup border as the outer frame and splits the inside with dividers only.
//
//	 > query          │ title
//	──────────────────┤ desc
//	 > result         │──────
//	   result         │ screen
func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	// No preview when too narrow.
	leftWidth := m.width
	if m.width >= 60 {
		leftWidth = m.width / 2
	}
	// Rows with a horizontal rule join the vertical divider with ┤.
	var left []string
	rules := map[int]bool{}
	addRule := func() {
		rules[len(left)] = true
		left = append(left, dimStyle.Render(strings.Repeat("─", leftWidth)))
	}
	// Always show the prompt so the list does not shift; key hints and counts go at the bottom, dimmed.
	left = append(left, " "+m.promptLine())
	addRule()
	resultsHeight := max(m.height-len(left)-1, 0)
	results := m.resultLines(leftWidth-1, resultsHeight)
	for i := range resultsHeight {
		line := ""
		if i < len(results) {
			line = " " + results[i]
		}
		left = append(left, line)
	}
	left = append(left, " "+m.footerLine(leftWidth-2))
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

// Truncates or pads each line to exactly width and returns height lines.
// Resets colors at the end of each line so pane colors do not bleed into the next column.
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

func (m model) promptLine() string {
	// Dim the > until / is pressed, since typing is not possible before that.
	prefix := lipgloss.NewStyle().Foreground(caretColor)
	if !m.searching {
		prefix = prefix.Foreground(faint)
	}
	prompt := prefix.Render("> ")
	if m.searching {
		prompt += m.queryWithCursor()
	} else {
		prompt += titleStyle.Render(string(m.query))
	}
	return prompt
}

// The bottom line: key hints, with the active filter and the count on the right.
func (m model) footerLine(width int) string {
	help := strings.Join(m.help(), "  ")
	faint := lipgloss.NewStyle().Foreground(faint)
	right := faint.Render(fmt.Sprintf("%d / %d", len(m.matches), len(m.items)))
	switch m.narrow {
	case "":
	case "commands":
		right = lipgloss.NewStyle().Foreground(kindColor("command")).Render("commands") + "  " + right
	default:
		right = lipgloss.NewStyle().Foreground(statusColor(m.narrow)).Render("● "+m.narrow) + "  " + right
	}
	return spread(faint.Render(help), right, width)
}

// Hints only the keys that work in the current state.
func (m model) help() []string {
	enter := "enter select"
	if m.searching {
		esc := "esc close"
		if m.selectFirst {
			esc = "esc done"
		}
		return []string{esc, "ctrl-n/p move", enter}
	}
	help := []string{"/ search"}
	keys := m.narrowKeys()
	var narrow []string
	for _, k := range []string{"b", "w", "i", "d", "c"} {
		if _, ok := keys[k]; ok {
			narrow = append(narrow, k)
		}
	}
	if len(narrow) > 0 {
		help = append(help, strings.Join(narrow, "/")+" filter", "a all")
	}
	if m.hasPanes {
		help = append(help, "←/→ workspace")
	}
	return append(help, enter, "q close")
}

// Puts left at the left edge and right at the right edge, dropping right if it does not fit.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// Draws the character under the cursor in reverse.
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
		lipgloss.NewStyle().Foreground(cursorFg).Background(fg).Render(under) +
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

// Shows agent status as a colored dot, like herdr's status_indicators = "dots".
func statusColor(status string) lipgloss.Color {
	switch status {
	case "working":
		return yellow
	case "blocked":
		return red
	case "done":
		return green
	default: // idle, unknown
		return dim
	}
}

// Like herdr's sidebar, the kind column shows claude in orange (catppuccin peach) and codex in blue.
func kindColor(kind string) lipgloss.Color {
	switch kind {
	case "claude":
		return orange
	case "codex":
		return blue
	case "command":
		return green
	default:
		return subtle
	}
}

// Each character carries the background so the selected row's highlight reaches the end of the line.
func resultLine(mt match, selected bool, width int) string {
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(selectionBg)
		}
		return s
	}
	base := bg(lipgloss.NewStyle().Foreground(fg))
	hit := bg(lipgloss.NewStyle().Foreground(accent).Bold(true))
	caret := base.Render("  ")
	if selected {
		caret = bg(lipgloss.NewStyle().Foreground(caretColor)).Render("> ")
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
	offset := 0 // byte offset in label; match positions are byte offsets
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

// Drops trailing blank lines of the pane screen and keeps the bottom (where the prompt is) if it does not fit.
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
