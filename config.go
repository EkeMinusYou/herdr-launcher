package main

// Config file (colors, launcher commands and profiles). Reads config.toml in HERDR_PLUGIN_CONFIG_DIR
// and fills in what it leaves out from the embedded default_config.toml. User-editable settings do
// not live in the plugin directory because reinstalling the plugin replaces it.

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"
)

//go:embed default_config.toml
var defaultConfig string

type config struct {
	Theme    string                   `toml:"theme"`
	Colors   palette                  `toml:"colors"`
	Commands []commandConfig          `toml:"commands"`
	Profiles map[string]profileConfig `toml:"profiles"`
}

type commandConfig struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Command     string `toml:"command"`
	Pane        string `toml:"pane"`
	Plugin      string `toml:"plugin"`      // the plugin that owns pane
	Interactive bool   `toml:"interactive"` // run command in the terminal instead of showing its output in the preview
	Close       bool   `toml:"close"`       // close the launcher when command succeeds
}

func configPath() string {
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

// The embedded default_config.toml, parsed once.
var defaults = func() config {
	var cfg config
	if _, err := toml.Decode(defaultConfig, &cfg); err != nil {
		panic("default_config.toml: " + err.Error())
	}
	return cfg
}()

// Reads the user's config.toml. Without one, or for the parts it leaves out, the defaults apply:
// the theme, and each profile field it does not set (see resolveOptions). Commands come only from config.toml.
// Unknown keys are an error so that a typo does not silently do nothing.
func loadConfig(path string) (config, error) {
	if path == "" {
		return defaults, nil
	}
	var cfg config
	md, err := toml.DecodeFile(path, &cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return cfg, fmt.Errorf("%s: unknown keys: %s", path, strings.Join(keys, ", "))
	}
	return cfg, nil
}

func commandItems(cfg config) ([]item, error) {
	// Align the name column and put the description after it.
	nameWidth := 0
	for _, c := range cfg.Commands {
		nameWidth = max(nameWidth, ansi.StringWidth(c.Name))
	}
	var items []item
	for i, c := range cfg.Commands {
		if c.Name == "" || (c.Command == "") == (c.Pane == "") {
			return nil, fmt.Errorf("commands[%d]: set name and exactly one of command or pane", i)
		}
		if (c.Plugin == "") != (c.Pane == "") {
			return nil, fmt.Errorf("commands[%d]: set plugin and pane together", i)
		}
		if c.Interactive && c.Command == "" {
			return nil, fmt.Errorf("commands[%d]: interactive needs command", i)
		}
		if c.Close && (c.Command == "" || c.Interactive) {
			return nil, fmt.Errorf("commands[%d]: close needs a command that is not interactive", i)
		}
		it := item{
			id:      "command:" + c.Name,
			kind:    "command",
			title:   c.Name,
			columns: []column{kindColumn("command"), {pad(c.Name, nameWidth) + "  ", fg}, {c.Description, subtle}},
			desc:    c.Description,
		}
		if c.Pane != "" {
			it.desc += "\npane: " + c.Plugin + " / " + c.Pane
			it.run = func() error { return openPane(c.Plugin, c.Pane) }
		} else {
			// Shell commands run inside the picker (see run.go) unless they need the terminal.
			it.command, it.closeOnSuccess = c.Command, c.Close
			if c.Interactive {
				it.run = func() error { return runInTerminal(c.Command) }
			}
		}
		items = append(items, it)
	}
	return items, nil
}
