package main

// Config file (colors, launcher commands and profiles). Reads config.toml in HERDR_PLUGIN_CONFIG_DIR
// and falls back to the embedded default_config.toml. User-editable settings do not live in
// the plugin directory because reinstalling the plugin replaces it.

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

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

// Uses the default config when path is empty or the file does not exist.
func loadConfig(path string) (config, error) {
	var cfg config
	if path != "" {
		_, err := toml.DecodeFile(path, &cfg)
		if err == nil {
			return cfg, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return cfg, fmt.Errorf("%s: %w", path, err)
		}
	}
	_, err := toml.Decode(defaultConfig, &cfg)
	return cfg, err
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
