package main

// Colors. Chosen with theme in the config file and overridable per color under [colors].

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
)

// Colors are #rrggbb or a terminal 16/256 color number such as "4". Empty means the terminal default.
type palette struct {
	Fg          string `toml:"fg"`           // regular text
	Subtle      string `toml:"subtle"`       // secondary text such as tab names and command descriptions
	Dim         string `toml:"dim"`          // dividers and supplementary text
	Faint       string `toml:"faint"`        // the least prominent text, such as key hints
	SelectionBg string `toml:"selection_bg"` // background of the selected row
	CursorFg    string `toml:"cursor_fg"`    // text under the prompt cursor
	Accent      string `toml:"accent"`       // matched characters
	Caret       string `toml:"caret"`        // the > of the prompt and the selected row
	Green       string `toml:"green"`        // command and idle
	Teal        string `toml:"teal"`         // done
	Yellow      string `toml:"yellow"`       // working
	Red         string `toml:"red"`          // blocked
	Orange      string `toml:"orange"`       // claude
	Blue        string `toml:"blue"`         // codex
}

var themes = map[string]palette{
	// Matches herdr's default theme.
	"catppuccin": {
		Fg: "#cdd6f4", Subtle: "#a6adc8", Dim: "#6c7086", Faint: "#585b70",
		SelectionBg: "#313244", CursorFg: "#1e1e2e", Accent: "#89b4fa", Caret: "#f2cdcd",
		Green: "#a6e3a1", Teal: "#94e2d5", Yellow: "#f9e2af", Red: "#f38ba8", Orange: "#fab387", Blue: "#89b4fa",
	},
	// Leaves colors to the terminal.
	"ansi": {
		Fg: "", Subtle: "7", Dim: "8", Faint: "8",
		SelectionBg: "8", CursorFg: "0", Accent: "4", Caret: "5",
		Green: "2", Teal: "6", Yellow: "3", Red: "1", Orange: "3", Blue: "4",
	},
}

var (
	fg, subtle, dim, faint, selectionBg, cursorFg, accent, caretColor color.Color
	green, teal, yellow, red, orange, blue                            color.Color

	titleStyle, dimStyle lipgloss.Style
)

func init() { usePalette(themes["catppuccin"]) }

// Uses theme name with the non-empty colors of overrides on top.
func applyTheme(name string, overrides palette) error {
	if name == "" {
		name = "catppuccin"
	}
	p, ok := themes[name]
	if !ok {
		names := make([]string, 0, len(themes))
		for n := range themes {
			names = append(names, n)
		}
		slices.Sort(names)
		return fmt.Errorf("unknown theme %q: use one of %s", name, strings.Join(names, ", "))
	}
	for _, c := range []struct {
		dst *string
		src string
	}{
		{&p.Fg, overrides.Fg}, {&p.Subtle, overrides.Subtle}, {&p.Dim, overrides.Dim}, {&p.Faint, overrides.Faint},
		{&p.SelectionBg, overrides.SelectionBg}, {&p.CursorFg, overrides.CursorFg}, {&p.Accent, overrides.Accent},
		{&p.Caret, overrides.Caret}, {&p.Green, overrides.Green}, {&p.Teal, overrides.Teal}, {&p.Yellow, overrides.Yellow},
		{&p.Red, overrides.Red}, {&p.Orange, overrides.Orange}, {&p.Blue, overrides.Blue},
	} {
		if c.src != "" {
			*c.dst = c.src
		}
	}
	usePalette(p)
	return nil
}

func usePalette(p palette) {
	fg, subtle, dim, faint = lipgloss.Color(p.Fg), lipgloss.Color(p.Subtle), lipgloss.Color(p.Dim), lipgloss.Color(p.Faint)
	selectionBg, cursorFg, accent, caretColor = lipgloss.Color(p.SelectionBg), lipgloss.Color(p.CursorFg), lipgloss.Color(p.Accent), lipgloss.Color(p.Caret)
	green, teal, yellow, red, orange, blue = lipgloss.Color(p.Green), lipgloss.Color(p.Teal), lipgloss.Color(p.Yellow), lipgloss.Color(p.Red), lipgloss.Color(p.Orange), lipgloss.Color(p.Blue)
	titleStyle = lipgloss.NewStyle().Foreground(fg)
	dimStyle = lipgloss.NewStyle().Foreground(dim)
}
