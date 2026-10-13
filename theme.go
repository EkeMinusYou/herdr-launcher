package main

// Colors. Chosen with theme in the config file and overridable per color under [colors].

import (
	"fmt"
	"image/color"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Colors are #rrggbb or a terminal 16/256 color number such as "4". Empty means the terminal default.
type palette struct {
	Bg          string `toml:"bg"`           // background of the whole picker; "none" leaves the terminal's
	Fg          string `toml:"fg"`           // regular text
	Subtle      string `toml:"subtle"`       // secondary text such as tab names and command descriptions
	Dim         string `toml:"dim"`          // dividers and supplementary text
	Faint       string `toml:"faint"`        // the least prominent text, such as key hints
	SelectionBg string `toml:"selection_bg"` // background of the selected row
	CursorFg    string `toml:"cursor_fg"`    // text under the prompt cursor
	Accent      string `toml:"accent"`       // matched characters
	Caret       string `toml:"caret"`        // the > of the prompt and the selected row
	Green       string `toml:"green"`        // idle
	Mauve       string `toml:"mauve"`        // command
	Teal        string `toml:"teal"`         // done
	Yellow      string `toml:"yellow"`       // working
	Red         string `toml:"red"`          // blocked
	Orange      string `toml:"orange"`       // claude
	Blue        string `toml:"blue"`         // codex
}

var themes = map[string]palette{
	// Matches herdr's default theme.
	"catppuccin": {
		Bg: "#181825", // mantle, a step darker than herdr's background so the popup stands out
		Fg: "#cdd6f4", Subtle: "#a6adc8", Dim: "#6c7086", Faint: "#585b70",
		SelectionBg: "#313244", CursorFg: "#1e1e2e", Accent: "#89b4fa", Caret: "#f2cdcd",
		Green: "#a6e3a1", Mauve: "#cba6f7", Teal: "#94e2d5", Yellow: "#f9e2af", Red: "#f38ba8", Orange: "#fab387", Blue: "#89b4fa",
	},
	// Leaves colors to the terminal.
	"ansi": {
		Fg: "", Subtle: "7", Dim: "8", Faint: "8",
		SelectionBg: "8", CursorFg: "0", Accent: "4", Caret: "5",
		Green: "2", Mauve: "5", Teal: "6", Yellow: "3", Red: "1", Orange: "3", Blue: "4",
	},
}

var (
	fg, subtle, dim, faint, selectionBg, cursorFg, accent, caretColor color.Color
	green, mauve, teal, yellow, red, orange, blue                     color.Color

	titleStyle, dimStyle lipgloss.Style
	bgSeq                string // sets the picker's background; empty when it is not painted
)

func init() { usePalette(themes["catppuccin"]) }

// Uses theme name with the non-empty colors of overrides on top.
func applyTheme(name string, overrides palette) error {
	if name == "" {
		name = "catppuccin"
	}
	p, ok := themes[name]
	if !ok {
		names := slices.Sorted(maps.Keys(themes))
		return fmt.Errorf("unknown theme %q: use one of %s", name, strings.Join(names, ", "))
	}
	// Every palette field is a color string, so copy each one the overrides set.
	dst, src := reflect.ValueOf(&p).Elem(), reflect.ValueOf(overrides)
	for i := range src.NumField() {
		if c := src.Field(i).String(); c != "" {
			dst.Field(i).SetString(c)
		}
	}
	usePalette(p)
	return nil
}

func usePalette(p palette) {
	fg, subtle, dim, faint = lipgloss.Color(p.Fg), lipgloss.Color(p.Subtle), lipgloss.Color(p.Dim), lipgloss.Color(p.Faint)
	selectionBg, cursorFg, accent, caretColor = lipgloss.Color(p.SelectionBg), lipgloss.Color(p.CursorFg), lipgloss.Color(p.Accent), lipgloss.Color(p.Caret)
	green, mauve, teal, yellow, red, orange, blue = lipgloss.Color(p.Green), lipgloss.Color(p.Mauve), lipgloss.Color(p.Teal), lipgloss.Color(p.Yellow), lipgloss.Color(p.Red), lipgloss.Color(p.Orange), lipgloss.Color(p.Blue)
	titleStyle = lipgloss.NewStyle().Foreground(fg)
	dimStyle = lipgloss.NewStyle().Foreground(dim)
	bgSeq = ""
	if p.Bg != "" && p.Bg != "none" {
		bgSeq = ansi.Style{}.BackgroundColor(lipgloss.Color(p.Bg)).String()
	}
}

var sgrPattern = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// Paints the picker's background behind every cell of line: at the start, and again after each SGR
// sequence that resets the background, such as the resets that styled text and pane screens contain.
func withBackground(line string) string {
	if bgSeq == "" {
		return line
	}
	return bgSeq + sgrPattern.ReplaceAllStringFunc(line, func(seq string) string {
		if resetsBackground(seq[2 : len(seq)-1]) {
			return seq + bgSeq
		}
		return seq
	})
}

// Whether SGR parameters leave the background at the terminal default: a reset (empty or 0) or 49,
// not followed by a background of their own. The numbers inside 38/48/58 color values are skipped,
// so a color component such as 49 is not mistaken for one.
func resetsBackground(params string) bool {
	reset := false
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		switch p := ps[i]; {
		case p == "" || p == "0" || p == "49":
			reset = true
		case p == "38" || p == "48" || p == "58":
			if p == "48" {
				reset = false
			}
			// Skip the color value: 5;n or 2;r;g;b.
			if i+1 < len(ps) {
				switch ps[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		case strings.HasPrefix(p, "48:"):
			reset = false
		default:
			if n, err := strconv.Atoi(p); err == nil && (n >= 40 && n <= 47 || n >= 100 && n <= 107) {
				reset = false
			}
		}
	}
	return reset
}
