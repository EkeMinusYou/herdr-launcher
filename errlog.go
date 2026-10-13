package main

// Errors nobody would see otherwise, such as parts of the list that failed to load or the work done
// in the background after the popup closed, are appended to launcher.log in HERDR_PLUGIN_STATE_DIR.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const maxLogSize = 256 << 10 // start the log over once it grows past this

func logPath() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "launcher.log")
}

func logError(err error) {
	if err == nil {
		return
	}
	path := logPath()
	if path == "" {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, statErr := os.Stat(path); statErr == nil && info.Size() > maxLogSize {
		flags |= os.O_TRUNC
	}
	f, openErr := os.OpenFile(path, flags, 0o644)
	if openErr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), err)
}
