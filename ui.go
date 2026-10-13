package main

// A picker laid out like nvim's telescope (horizontal, prompt_position = top, sorting_strategy = ascending).
// Colors live in theme.go; the outer frame is the herdr popup's own border.

import (
	"fmt"
	"image/color"
	"os"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
	width, height int
	chosen        *item
	previews      map[string]string // pane screens fetched so far, by item id
	fetching      map[string]bool   // pane screens being fetched, so a refresh does not pile up
	ticking       bool              // the refresh timer runs only while the selected item has a pane screen
	outputs       map[string]*commandOutput
	proc          *runningCommand // the command running inside the picker, if any
	outputCh      <-chan tea.Msg  // its output and exit
	record        func(id string) // records a pick for frecency

	// With start = "select", like herdr's goto, j/k move and / starts a search. With "search", it is always searching.
	selectFirst bool
	searching   bool
	narrow      string // agent_status narrowed with b/w/i/d, or "commands" with c. Empty shows everything.

	// Whether commands and panes are listed. Keys that only make sense for one are enabled and hinted only then.
	hasCommands bool
	hasPanes    bool
}

type previewMsg struct{ key, content string }

type tickMsg struct{}

// How often the selected pane's screen is read again while the picker is open.
const refreshInterval = 500 * time.Millisecond

func tick() tea.Cmd { return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} }) }

func pick(items []item, opts options, record func(id string)) (*item, error) {
	result, err := tea.NewProgram(newModel(items, opts, record)).Run()
	if err != nil {
		return nil, err
	}
	final := result.(model)
	if final.proc != nil {
		final.proc.stop() // closing the picker stops a command still running in it
	}
	return final.chosen, nil
}

func newModel(items []item, opts options, record func(id string)) model {
	selectFirst := opts.start == "select"
	m := model{
		items:       items,
		previews:    map[string]string{},
		fetching:    map[string]bool{},
		outputs:     map[string]*commandOutput{},
		record:      record,
		selectFirst: selectFirst,
		searching:   !selectFirst,
		hasCommands: slices.ContainsFunc(items, func(it item) bool { return it.kind == "command" }),
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
	m.ticking = m.hasPreview()
	return m
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
	m.cursor = 0
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

func (m model) Init() tea.Cmd {
	if m.ticking {
		return tea.Batch(m.loadPreview(), tick())
	}
	return m.loadPreview()
}

func (m model) hasPreview() bool {
	return len(m.matches) > 0 && m.matches[m.cursor].item.preview != nil
}

// After the selection changes: loads its pane screen and starts the refresh timer if it was stopped.
// It changes m, so call it before returning m.
func (m *model) selectionChanged() tea.Cmd {
	cmd := m.loadPreview()
	if m.ticking || !m.hasPreview() {
		return cmd
	}
	m.ticking = true
	return tea.Batch(cmd, tick())
}

// Fetches the selected item's pane screen in the background unless it is cached.
func (m model) loadPreview() tea.Cmd {
	if len(m.matches) == 0 {
		return nil
	}
	if _, ok := m.previews[m.matches[m.cursor].item.id]; ok {
		return nil
	}
	return m.fetchPreview()
}

// Fetches the selected item's pane screen in the background, unless a fetch is already under way.
func (m model) fetchPreview() tea.Cmd {
	if len(m.matches) == 0 || m.previews == nil {
		return nil
	}
	it := m.matches[m.cursor].item
	if it.preview == nil || m.fetching[it.id] {
		return nil
	}
	m.fetching[it.id] = true
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
		delete(m.fetching, msg.key)
	case tickMsg:
		// Nothing to refresh, such as a command row, stops the timer until a pane is selected again.
		if !m.hasPreview() {
			m.ticking = false
			return m, nil
		}
		return m, tea.Batch(m.fetchPreview(), tick())
	case outputMsg:
		m.outputs[msg.id].add(msg.chunk)
		return m, waitOutput(m.outputCh)
	case exitMsg:
		out := m.outputs[msg.id]
		out.running, out.err = false, msg.err
		m.proc, m.outputCh = nil, nil
		// A failure stays on screen so its output can be read.
		if out.closeOnSuccess && msg.err == nil {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.move(-1)
		case tea.MouseWheelDown:
			m.move(1)
		}
		cmd := m.selectionChanged()
		return m, cmd
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		// A click selects a row; a click on the selected row runs it like enter.
		i, ok := m.rowAt(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		if i == m.cursor {
			return m.activate()
		}
		m.cursor = i
		cmd := m.selectionChanged()
		return m, cmd
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			return m.activate()
		case "up", "ctrl+p":
			m.move(-1)
		case "down", "ctrl+n":
			m.move(1)
		case "esc":
			// With start = "select", esc while searching goes back to j/k navigation.
			if !m.selectFirst || !m.searching {
				return m, tea.Quit
			}
			m.searching = false
		default:
			switch {
			case m.searching:
				m.editQuery(msg)
			case typed(msg) == "q":
				return m, tea.Quit
			default:
				m.navigate(msg)
			}
		}
		cmd := m.selectionChanged()
		return m, cmd
	}
	return m, nil
}

// Runs the selected item: a shell command runs inside the picker with its output in the preview,
// anything else closes the picker and runs afterwards.
func (m model) activate() (tea.Model, tea.Cmd) {
	if len(m.matches) == 0 {
		return m, tea.Quit
	}
	it := m.matches[m.cursor].item
	if it.command == "" || it.run != nil {
		m.chosen = &it
		return m, tea.Quit
	}
	if m.proc != nil {
		return m, nil // one command at a time
	}
	if m.record != nil {
		m.record(it.id)
	}
	out := &commandOutput{running: true, closeOnSuccess: it.closeOnSuccess}
	m.outputs[it.id] = out
	proc, ch, err := startCommand(it)
	if err != nil {
		out.running, out.err = false, err
		return m, nil
	}
	m.proc, m.outputCh = proc, ch
	return m, waitOutput(ch)
}

// The list row at x, y on the screen, as an index into matches.
func (m model) rowAt(x, y int) (int, bool) {
	row := y - listTop
	if x >= m.leftWidth() || row < 0 || row >= m.listHeight() {
		return 0, false
	}
	i := m.listOffset() + row
	return i, i < len(m.matches)
}

// The list starts below the prompt and its rule, and ends above the footer.
const listTop = 2

func (m model) listHeight() int { return max(m.height-listTop-1, 0) }

// The first match shown, so that the cursor stays on screen.
func (m model) listOffset() int { return max(m.cursor-m.listHeight()+1, 0) }

// The list takes the whole width when it is too narrow for a preview.
func (m model) leftWidth() int {
	if m.width >= 60 {
		return m.width / 2
	}
	return m.width
}

// The text a key types, or "" for keys held with ctrl, alt or super.
func typed(msg tea.KeyPressMsg) string {
	if msg.Mod.Contains(tea.ModCtrl) || msg.Mod.Contains(tea.ModAlt) || msg.Mod.Contains(tea.ModSuper) {
		return ""
	}
	return msg.Text
}

// Keys when not searching with start = "select" (the same as herdr's goto).
func (m *model) navigate(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "left":
		if m.hasPanes {
			m.jumpGroup(-1)
		}
		return
	case "right":
		if m.hasPanes {
			m.jumpGroup(1)
		}
		return
	}
	switch key := typed(msg); key {
	case "j":
		m.move(1)
	case "k":
		m.move(-1)
	case "/":
		m.searching = true
		m.pos = len(m.query)
	default:
		if narrow, ok := m.narrowKeys()[key]; ok && key != "" {
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

// The prompt is edited with readline/emacs keys. Depending on the terminal, ctrl+h arrives as
// either ctrl+h or backspace, so both delete a character.
func (m *model) editQuery(msg tea.KeyPressMsg) {
	q, p := m.query, m.pos
	switch msg.String() {
	case "ctrl+a", "home":
		m.pos = 0
	case "ctrl+e", "end":
		m.pos = len(q)
	case "ctrl+b", "left":
		m.pos = max(p-1, 0)
	case "ctrl+f", "right":
		m.pos = min(p+1, len(q))
	case "alt+b":
		m.pos = wordStart(q, p)
	case "alt+f":
		m.pos = wordEnd(q, p)
	case "alt+d":
		m.setQuery(splice(q, p, wordEnd(q, p), nil), p)
	case "ctrl+w", "alt+backspace":
		start := wordStart(q, p)
		m.setQuery(splice(q, start, p, nil), start)
	case "ctrl+h", "backspace":
		if p > 0 {
			m.setQuery(splice(q, p-1, p, nil), p-1)
		}
	case "ctrl+d", "delete":
		if p < len(q) {
			m.setQuery(splice(q, p, p+1, nil), p)
		}
	case "ctrl+u":
		m.setQuery(splice(q, 0, p, nil), 0)
	case "ctrl+k":
		m.setQuery(splice(q, p, len(q), nil), p)
	default:
		if text := []rune(typed(msg)); len(text) > 0 {
			m.setQuery(splice(q, p, p, text), p+len(text))
		}
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
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) render() string {
	if m.width == 0 {
		return ""
	}
	// No preview when too narrow.
	leftWidth := m.leftWidth()
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
	results := m.resultLines(leftWidth - 1)
	for i := range m.listHeight() {
		line := ""
		if i < len(results) {
			line = " " + results[i]
		}
		left = append(left, line)
	}
	left = append(left, " "+m.footerLine(leftWidth-2))
	if leftWidth == m.width {
		lines := fitLines(left, leftWidth, m.height)
		for i, l := range lines {
			lines[i] = withBackground(l)
		}
		return strings.Join(lines, "\n")
	}

	rightWidth := m.width - leftWidth - 1
	var right []string
	if len(m.matches) > 0 {
		it := m.matches[m.cursor].item
		right = append(right, " "+titleStyle.Bold(true).Render(it.title))
		right = append(right, previewLines(it, rightWidth-2)...)
		if out := m.outputs[it.id]; out != nil {
			text, color := out.status()
			right = append(right, " "+lipgloss.NewStyle().Foreground(color).Render(text))
			right = append(right, dimStyle.Render(strings.Repeat("─", rightWidth)))
			right = append(right, tail(out.lines(), m.height-len(right))...)
		} else if content, ok := m.previews[it.id]; ok {
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
		lines[i] = withBackground(left[i] + dimStyle.Render(divider) + right[i])
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
	faint := lipgloss.NewStyle().Foreground(faint)
	right := faint.Render(fmt.Sprintf("%d / %d", len(m.matches), len(m.items)))
	switch m.narrow {
	case "":
	case "commands":
		right = lipgloss.NewStyle().Foreground(kindColor("command")).Render("commands") + "  " + right
	default:
		right = lipgloss.NewStyle().Foreground(statusColor(m.narrow)).Render(statusDot(m.narrow)+" "+m.narrow) + "  " + right
	}
	// The count always shows; key hints that do not fit are dropped, least important first.
	hints := m.help()
	for len(hints) > 0 && lipgloss.Width(strings.Join(hints, "  "))+1+lipgloss.Width(right) > width {
		least := 0
		for i, h := range hints {
			if hintRank(h) < hintRank(hints[least]) {
				least = i
			}
		}
		hints = slices.Delete(hints, least, least+1)
	}
	return spread(faint.Render(strings.Join(hints, "  ")), right, width)
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

// How much a key hint matters when the footer is too narrow for all of them; lower is dropped first.
// enter and closing are what everyone needs, so they stay longest.
func hintRank(h string) int {
	switch {
	case strings.HasSuffix(h, " workspace"):
		return 0
	case h == "a all":
		return 1
	case strings.HasSuffix(h, " filter"):
		return 2
	case strings.HasSuffix(h, " move"):
		return 3
	case h == "/ search":
		return 4
	case strings.HasPrefix(h, "esc "), h == "q close":
		return 5
	default: // enter
		return 6
	}
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
	under, after := " ", ""
	if m.pos < len(m.query) {
		under, after = string(m.query[m.pos]), string(m.query[m.pos+1:])
	}
	return titleStyle.Render(string(m.query[:m.pos])) +
		lipgloss.NewStyle().Foreground(cursorFg).Background(fg).Render(under) +
		titleStyle.Render(after)
}

func (m model) resultLines(width int) []string {
	offset, height := m.listOffset(), m.listHeight()
	var lines []string
	for i := offset; i < len(m.matches) && i < offset+height; i++ {
		lines = append(lines, resultLine(m.matches[i], i == m.cursor, width))
	}
	return lines
}

// Shows agent status as a colored dot, like herdr's goto (status_indicators = "dots").
func statusDot(status string) string {
	switch status {
	case "working", "blocked", "done":
		return "●"
	case "idle":
		return "○"
	default: // unknown
		return "·"
	}
}

func statusColor(status string) color.Color {
	switch status {
	case "working":
		return yellow
	case "blocked":
		return red
	case "done":
		return teal
	case "idle":
		return green
	default: // unknown
		return dim
	}
}

// Like herdr's sidebar, the kind column shows claude in orange (catppuccin peach) and codex in blue.
func kindColor(kind string) color.Color {
	switch kind {
	case "claude":
		return orange
	case "codex":
		return blue
	case "command":
		return mauve
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
		b.WriteString(bg(lipgloss.NewStyle().Foreground(statusColor(mt.item.status))).Render(statusDot(mt.item.status) + " "))
	} else {
		b.WriteString(base.Render("  "))
	}
	offset := 0 // byte offset in label; match positions are byte offsets
	for _, c := range mt.item.columns {
		style := bg(lipgloss.NewStyle().Foreground(c.color))
		// Styling is the costly part of drawing, so style each run of matched or unmatched characters at once.
		for start := 0; start < len(c.text); {
			isHit, end := matched[offset+start], start
			for end < len(c.text) && matched[offset+end] == isHit {
				_, size := utf8.DecodeRuneInString(c.text[end:])
				end += size
			}
			if isHit {
				b.WriteString(hit.Render(c.text[start:end]))
			} else {
				b.WriteString(style.Render(c.text[start:end]))
			}
			start = end
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
	return tail(strings.Split(strings.TrimRight(content, "\n"), "\n"), height)
}

// Drops trailing blank lines and keeps the last height lines, indented by one space.
func tail(lines []string, height int) []string {
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if height <= 0 {
		return nil
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = " " + l
	}
	return out
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
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(statusColor(it.status)).Render(statusDot(it.status)+" "+it.status))
	}
	if it.command != "" {
		cwd, _ := os.Getwd()
		lines = append(lines, "")
		add(lipgloss.NewStyle().Foreground(mauve), "$ "+it.command)
		add(dimStyle, "in "+shortenHome(cwd))
	}
	return lines
}
