package main

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// typed text, as a terminal delivers a printable key (or a paste of several characters)
func runes(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}
func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }
func ctrl(r rune) tea.KeyPressMsg   { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
func alt(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Mod: tea.ModAlt}
}

func TestEditQuery(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyPressMsg
		query string
		pos   int
	}{
		{"type", []tea.KeyPressMsg{runes("gh"), runes(" "), runes("pr")}, "gh pr", 5},
		{"ctrl+h", []tea.KeyPressMsg{runes("gh"), ctrl('h')}, "g", 1},
		{"ctrl+a then insert", []tea.KeyPressMsg{runes("pr"), ctrl('a'), runes("gh ")}, "gh pr", 3},
		{"ctrl+b ctrl+d", []tea.KeyPressMsg{runes("abc"), ctrl('b'), ctrl('b'), ctrl('d')}, "ac", 1},
		{"ctrl+f ctrl+e", []tea.KeyPressMsg{runes("abc"), ctrl('a'), ctrl('f'), ctrl('e')}, "abc", 3},
		{"ctrl+w", []tea.KeyPressMsg{runes("gh pr view"), ctrl('w')}, "gh pr ", 6},
		{"ctrl+w twice", []tea.KeyPressMsg{runes("gh pr view"), ctrl('w'), ctrl('w')}, "gh ", 3},
		{"ctrl+u", []tea.KeyPressMsg{runes("gh pr"), ctrl('b'), ctrl('u')}, "r", 0},
		{"ctrl+k", []tea.KeyPressMsg{runes("gh pr"), ctrl('a'), ctrl('f'), ctrl('k')}, "g", 1},
		{"alt+b alt+d", []tea.KeyPressMsg{runes("gh pr view"), alt("b"), alt("b"), alt("d")}, "gh  view", 3},
		{"alt+f", []tea.KeyPressMsg{runes("gh pr"), ctrl('a'), alt("f")}, "gh pr", 2},
		{"bounds", []tea.KeyPressMsg{ctrl('h'), ctrl('d'), ctrl('b'), ctrl('f')}, "", 0},
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
		keys   []tea.KeyPressMsg
		cursor int
		quit   bool
	}{
		{"ctrl+n ctrl+p", []tea.KeyPressMsg{ctrl('n'), ctrl('n'), ctrl('p')}, 1, false},
		{"down up", []tea.KeyPressMsg{key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyUp)}, 1, false},
		{"bounds", []tea.KeyPressMsg{ctrl('p')}, 0, false},
		{"esc", []tea.KeyPressMsg{key(tea.KeyEsc)}, 0, true},
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
		keys  []tea.KeyPressMsg
		query string
		ids   string // items left after filtering
		at    string // selected item
		quit  bool
	}{
		{"j k", []tea.KeyPressMsg{runes("j"), runes("j"), runes("k")}, "", "a1 a2 b1 b2 c1", "a2", false},
		{"right", []tea.KeyPressMsg{key(tea.KeyRight)}, "", "a1 a2 b1 b2 c1", "b1", false},
		{"right right right", []tea.KeyPressMsg{key(tea.KeyRight), key(tea.KeyRight), key(tea.KeyRight)}, "", "a1 a2 b1 b2 c1", "c1", false},
		{"left from middle", []tea.KeyPressMsg{runes("j"), runes("j"), runes("j"), key(tea.KeyLeft)}, "", "a1 a2 b1 b2 c1", "a1", false},
		{"left at first", []tea.KeyPressMsg{runes("j"), key(tea.KeyLeft)}, "", "a1 a2 b1 b2 c1", "a2", false},
		{"filter working", []tea.KeyPressMsg{runes("w")}, "", "a1 c1", "a1", false},
		{"filter blocked then all", []tea.KeyPressMsg{runes("b"), runes("a")}, "", "a1 a2 b1 b2 c1", "a1", false},
		// after /, j and w are typed into the search
		{"search", []tea.KeyPressMsg{runes("/"), runes("b")}, "b", "b1 b2", "b1", false},
		{"search then esc then j", []tea.KeyPressMsg{runes("/"), runes("b"), key(tea.KeyEsc), runes("j")}, "b", "b1 b2", "b2", false},
		{"esc", []tea.KeyPressMsg{key(tea.KeyEsc)}, "", "a1 a2 b1 b2 c1", "a1", true},
		{"q", []tea.KeyPressMsg{runes("q")}, "", "a1 a2 b1 b2 c1", "a1", true},
		// while searching, q is typed into the search instead of closing
		{"search q", []tea.KeyPressMsg{runes("/"), runes("q")}, "q", "", "", false},
		{"search esc esc", []tea.KeyPressMsg{runes("/"), key(tea.KeyEsc), key(tea.KeyEsc)}, "", "a1 a2 b1 b2 c1", "a1", true},
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

func TestResolveOptions(t *testing.T) {
	defaults, err := loadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	opts, err := resolveOptions("goto", defaults)
	if err != nil {
		t.Fatal(err)
	}
	if opts.start != "select" || strings.Join(opts.sources, ",") != "panes" || opts.cursor != "current" || opts.sort != "herdr" {
		t.Errorf("goto: got %+v", opts)
	}

	// A user's profile only changes the fields it sets, and new profiles can be added.
	path := t.TempDir() + "/config.toml"
	os.WriteFile(path, []byte("[profiles.goto]\nsort = \"frecency\"\n\n[profiles.agents]\nsources = [\"panes\"]\nstart = \"search\"\n"), 0o644)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if opts, _ := resolveOptions("goto", cfg); opts.sort != "frecency" || opts.cursor != "current" || strings.Join(opts.sources, ",") != "panes" {
		t.Errorf("goto with override: got %+v", opts)
	}
	if opts, _ := resolveOptions("launcher", cfg); opts.sort != "frecency" || strings.Join(opts.sources, ",") != "commands,panes" {
		t.Errorf("launcher missing from config.toml should use the default: got %+v", opts)
	}
	if opts, err := resolveOptions("agents", cfg); err != nil || opts.start != "search" || opts.cursor != "top" || opts.sort != "herdr" {
		t.Errorf("agents: got %+v, %v", opts, err)
	}
	if _, err := resolveOptions("missing", cfg); err == nil || !strings.Contains(err.Error(), "agents") {
		t.Errorf("unknown profile: err = %v, want an error listing the defined profiles", err)
	}

	for _, body := range []string{"start = \"x\"", "sources = [\"commands\", \"x\"]", "cursor = \"x\"", "sort = \"x\""} {
		os.WriteFile(path, []byte("[profiles.goto]\n"+body+"\n"), 0o644)
		cfg, _ := loadConfig(path)
		if _, err := resolveOptions("goto", cfg); err == nil {
			t.Errorf("%s: want error", body)
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
	if accent != lipgloss.Color("#123456") || red != lipgloss.Color("1") {
		t.Errorf("accent = %q, red = %q", accent, red)
	}
	if err := applyTheme("nope", palette{}); err == nil {
		t.Error("want error for unknown theme")
	}
}

func TestSearch(t *testing.T) {
	mk := func(label string) item { return item{id: label, columns: []column{{label, fg}}} }
	items := []item{mk("gh pr view --web"), mk("gh browse"), mk("lazygit"), mk("Hydra console"), mk("hydra login")}
	tests := []struct {
		query string
		want  string // ids in order
	}{
		{"pr gh", "gh pr view --web"},          // terms match in any order
		{"lzg", "lazygit"},                     // fuzzy
		{"hydra", "hydra login Hydra console"}, // lowercase is case-insensitive; shorter first on a tie
		{"Hydra", "Hydra console"},             // uppercase makes it case-sensitive
		{"hydra !console", "hydra login"},      // ! excludes
		{"'git", "lazygit"},                    // ' matches exactly
		{"^gh", "gh browse gh pr view --web"},  // ^ matches the start
		{"web$", "gh pr view --web"},           // $ matches the end
		{"gh zzz", ""},                         // every term has to match
	}
	for _, tt := range tests {
		var ids []string
		for _, m := range search(items, tt.query) {
			ids = append(ids, m.item.id)
		}
		if got := strings.Join(ids, " "); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.query, got, tt.want)
		}
	}
	// highlight offsets are byte offsets into the label, also for multibyte text
	ms := search([]item{mk("日本 gh")}, "gh")
	if len(ms) != 1 || len(ms[0].indexes) != 2 || ms[0].indexes[0] != len("日本 ") {
		t.Errorf("multibyte highlight offsets = %v", ms)
	}
}

func TestPluginPaneCommand(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	os.WriteFile(path, []byte("[[commands]]\nname = \"neovim\"\nplugin = \"neovim\"\npane = \"nvim\"\n"), 0o644)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	items, err := commandItems(cfg)
	if err != nil || len(items) != 1 || !strings.Contains(items[0].desc, "pane: neovim / nvim") {
		t.Errorf("items = %+v, err = %v", items, err)
	}
	os.WriteFile(path, []byte("[[commands]]\nname = \"x\"\nplugin = \"neovim\"\ncommand = \"ls\"\n"), 0o644)
	cfg, _ = loadConfig(path)
	if _, err := commandItems(cfg); err == nil {
		t.Error("plugin without pane should be an error")
	}
}
