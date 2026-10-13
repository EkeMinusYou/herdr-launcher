package main

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func testCommands(t *testing.T) []item {
	t.Helper()
	cfg, err := loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	items, err := commandItems(cfg)
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
	// name columns have the same width
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
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commandItems(cfg); err == nil {
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

func TestScreenLines(t *testing.T) {
	got := screenLines("a\nb\nc\n\x1b[0m  \n\n", 2)
	if len(got) != 2 || got[0] != " b" || got[1] != " c" {
		t.Errorf("screenLines = %q, want [\" b\" \" c\"]", got)
	}
}

func gotoItems() []item {
	mk := func(id, group, status string) item {
		return item{id: id, group: group, status: status, columns: []column{{id, fg}}}
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
		ids   string // items left after filtering
		at    string // selected item
		quit  bool
	}{
		{"j k", []tea.KeyMsg{runes("j"), runes("j"), runes("k")}, "", "a1 a2 b1 b2 c1", "a2", false},
		{"right", []tea.KeyMsg{key(tea.KeyRight)}, "", "a1 a2 b1 b2 c1", "b1", false},
		{"right right right", []tea.KeyMsg{key(tea.KeyRight), key(tea.KeyRight), key(tea.KeyRight)}, "", "a1 a2 b1 b2 c1", "c1", false},
		{"left from middle", []tea.KeyMsg{runes("j"), runes("j"), runes("j"), key(tea.KeyLeft)}, "", "a1 a2 b1 b2 c1", "a1", false},
		{"left at first", []tea.KeyMsg{runes("j"), key(tea.KeyLeft)}, "", "a1 a2 b1 b2 c1", "a2", false},
		{"filter working", []tea.KeyMsg{runes("w")}, "", "a1 c1", "a1", false},
		{"filter blocked then all", []tea.KeyMsg{runes("b"), runes("a")}, "", "a1 a2 b1 b2 c1", "a1", false},
		// after /, j and w are typed into the search
		{"search", []tea.KeyMsg{runes("/"), runes("b")}, "b", "b1 b2", "b1", false},
		{"search then esc then j", []tea.KeyMsg{runes("/"), runes("b"), key(tea.KeyEsc), runes("j")}, "b", "b1 b2", "b2", false},
		{"esc", []tea.KeyMsg{key(tea.KeyEsc)}, "", "a1 a2 b1 b2 c1", "a1", true},
		{"q", []tea.KeyMsg{runes("q")}, "", "a1 a2 b1 b2 c1", "a1", true},
		// while searching, q is typed into the search instead of closing
		{"search q", []tea.KeyMsg{runes("/"), runes("q")}, "q", "", "", false},
		{"search esc esc", []tea.KeyMsg{runes("/"), key(tea.KeyEsc), key(tea.KeyEsc)}, "", "a1 a2 b1 b2 c1", "a1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mm := model{items: gotoItems(), selectFirst: true, hasPanes: true}
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
		{columns: []column{{"command  ", fg}, {"gh browse  ", fg}, {"説明", fg}}},
		{columns: []column{{"claude   ", fg}, {"dotfiles  ", fg}, {"1  ", fg}, {"ペイン", fg}}},
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

func TestParseOptions(t *testing.T) {
	opts, err := parseOptions([]string{"--start", "select", "--sources", "panes", "--cursor", "current"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.start != "select" || strings.Join(opts.sources, ",") != "panes" || opts.cursor != "current" || opts.sort != "herdr" {
		t.Errorf("got %+v", opts)
	}
	for _, args := range [][]string{{"--start", "x"}, {"--sources", "commands,x"}, {"--cursor", "x"}, {"--sort", "x"}} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("%v: want error", args)
		}
	}
}

func TestHistory(t *testing.T) {
	path := t.TempDir() + "/history.json"
	now := time.Now()
	panes := []string{"panes"}
	h := loadHistory(path)
	h.record(path, "a", now, nil, panes)
	h.record(path, "b", now, nil, panes)
	h.record(path, "b", now, nil, panes)
	h = loadHistory(path)
	if h["a"].Rank != 1 || h["b"].Rank != 2 {
		t.Errorf("ranks a=%v b=%v, want 1 and 2", h["a"].Rank, h["b"].Rank)
	}
	if !(h.score("b", now) > h.score("a", now) && h.score("a", now) > h.score("none", now)) {
		t.Errorf("scores b=%v a=%v none=%v", h.score("b", now), h.score("a", now), h.score("none", now))
	}
	// zoxide's weights: 4 within an hour, 2 within a day, 0.5 within a week, 0.25 after that
	for _, c := range []struct {
		age  time.Duration
		want float64
	}{{30 * time.Minute, 4}, {5 * time.Hour, 2}, {3 * 24 * time.Hour, 0.5}, {30 * 24 * time.Hour, 0.25}} {
		h := history{"x": {Rank: 1, Last: now.Add(-c.age).Unix()}}
		if got := h.score("x", now); got != c.want {
			t.Errorf("age %v: score %v, want %v", c.age, got, c.want)
		}
	}
}

func TestHistoryAging(t *testing.T) {
	now := time.Now().Unix()
	h := history{"big": {Rank: 9999, Last: now}, "small": {Rank: 1.05, Last: now}}
	h["new"] = visit{Rank: 1, Last: now} // total 10001.05 > 10000
	h.age()
	// every rank is scaled so the total would be 90% of the limit, then ranks below 1 are dropped
	factor := 0.9 * maxRankTotal / 10001.05
	if got, want := h["big"].Rank, 9999*factor; got < want-0.001 || got > want+0.001 {
		t.Errorf("big rank = %v, want %v", got, want)
	}
	for _, id := range []string{"small", "new"} {
		if _, ok := h[id]; ok {
			t.Errorf("%s fell below 1 and should be forgotten", id)
		}
	}
}

func TestHistoryForgetStale(t *testing.T) {
	path := t.TempDir() + "/history.json"
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour).Unix()
	h := history{
		"w1:p1":        {Rank: 5, Last: old},        // closed pane, old: forgotten
		"w1:p2":        {Rank: 5, Last: old},        // still listed: kept
		"command:gone": {Rank: 5, Last: old},        // commands were not loaded: kept
		"w1:p3":        {Rank: 5, Last: now.Unix()}, // closed but recent: kept
	}
	h.record(path, "w1:p2", now, map[string]bool{"w1:p2": true}, []string{"panes"})
	for id, want := range map[string]bool{"w1:p1": false, "w1:p2": true, "command:gone": true, "w1:p3": true} {
		if _, ok := h[id]; ok != want {
			t.Errorf("%s kept = %v, want %v", id, ok, want)
		}
	}
}

func TestFilterCommands(t *testing.T) {
	items := append(gotoItems(), item{id: "cmd", kind: "command", columns: []column{{"cmd", fg}}})
	mm := model{items: items, selectFirst: true, hasPanes: true, hasCommands: true}
	mm.filter()
	var m tea.Model = mm
	m, _ = m.Update(runes("c"))
	got := m.(model)
	if len(got.matches) != 1 || got.matches[0].item.id != "cmd" {
		t.Errorf("c should keep only commands, got %d matches", len(got.matches))
	}
	m, _ = m.Update(runes("a"))
	if got := m.(model); len(got.matches) != len(items) {
		t.Errorf("a should show all, got %d", len(got.matches))
	}
}

func TestHelp(t *testing.T) {
	tests := []struct {
		name string
		m    model
		want string
	}{
		{"panes", model{hasPanes: true}, "/ search  b/w/i/d filter  a all  ←/→ workspace  enter select  q close"},
		{"commands", model{hasCommands: true}, "/ search  enter select  q close"},
		{"both", model{hasPanes: true, hasCommands: true}, "/ search  b/w/i/d/c filter  a all  ←/→ workspace  enter select  q close"},
		{"searching from select", model{hasPanes: true, selectFirst: true, searching: true}, "esc done  ctrl-n/p move  enter select"},
		{"searching from search", model{hasPanes: true, searching: true}, "esc close  ctrl-n/p move  enter select"},
	}
	for _, tt := range tests {
		if got := strings.Join(tt.m.help(), "  "); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
	// c is not in the hints, so it does nothing
	mm := model{items: gotoItems(), selectFirst: true, hasPanes: true}
	mm.filter()
	var m tea.Model = mm
	m, _ = m.Update(runes("c"))
	if got := m.(model); got.narrow != "" {
		t.Errorf("c should be ignored without commands, narrow = %q", got.narrow)
	}
}

func TestLoadConfigFallback(t *testing.T) {
	cfg, err := loadConfig(t.TempDir() + "/missing.toml")
	if err != nil || len(cfg.Commands) == 0 || cfg.Theme != "catppuccin" {
		t.Errorf("missing file should fall back to the default config, got %+v, %v", cfg, err)
	}
}

func TestApplyTheme(t *testing.T) {
	defer applyTheme("catppuccin", palette{})
	if err := applyTheme("ansi", palette{Accent: "#123456"}); err != nil {
		t.Fatal(err)
	}
	if accent != "#123456" || red != "1" {
		t.Errorf("accent = %q, red = %q", accent, red)
	}
	if err := applyTheme("nope", palette{}); err == nil {
		t.Error("want error for unknown theme")
	}
}
