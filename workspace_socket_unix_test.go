//go:build !windows

package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkspaceSocketFlow(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "launcher-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "herdr.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	type request struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	requests := make(chan request, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var req request
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				conn.Close()
				return
			}
			requests <- req
			var result any = map[string]any{}
			switch req.Method {
			case "session.snapshot":
				result = map[string]any{"snapshot": map[string]any{
					"focused_workspace_id": "w1",
					"workspaces": []map[string]any{
						{"workspace_id": "w1", "number": 1, "label": "first", "active_tab_id": "t1"},
						{"workspace_id": "w2", "number": 2, "label": "second", "active_tab_id": "t2"},
					},
					"tabs":    []map[string]any{{"tab_id": "t2", "workspace_id": "w2", "label": "server"}},
					"panes":   []map[string]any{{"pane_id": "w2:p1", "tab_id": "t2", "workspace_id": "w2", "label": "api"}},
					"layouts": []map[string]any{{"tab_id": "t2", "focused_pane_id": "w2:p1"}},
				}}
			case "pane.read":
				result = map[string]any{"read": map[string]any{"text": "live screen\r\n"}}
			}
			json.NewEncoder(conn).Encode(map[string]any{"id": "launcher", "result": result})
			conn.Close()
		}
	}()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	t.Setenv("HERDR_PANE_ID", "")
	t.Setenv("HERDR_PLUGIN_ID", "test.launcher")
	configDir := t.TempDir()
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", configDir)
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[profiles.goto-workspace]\nwidth = \"81%\"\nheight = \"27\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldContext := launchContext
	launchContext = pluginContext{}
	t.Cleanup(func() { launchContext = oldContext })
	cfg, err := loadConfig(configPath())
	if err != nil {
		t.Fatal(err)
	}
	opts, err := resolveOptions("goto-workspace", cfg)
	if err != nil {
		t.Fatal(err)
	}
	items, err := collect(opts, cfg, history{})
	if err != nil || len(items) != 2 || !items[0].focused || items[1].preview == nil {
		t.Fatalf("collect: items=%+v, err=%v", items, err)
	}
	if preview, err := items[1].preview(); err != nil || preview != "live screen\n" {
		t.Fatalf("preview = %q, err = %v", preview, err)
	}
	if err := focusWorkspace("w2"); err != nil {
		t.Fatal(err)
	}
	if err := openPicker("goto-workspace"); err != nil {
		t.Fatal(err)
	}
	want := []request{
		{Method: "session.snapshot", Params: map[string]any{}},
		{Method: "pane.read", Params: map[string]any{"pane_id": "w2:p1", "source": "visible", "format": "ansi"}},
		{Method: "workspace.focus", Params: map[string]any{"workspace_id": "w2"}},
		{Method: "plugin.pane.open", Params: map[string]any{"plugin_id": "test.launcher", "entrypoint": "goto-workspace", "focus": true, "width": "81%", "height": float64(27)}},
	}
	for _, expected := range want {
		if got := <-requests; !reflect.DeepEqual(got, expected) {
			t.Errorf("request = %+v, want %+v", got, expected)
		}
	}
}
