package main

// A profile chooses the items, the starting mode, the cursor position and the order.
// Profiles are [profiles.<name>] tables in config.toml; default_config.toml defines the built-in profiles.
// herdr cannot pass arguments to a plugin pane, so the profile is picked by environment:
// LAUNCHER_PROFILE (herdr plugin pane open --env LAUNCHER_PROFILE=<name>), else the pane that was opened.

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type options struct {
	start   string   // search: start typing right away / select: move with j/k and press / to search (like herdr's goto)
	sources []string // what to list: commands, panes, workspaces
	cursor  string   // initial cursor: top, or current (the focused pane or workspace)
	sort    string   // order when not searching: herdr, name, frecency
	width   string   // popup size when a plugin action opens it: "80%" or a number of cells
	height  string
}

type profileConfig struct {
	Start   string   `toml:"start"`
	Sources []string `toml:"sources"`
	Cursor  string   `toml:"cursor"`
	Sort    string   `toml:"sort"`
	Width   string   `toml:"width"`
	Height  string   `toml:"height"`
}

// herdr's popup sizes: a percentage of the terminal from 1% to 100%, or a number of terminal cells.
var popupSizePattern = regexp.MustCompile(`^((100|[1-9][0-9]?)%|[0-9]+)$`)

// The size as herdr's plugin.pane.open takes it: the percentage string, or the number of cells.
func popupSize(s string) any {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

func profileName() string {
	for _, key := range []string{"LAUNCHER_PROFILE", "HERDR_PLUGIN_ENTRYPOINT_ID"} {
		if name := os.Getenv(key); name != "" {
			return name
		}
	}
	return "launcher"
}

// Builds the options for the named profile. Fields the user's profile leaves out come from the default
// profile of the same name, so a config.toml only needs the fields it changes.
func resolveOptions(name string, cfg config) (options, error) {
	def, inDefaults := defaults.Profiles[name]
	user, inUser := cfg.Profiles[name]
	if !inDefaults && !inUser {
		names := slices.Sorted(maps.Keys(defaults.Profiles))
		for n := range cfg.Profiles {
			if !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		return options{}, fmt.Errorf("profile %q is not defined; defined profiles: %s", name, strings.Join(names, ", "))
	}
	opts := options{start: "select", sources: []string{"commands", "panes"}, cursor: "top", sort: "herdr", width: "70%", height: "70%"}
	for _, p := range []profileConfig{def, user} {
		opts.start = cmp.Or(p.Start, opts.start)
		opts.cursor = cmp.Or(p.Cursor, opts.cursor)
		opts.sort = cmp.Or(p.Sort, opts.sort)
		opts.width = cmp.Or(p.Width, opts.width)
		opts.height = cmp.Or(p.Height, opts.height)
		if len(p.Sources) > 0 {
			opts.sources = p.Sources
		}
	}
	checks := []struct {
		field  string
		values []string
		allow  []string
	}{
		{"start", []string{opts.start}, []string{"search", "select"}},
		{"sources", opts.sources, []string{"commands", "panes", "workspaces"}},
		{"cursor", []string{opts.cursor}, []string{"top", "current"}},
		{"sort", []string{opts.sort}, []string{"herdr", "name", "frecency"}},
	}
	for _, c := range checks {
		for _, v := range c.values {
			if !slices.Contains(c.allow, v) {
				return options{}, fmt.Errorf("profiles.%s.%s %q: use one of %s", name, c.field, v, strings.Join(c.allow, ", "))
			}
		}
	}
	for field, v := range map[string]string{"width": opts.width, "height": opts.height} {
		if !popupSizePattern.MatchString(v) {
			return options{}, fmt.Errorf("profiles.%s.%s %q: use a percentage such as \"80%%\" or a number of cells such as \"120\"", name, field, v)
		}
	}
	return opts, nil
}
