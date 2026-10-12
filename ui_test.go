package main

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func testCommands(t *testing.T) []item {
	t.Helper()
	items, err := loadCommands("commands.toml")
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestLoadCommands(t *testing.T) {
	items := testCommands(t)
	if len(items) == 0 {
		t.Fatal("no commands")
	}
	// 名前の列幅が揃っている
	w := ansi.StringWidth(items[0].columns[1].text)
	for _, it := range items {
		if got := ansi.StringWidth(it.columns[1].text); got != w {
			t.Errorf("%s: name column width %d, want %d", it.title, got, w)
		}
	}
}

func TestLoadCommandsInvalid(t *testing.T) {
	path := t.TempDir() + "/commands.toml"
	os.WriteFile(path, []byte("[[commands]]\nname = \"both\"\ncommand = \"ls\"\npane = \"nvim\"\n"), 0o644)
	if _, err := loadCommands(path); err == nil {
		t.Error("want error when both command and pane are set")
	}
}

func runes(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func key(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func alt(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Alt: true}
}

func TestEditQuery(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyMsg
		query string
		pos   int
	}{
		{"type", []tea.KeyMsg{runes("gh"), key(tea.KeySpace), runes("pr")}, "gh pr", 5},
		{"ctrl+h", []tea.KeyMsg{runes("gh"), key(tea.KeyCtrlH)}, "g", 1},
		{"ctrl+a then insert", []tea.KeyMsg{runes("pr"), key(tea.KeyCtrlA), runes("gh ")}, "gh pr", 3},
		{"ctrl+b ctrl+d", []tea.KeyMsg{runes("abc"), key(tea.KeyCtrlB), key(tea.KeyCtrlB), key(tea.KeyCtrlD)}, "ac", 1},
		{"ctrl+f ctrl+e", []tea.KeyMsg{runes("abc"), key(tea.KeyCtrlA), key(tea.KeyCtrlF), key(tea.KeyCtrlE)}, "abc", 3},
		{"ctrl+w", []tea.KeyMsg{runes("gh pr view"), key(tea.KeyCtrlW)}, "gh pr ", 6},
		{"ctrl+w twice", []tea.KeyMsg{runes("gh pr view"), key(tea.KeyCtrlW), key(tea.KeyCtrlW)}, "gh ", 3},
		{"ctrl+u", []tea.KeyMsg{runes("gh pr"), key(tea.KeyCtrlB), key(tea.KeyCtrlU)}, "r", 0},
		{"ctrl+k", []tea.KeyMsg{runes("gh pr"), key(tea.KeyCtrlA), key(tea.KeyCtrlF), key(tea.KeyCtrlK)}, "g", 1},
		{"alt+b alt+d", []tea.KeyMsg{runes("gh pr view"), alt("b"), alt("b"), alt("d")}, "gh  view", 3},
		{"alt+f", []tea.KeyMsg{runes("gh pr"), key(tea.KeyCtrlA), alt("f")}, "gh pr", 2},
		{"bounds", []tea.KeyMsg{key(tea.KeyCtrlH), key(tea.KeyCtrlD), key(tea.KeyCtrlB), key(tea.KeyCtrlF)}, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m tea.Model = model{items: testCommands(t), searching: true}
			for _, k := range tt.keys {
				m, _ = m.Update(k)
			}
			got := m.(model)
			if string(got.query) != tt.query || got.pos != tt.pos {
				t.Errorf("got %q pos %d, want %q pos %d", string(got.query), got.pos, tt.query, tt.pos)
			}
		})
	}
}

func TestMoveSelection(t *testing.T) {
	tests := []struct {
		name   string
		keys   []tea.KeyMsg
		cursor int
		quit   bool
	}{
		{"ctrl+n ctrl+p", []tea.KeyMsg{key(tea.KeyCtrlN), key(tea.KeyCtrlN), key(tea.KeyCtrlP)}, 1, false},
		{"down up", []tea.KeyMsg{key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyUp)}, 1, false},
		{"bounds", []tea.KeyMsg{key(tea.KeyCtrlP)}, 0, false},
		{"esc", []tea.KeyMsg{key(tea.KeyEsc)}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := model{items: testCommands(t), searching: true}
			mm.filter()
			var m tea.Model = mm
			quit := false
			for _, k := range tt.keys {
				var cmd tea.Cmd
				m, cmd = m.Update(k)
				if cmd != nil {
					quit = true
					break
				}
			}
			if got := m.(model).cursor; got != tt.cursor || quit != tt.quit {
				t.Errorf("cursor %d quit %v, want cursor %d quit %v", got, quit, tt.cursor, tt.quit)
			}
		})
	}
}

func TestDirection(t *testing.T) {
	from := rect{X: 10, Y: 10, Width: 10, Height: 10}
	tests := []struct {
		to   rect
		want string
	}{
		{rect{X: 0, Y: 10, Width: 10, Height: 10}, "left"},
		{rect{X: 20, Y: 0, Width: 10, Height: 30}, "right"},
		{rect{X: 0, Y: 0, Width: 30, Height: 10}, "up"},
		{rect{X: 10, Y: 20, Width: 10, Height: 10}, "down"},
	}
	for _, tt := range tests {
		if got := direction(from, tt.to); got != tt.want {
			t.Errorf("direction(%v) = %s, want %s", tt.to, got, tt.want)
		}
	}
}

func TestScreenLines(t *testing.T) {
	got := screenLines("a\nb\nc\n\x1b[0m  \n\n", 2)
	if len(got) != 2 || got[0] != " b" || got[1] != " c" {
		t.Errorf("screenLines = %q, want [\" b\" \" c\"]", got)
	}
}

func gotoItems() []item {
	mk := func(id, group, status string) item {
		return item{id: id, group: group, status: status, columns: []column{{id, text}}}
	}
	return []item{
		mk("a1", "a", "working"),
		mk("a2", "a", "idle"),
		mk("b1", "b", ""),
		mk("b2", "b", "blocked"),
		mk("c1", "c", "working"),
	}
}

func TestGoto(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyMsg
		query string
		ids   string // 絞り込み後の候補
		at    string // 選択中の候補
		quit  bool
	}{
		{"j k", []tea.KeyMsg{runes("j"), runes("j"), runes("k")}, "", "a1 a2 b1 b2 c1", "a2", false},
		{"right", []tea.KeyMsg{key(tea.KeyRight)}, "", "a1 a2 b1 b2 c1", "b1", false},
		{"right right right", []tea.KeyMsg{key(tea.KeyRight), key(tea.KeyRight), key(tea.KeyRight)}, "", "a1 a2 b1 b2 c1", "c1", false},
		{"left from middle", []tea.KeyMsg{runes("j"), runes("j"), runes("j"), key(tea.KeyLeft)}, "", "a1 a2 b1 b2 c1", "a1", false},
		{"left at first", []tea.KeyMsg{runes("j"), key(tea.KeyLeft)}, "", "a1 a2 b1 b2 c1", "a2", false},
		{"filter working", []tea.KeyMsg{runes("w")}, "", "a1 c1", "a1", false},
		{"filter blocked then all", []tea.KeyMsg{runes("b"), runes("a")}, "", "a1 a2 b1 b2 c1", "a1", false},
		// / の後は j や w も検索文字になる
		{"search", []tea.KeyMsg{runes("/"), runes("b")}, "b", "b1 b2", "b1", false},
		{"search then esc then j", []tea.KeyMsg{runes("/"), runes("b"), key(tea.KeyEsc), runes("j")}, "b", "b1 b2", "b2", false},
		{"esc", []tea.KeyMsg{key(tea.KeyEsc)}, "", "a1 a2 b1 b2 c1", "a1", true},
		{"search esc esc", []tea.KeyMsg{runes("/"), key(tea.KeyEsc), key(tea.KeyEsc)}, "", "a1 a2 b1 b2 c1", "a1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := model{items: gotoItems(), gotoMode: true}
			mm.filter()
			var m tea.Model = mm
			quit := false
			for _, k := range tt.keys {
				var cmd tea.Cmd
				m, cmd = m.Update(k)
				if cmd != nil && cmd() == tea.Quit() {
					quit = true
					break
				}
			}
			got := m.(model)
			var ids []string
			for _, mt := range got.matches {
				ids = append(ids, mt.item.id)
			}
			at := ""
			if len(got.matches) > 0 {
				at = got.matches[got.cursor].item.id
			}
			if string(got.query) != tt.query || strings.Join(ids, " ") != tt.ids || at != tt.at || quit != tt.quit {
				t.Errorf("got query %q ids %q at %s quit %v, want %q %q %s %v",
					string(got.query), strings.Join(ids, " "), at, quit, tt.query, tt.ids, tt.at, tt.quit)
			}
		})
	}
}

func TestAlignLastColumn(t *testing.T) {
	items := alignLastColumn([]item{
		{columns: []column{{"command  ", text}, {"gh browse  ", text}, {"説明", text}}},
		{columns: []column{{"claude   ", text}, {"dotfiles  ", text}, {"1  ", text}, {"ペイン", text}}},
	})
	var starts []int
	for _, it := range items {
		w := 0
		for _, c := range it.columns[:len(it.columns)-1] {
			w += ansi.StringWidth(c.text)
		}
		starts = append(starts, w)
	}
	if starts[0] != starts[1] {
		t.Errorf("last column starts at %v, want equal", starts)
	}
}
