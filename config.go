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
	Plugin      string `toml:"plugin"` // the plugin that owns pane; defaults to this plugin
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
// commands when it has no commands key, and each profile field it does not set (see resolveOptions).
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
	if !md.IsDefined("commands") {
		cfg.Commands = defaults.Commands
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
		if c.Plugin != "" && c.Pane == "" {
			return nil, fmt.Errorf("commands[%d]: plugin needs pane", i)
		}
		it := item{
			id:      "command:" + c.Name,
			kind:    "command",
			title:   c.Name,
			columns: []column{kindColumn("command"), {pad(c.Name, nameWidth) + "  ", fg}, {c.Description, subtle}},
			desc:    c.Description,
		}
		if c.Pane != "" {
			plugin := c.Plugin
			if plugin == "" {
				plugin = pluginID()
			}
			it.desc += "\npane: " + plugin + " / " + c.Pane
			it.run = func() error { return openPane(plugin, c.Pane) }
		} else {
			it.command = c.Command
			it.run = func() error { return runCommand("sh", "-c", c.Command) }
		}
		items = append(items, it)
	}
	return items, nil
}
