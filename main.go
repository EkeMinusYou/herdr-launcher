// launcher picks a command or a pane to jump to in a herdr popup and runs it in the cwd of the pane it was opened from.
// A profile in config.toml decides what is listed and how it is picked (options.go).
package main

import (
	"bufio"
	"cmp"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

type item struct {
	id      string // key for the preview cache
	kind    string // command, terminal, or an agent name such as claude
	status  string // herdr's agent_status; empty for commands and terminals
	title   string // heading of the preview
	columns []column
	extra   string // searchable text that is not displayed (branch and path)
	group   string // unit for jumping to the previous/next workspace
	focused bool
	desc    string
	command string                 // shell command; empty for panes
	run     func() error           // runs once the picker has closed; nil for commands that run inside it
	preview func() (string, error) // screen shown in the preview; nil shows none

	closeOnSuccess bool // for a command run inside the picker: close the picker once it succeeds
}

// One column of a list row. text is padded to the column width so columns line up.
type column struct {
	text  string
	color color.Color
}

func (it item) label() string {
	var b strings.Builder
	for _, c := range it.columns {
		b.WriteString(c.text)
	}
	return b.String()
}

// Starts with the displayed label, so match positions map directly onto the row for highlighting.
func (it item) searchText() string { return it.label() + " " + it.extra }

const kindWidth = 8

// Pads s with spaces to width, counting display width (wide characters count as 2).
func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// Aligns where the last column starts across all items, such as command descriptions and pane names,
// by padding the column before it.
func alignLastColumn(items []item) []item {
	prefixWidth := func(it item) int {
		w := 0
		for _, c := range it.columns[:len(it.columns)-1] {
			w += ansi.StringWidth(c.text)
		}
		return w
	}
	target := 0
	for _, it := range items {
		target = max(target, prefixWidth(it))
	}
	for i, it := range items {
		if len(it.columns) < 2 {
			continue
		}
		cols := append([]column(nil), it.columns...)
		before := &cols[len(cols)-2]
		before.text += strings.Repeat(" ", target-prefixWidth(it))
		items[i].columns = cols
	}
	return items
}

func kindColumn(kind string) column { return column{pad(kind, kindWidth) + "  ", kindColor(kind)} }

func main() {
	// Started by afterClose once the launcher has closed.
	// Nothing is on screen by then, so failures go to the log.
	if len(os.Args) == 5 && os.Args[1] == "open-pane" {
		if err := openPaneAfterClose(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			logError(fmt.Errorf("open pane %s of %s: %w", os.Args[3], os.Args[2], err))
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "focus-pane" {
		waitForClose()
		if err := focusPane(os.Args[2]); err != nil {
			logError(fmt.Errorf("focus pane %s: %w", os.Args[2], err))
			os.Exit(1)
		}
		return
	}

	var (
		chosen *item
		items  []item
		opts   options
		hist   = loadHistory(historyPath())
	)
	// Records a pick for frecency. Commands that run inside the picker are recorded when they start.
	record := func(id string) {
		path := historyPath()
		if path == "" {
			return
		}
		listed := map[string]bool{}
		for _, it := range items {
			listed[it.id] = true
		}
		logError(hist.record(path, id, time.Now(), listed, opts.sources))
	}
	err := rebuildIfStale()
	if err == nil {
		chdirToFocusedPane()
		var cfg config
		if cfg, err = loadConfig(configPath()); err == nil {
			err = applyTheme(cfg.Theme, cfg.Colors)
		}
		if err == nil {
			opts, err = resolveOptions(profileName(), cfg)
		}
		if err == nil {
			items, err = collect(opts, cfg, hist)
		}
		if err == nil {
			chosen, err = pick(items, opts, record)
		}
	}
	if err == nil && chosen == nil {
		return
	}
	if err == nil {
		record(chosen.id)
		err = chosen.run()
	}
	if err != nil {
		// Keep the error readable before closing.
		fmt.Fprintf(os.Stderr, "\n%v\npress enter to close", err)
		bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
}

// Collects items from opts.sources and orders them by opts.sort, using h for frecency.
func collect(opts options, cfg config, h history) ([]item, error) {
	var items []item
	for _, source := range opts.sources {
		switch source {
		case "commands":
			commands, err := commandItems(cfg)
			if err != nil {
				return nil, err
			}
			items = append(items, commands...)
		case "panes":
			items = append(items, paneItems()...)
		}
	}
	switch opts.sort {
	case "name":
		slices.SortStableFunc(items, func(a, b item) int { return strings.Compare(strings.ToLower(a.title), strings.ToLower(b.title)) })
	case "frecency":
		now := time.Now()
		slices.SortStableFunc(items, func(a, b item) int { return cmp.Compare(h.score(b.id, now), h.score(a.id, now)) })
	}
	return alignLastColumn(items), nil
}

// Set with -ldflags "-X main.releaseVersion=..." for release builds, which are fetched prebuilt and never rebuild themselves.
var releaseVersion string

// plugin link does not build, so rebuild when a source file is newer than the binary and re-exec the new binary.
// The very first binary still has to be built by hand with go build.
// Windows cannot replace a running executable, so there it is rebuilt by hand.
func rebuildIfStale() error {
	if releaseVersion != "" || runtime.GOOS == "windows" {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return err
	}
	built, err := os.Stat(self)
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(self))
	stale := false
	for _, pattern := range []string{"*.go", "go.mod", "go.sum", "default_config.toml"} {
		files, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, f := range files {
			if info, err := os.Stat(f); err == nil && info.ModTime().After(built.ModTime()) {
				stale = true
			}
		}
	}
	if !stale {
		return nil
	}
	cmd := exec.Command("go", "build", "-o", self, ".")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}
	return reexec(self)
}

// Plugin panes start in the plugin directory; move to the cwd of the pane the launcher was opened from so commands run there.
func chdirToFocusedPane() {
	if cwd := launchContext.FocusedPaneCwd; cwd != "" {
		os.Chdir(cwd)
	}
}

// The plugin id passed by herdr. The environment is inherited by processes started with afterClose.
func pluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return "ekeminusyou.launcher"
}

// Runs an interactive command in the launcher's terminal after the picker has closed.
func runInTerminal(command string) error {
	cmd := shellCommand(command)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Only one popup can be open at a time, and an overlay restores the previous focus when it closes.
// To work with both, opening another plugin pane or focusing a pane happens after the launcher has closed.
// Closing stops the popup's processes, so hand the work to a detached copy of this binary.
func afterClose(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return startDetached(self, args...)
}

func openPane(plugin, entrypoint string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return afterClose("open-pane", plugin, entrypoint, cwd)
}

// As an overlay, HERDR_PANE_ID is the launcher's own pane; wait for it to go away (it is empty for popups).
func waitForClose() {
	self := os.Getenv("HERDR_PANE_ID")
	if self == "" {
		return
	}
	for range 60 {
		if socketCall("pane.get", map[string]string{"pane_id": self}, nil) != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func openPaneAfterClose(plugin, entrypoint, cwd string) error {
	waitForClose()
	var err error
	// Only one popup can be open at a time, so retry for a while if it cannot open yet.
	for range 20 {
		params := map[string]any{"plugin_id": plugin, "entrypoint": entrypoint, "cwd": cwd, "focus": true}
		if err = socketCall("plugin.pane.open", params, nil); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}
