package main

// Command-line flags choose the items, the starting mode, the cursor position and the order.
// Each [[panes]] entry in the manifest passes different flags to build the launcher and goto.

import (
	"flag"
	"fmt"
	"slices"
	"strings"
)

type options struct {
	start   string   // search: start typing right away / select: move with j/k and press / to search (like herdr's goto)
	sources []string // what to list: commands, panes
	cursor  string   // initial cursor: top, or current (the focused pane)
	sort    string   // order when not searching: herdr, name, frecency
}

func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("launcher", flag.ContinueOnError)
	start := fs.String("start", "search", "search or select")
	sources := fs.String("sources", "commands,panes", "comma separated: commands, panes")
	cursor := fs.String("cursor", "top", "top or current")
	sort := fs.String("sort", "herdr", "herdr, name or frecency")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	opts := options{start: *start, sources: strings.Split(*sources, ","), cursor: *cursor, sort: *sort}
	checks := []struct {
		name   string
		values []string
		allow  []string
	}{
		{"--start", []string{opts.start}, []string{"search", "select"}},
		{"--sources", opts.sources, []string{"commands", "panes"}},
		{"--cursor", []string{opts.cursor}, []string{"top", "current"}},
		{"--sort", []string{opts.sort}, []string{"herdr", "name", "frecency"}},
	}
	for _, c := range checks {
		for _, v := range c.values {
			if !slices.Contains(c.allow, v) {
				return options{}, fmt.Errorf("%s %q: use one of %s", c.name, v, strings.Join(c.allow, ", "))
			}
		}
	}
	return opts, nil
}
