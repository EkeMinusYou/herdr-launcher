package main

// commands.toml に書いたコマンドをランチャーの候補にする

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/ansi"
)

type commandConfig struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Command     string `toml:"command"`
	Pane        string `toml:"pane"`
}

// プラグインのディレクトリの commands.toml。HERDR_PLUGIN_ROOT がなければ bin/launcher の 1 つ上
func commandsPath() string {
	root := os.Getenv("HERDR_PLUGIN_ROOT")
	if root == "" {
		if self, err := os.Executable(); err == nil {
			root = filepath.Dir(filepath.Dir(self))
		}
	}
	return filepath.Join(root, "commands.toml")
}

func loadCommands(path string) ([]item, error) {
	var cfg struct {
		Commands []commandConfig `toml:"commands"`
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, err
	}
	// 名前の列幅を揃え、説明をその後ろに並べる
	nameWidth := 0
	for _, c := range cfg.Commands {
		nameWidth = max(nameWidth, ansi.StringWidth(c.Name))
	}
	var items []item
	for i, c := range cfg.Commands {
		if c.Name == "" || (c.Command == "") == (c.Pane == "") {
			return nil, fmt.Errorf("%s: commands[%d] には name と、command か pane のどちらか一方が必要です", path, i)
		}
		it := item{
			id:      "command:" + c.Name,
			kind:    "command",
			title:   c.Name,
			columns: []column{kindColumn("command"), {pad(c.Name, nameWidth) + "  ", text}, {c.Description, subtext0}},
			desc:    c.Description,
		}
		if c.Pane != "" {
			it.desc += "\npane: " + c.Pane
			it.run = func() error { return openPane(c.Pane) }
		} else {
			it.command = c.Command
			it.run = func() error { return runCommand("sh", "-c", c.Command) }
		}
		items = append(items, it)
	}
	return items, nil
}
