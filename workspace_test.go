package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"
)

func TestWorkspaceItems(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/feature/workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := sessionSnapshot{
		FocusedWorkspaceID: "w2",
		Workspaces: []workspace{
			{ID: "w2", Label: "project", Focused: true, ActiveTabID: "t2", TabCount: 1, PaneCount: 2, AgentStatus: "working"},
			{ID: "w1", Label: "project", ActiveTabID: "t1", TabCount: 1, PaneCount: 2, AgentStatus: "blocked"},
			{ID: "w3", Label: "empty", TabCount: 0, PaneCount: 0},
		},
		Tabs: []tab{{ID: "t1", WorkspaceID: "w1", Label: "dev"}, {ID: "t2", WorkspaceID: "w2", Label: "server"}},
		Panes: []pane{
			{ID: "other", WorkspaceID: "w2", TabID: "t2", Label: "other"},
			{ID: "active", WorkspaceID: "w2", TabID: "t2", Label: "api", Cwd: dir},
			{ID: "underlying", WorkspaceID: "w1", TabID: "t1", Agent: "codex", Title: "fix bug", Cwd: dir},
			{ID: "overlay", WorkspaceID: "w1", TabID: "t1"},
		},
		Layouts: []paneLayout{{TabID: "t1", FocusedPaneID: "overlay"}, {TabID: "t2", FocusedPaneID: "active"}},
	}
	items := itemsFromWorkspaces(snapshot, pluginContext{WorkspaceID: "w1", FocusedPaneID: "underlying"}, "overlay")
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	if items[0].id != "workspace:w2" || items[1].id != "workspace:w1" || items[2].id != "workspace:w3" {
		t.Fatalf("workspace order or ids are wrong: %+v", items)
	}
	if items[0].focused || !items[1].focused || items[2].focused {
		t.Errorf("the invocation workspace should be current: %+v", items)
	}
	if items[0].label() != "project  feature/workspace         api" || items[1].label() != "project  feature/workspace  codex  fix bug" || items[2].label() != "empty" {
		t.Errorf("workspace rows should show available branch and active pane details: %q, %q, %q", items[0].label(), items[1].label(), items[2].label())
	}
	if items[0].preview == nil || items[1].preview == nil || items[2].preview != nil {
		t.Error("preview should exist only when the active tab has a usable pane")
	}
	for _, want := range []string{"tabs: 1, panes: 1 (current)", "dev › fix bug", "branch: feature/workspace"} {
		if !strings.Contains(items[1].desc, want) {
			t.Errorf("current workspace description %q does not contain %q", items[1].desc, want)
		}
	}
	if !strings.Contains(items[0].desc, "server › api") || strings.Contains(items[0].desc, "other") {
		t.Errorf("preview must describe the active tab's focused pane: %q", items[0].desc)
	}
	for _, query := range []string{"feature/workspace", "fix bug", "dev"} {
		matches := search(items, query)
		found := false
		for _, match := range matches {
			found = found || match.item.id == "workspace:w1"
		}
		if !found {
			t.Errorf("search %q did not find the workspace", query)
		}
	}

	// The focused pane in the invocation context is enough to identify the current workspace.
	items = itemsFromWorkspaces(snapshot, pluginContext{FocusedPaneID: "underlying"}, "overlay")
	if !items[1].focused || items[0].focused {
		t.Error("did not derive the current workspace from the invocation pane")
	}
	items = itemsFromWorkspaces(snapshot, pluginContext{}, "")
	if !items[0].focused || items[1].focused {
		t.Error("did not fall back to the snapshot's focused workspace")
	}
	snapshot.FocusedWorkspaceID = ""
	items = itemsFromWorkspaces(snapshot, pluginContext{}, "")
	if !items[0].focused {
		t.Error("did not fall back to the workspace's focused flag")
	}
}

func TestWorkspaceColumns(t *testing.T) {
	items := alignWorkspaceColumns([]item{
		{kind: "workspace", columns: []column{{"api", fg}, {"main", subtle}, {"codex", blue}, {"deploy", fg}}},
		{kind: "workspace", columns: []column{{"開発", fg}, {"修正", subtle}, {"", subtle}, {"ログ", fg}}},
		{kind: "workspace", columns: []column{{"x", fg}, {"", subtle}, {"claude", orange}, {"review", fg}}},
	})
	for col := 1; col < 4; col++ {
		start := func(it item) int {
			width := 0
			for _, c := range it.columns[:col] {
				width += ansi.StringWidth(c.text)
			}
			return width
		}
		for i := 1; i < len(items); i++ {
			if got, want := start(items[i]), start(items[0]); got != want {
				t.Errorf("column %d starts at %d in row %d, want %d", col, got, i, want)
			}
		}
	}
	before := items[0].label()
	items = append(items, item{kind: "terminal", columns: []column{{strings.Repeat("x", 80), fg}, {"pane", fg}}})
	items = alignLastColumn(items)
	if items[0].label() != before {
		t.Error("mixed pane and workspace profiles must preserve workspace column alignment")
	}
}

func TestWorkspacePickerProfileAndNavigation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[profiles.goto-workspace]\nwidth = \"80%\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := resolveOptions("goto-workspace", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if opts.start != "select" || strings.Join(opts.sources, ",") != "workspaces" || opts.cursor != "current" || opts.sort != "herdr" || opts.width != "80%" {
		t.Fatalf("unexpected workspace profile: %+v", opts)
	}
	items := []item{
		{id: "workspace:w1", kind: "workspace", status: "working", columns: []column{{"first", fg}}},
		{id: "workspace:w2", kind: "workspace", status: "blocked", focused: true, columns: []column{{"second", fg}}},
	}
	m := newModel(items, opts, nil)
	if m.cursor != 1 || strings.Contains(strings.Join(m.help(), " "), "←/→") {
		t.Fatalf("workspace picker should start on the current workspace without pane group navigation: %+v", m)
	}
	m.navigate(runes("w"))
	if len(m.matches) != 1 || m.matches[0].item.id != "workspace:w1" {
		t.Fatalf("status filter did not select the working workspace: %+v", m.matches)
	}
	m.navigate(runes("a"))
	m.navigate(runes("/"))
	m.editQuery(runes("second"))
	if len(m.matches) != 1 || m.matches[0].item.id != "workspace:w2" {
		t.Fatalf("workspace search failed: %+v", m.matches)
	}
	chosen, quit := press(m, key(tea.KeyEnter))
	if !quit || chosen.chosen == nil || chosen.chosen.id != "workspace:w2" {
		t.Errorf("enter did not select the workspace: %+v, quit %v", chosen.chosen, quit)
	}

	if err := os.WriteFile(path, []byte("[profiles.mixed]\nsources = [\"commands\", \"workspaces\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	opts, err = resolveOptions("mixed", cfg)
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, item{id: "command:run", kind: "command", columns: []column{{"run", fg}}})
	m = newModel(items, opts, nil)
	m.navigate(runes("c"))
	if len(m.matches) != 1 || m.matches[0].item.id != "command:run" {
		t.Errorf("command filter failed in a mixed workspace profile: %+v", m.matches)
	}
}

func TestWorkspaceHistoryPruning(t *testing.T) {
	now := time.Now()
	old := now.Add(-100 * 24 * time.Hour).Unix()
	h := history{"w1:p1": {Rank: 1, Last: old}, "workspace:w1": {Rank: 1, Last: old}, "command:old": {Rank: 1, Last: old}}
	h.forgetStale(now, nil, []string{"panes"})
	if _, ok := h["w1:p1"]; ok {
		t.Error("stale pane should be removed when panes were loaded")
	}
	if _, ok := h["workspace:w1"]; !ok {
		t.Error("workspace history should survive a pane-only picker")
	}
	h.forgetStale(now, nil, []string{"workspaces"})
	if _, ok := h["workspace:w1"]; ok {
		t.Error("stale workspace should be removed when workspaces were loaded")
	}
	if _, ok := h["command:old"]; !ok {
		t.Error("command history should survive a workspace-only picker")
	}
}

func TestWorkspacePickerManifest(t *testing.T) {
	var manifest struct {
		Actions []struct {
			ID      string   `toml:"id"`
			Command []string `toml:"command"`
		} `toml:"actions"`
		Panes []struct {
			ID        string `toml:"id"`
			Placement string `toml:"placement"`
		} `toml:"panes"`
	}
	if _, err := toml.DecodeFile("herdr-plugin.toml", &manifest); err != nil {
		t.Fatal(err)
	}
	action, pane := false, false
	for _, a := range manifest.Actions {
		if a.ID == "goto-workspace" && strings.Join(a.Command, " ") == "./bin/launcher.exe open goto-workspace" {
			action = true
		}
	}
	for _, p := range manifest.Panes {
		if p.ID == "goto-workspace" && p.Placement == "popup" {
			pane = true
		}
	}
	if !action || !pane {
		t.Errorf("goto-workspace manifest entry: action %v, pane %v", action, pane)
	}
}
