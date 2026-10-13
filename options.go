package main

// A profile chooses the items, the starting mode, the cursor position and the order.
// Profiles are [profiles.<name>] tables in config.toml; default_config.toml defines launcher and goto.
// herdr cannot pass arguments to a plugin pane, so the profile is picked by environment:
// LAUNCHER_PROFILE (herdr plugin pane open --env LAUNCHER_PROFILE=<name>), else the pane that was opened.

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

type options struct {
	start   string   // search: start typing right away / select: move with j/k and press / to search (like herdr's goto)
	sources []string // what to list: commands, panes
	cursor  string   // initial cursor: top, or current (the focused pane)
	sort    string   // order when not searching: herdr, name, frecency
}

type profileConfig struct {
	Start   string   `toml:"start"`
	Sources []string `toml:"sources"`
	Cursor  string   `toml:"cursor"`
	Sort    string   `toml:"sort"`
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
	var defaults config
	if _, err := toml.Decode(defaultConfig, &defaults); err != nil {
		return options{}, err
	}
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
	opts := options{start: "select", sources: []string{"commands", "panes"}, cursor: "top", sort: "herdr"}
	for _, p := range []profileConfig{def, user} {
		opts.start = cmp.Or(p.Start, opts.start)
		opts.cursor = cmp.Or(p.Cursor, opts.cursor)
		opts.sort = cmp.Or(p.Sort, opts.sort)
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
		{"sources", opts.sources, []string{"commands", "panes"}},
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
	return opts, nil
}
