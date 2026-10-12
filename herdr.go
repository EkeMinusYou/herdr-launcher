package main

// Lists panes with the herdr CLI and focuses the chosen one with the socket API's pane.focus.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type workspace struct {
	ID    string `json:"workspace_id"`
	Label string `json:"label"`
}

type tab struct {
	ID          string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

type pane struct {
	ID          string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Agent       string `json:"agent"`
	AgentStatus string `json:"agent_status"`
	Title       string `json:"terminal_title_stripped"`
	Cwd         string `json:"cwd"`
	Focused     bool   `json:"focused"`
}

func herdrBin() string {
	if bin := os.Getenv("HERDR_BIN_PATH"); bin != "" {
		return bin
	}
	return "herdr"
}

func herdr(args ...string) error {
	out, err := exec.Command(herdrBin(), args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("herdr %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}

// Decodes the result of a herdr JSON response into out.
func herdrJSON(out any, args ...string) error {
	b, err := exec.Command(herdrBin(), args...).Output()
	if err != nil {
		return fmt.Errorf("herdr %s: %w", strings.Join(args, " "), err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	return json.Unmarshal(resp.Result, out)
}

// Turns the current panes into items. Anything that fails to load is left out.
// herdr returns panes in workspace order, which is kept as is.
func paneItems() []item {
	var (
		ws struct {
			Workspaces []workspace `json:"workspaces"`
		}
		ts struct {
			Tabs []tab `json:"tabs"`
		}
		ps struct {
			Panes []pane `json:"panes"`
		}
		wg sync.WaitGroup
	)
	wg.Go(func() { herdrJSON(&ws, "workspace", "list") })
	wg.Go(func() { herdrJSON(&ts, "tab", "list") })
	wg.Go(func() { herdrJSON(&ps, "pane", "list") })
	wg.Wait()

	wsLabel := map[string]string{}
	for _, w := range ws.Workspaces {
		wsLabel[w.ID] = w.Label
	}
	tabLabel := map[string]string{}
	for _, t := range ts.Tabs {
		tabLabel[t.ID] = t.Label
	}
	branches := gitBranches(ps.Panes)

	// Align the workspace and tab columns.
	wsWidth, tabWidth := 0, 0
	for _, p := range ps.Panes {
		wsWidth = max(wsWidth, ansi.StringWidth(wsLabel[p.WorkspaceID]))
		tabWidth = max(tabWidth, ansi.StringWidth(tabLabel[p.TabID]))
	}

	// When opened as an overlay the launcher is itself a pane, so skip it and treat the pane it was opened from as current.
	self, here := os.Getenv("HERDR_PANE_ID"), launchedFrom()
	var items []item
	for _, p := range ps.Panes {
		if p.ID == self {
			continue
		}
		if here != "" {
			p.Focused = p.ID == here
		}
		kind, status := "terminal", ""
		if p.Agent != "" {
			kind, status = p.Agent, p.AgentStatus
		}
		ws, tb, name := wsLabel[p.WorkspaceID], tabLabel[p.TabID], paneName(p)
		desc := fmt.Sprintf("%s › %s › %s%s\ncwd: %s", ws, tb, name, current(p.Focused), shortenHome(p.Cwd))
		if b := branches[p.Cwd]; b != "" {
			desc += "\nbranch: " + b
		}
		items = append(items, item{
			id:     p.ID,
			kind:   kind,
			status: status,
			title:  name,
			columns: []column{
				kindColumn(kind),
				{pad(ws, wsWidth) + "  ", fg},
				{pad(tb, tabWidth) + "  ", subtle},
				{name, fg},
			},
			extra:   branches[p.Cwd] + " " + p.Cwd,
			group:   p.WorkspaceID,
			focused: p.Focused,
			desc:    desc,
			run:     func() error { return afterClose("focus-pane", p.ID) },
			preview: func() (string, error) { return readPane(p.ID) },
		})
	}
	return items
}

// Looks up the git branch of each pane's cwd in parallel. Empty outside a git repository.
func gitBranches(panes []pane) map[string]string {
	cwds := map[string]bool{}
	for _, p := range panes {
		cwds[p.Cwd] = true
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		branches = map[string]string{}
	)
	for cwd := range cwds {
		wg.Go(func() {
			out, err := exec.Command("git", "-C", cwd, "branch", "--show-current").Output()
			if err != nil {
				return
			}
			mu.Lock()
			branches[cwd] = strings.TrimSpace(string(out))
			mu.Unlock()
		})
	}
	wg.Wait()
	return branches
}

// The pane that was focused when the launcher opened.
func launchedFrom() string {
	var ctx struct {
		FocusedPaneID string `json:"focused_pane_id"`
	}
	json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx)
	return ctx.FocusedPaneID
}

func current(focused bool) string {
	if focused {
		return " (current)"
	}
	return ""
}

// The agent's task title if there is an agent, otherwise the label or directory name.
func paneName(p pane) string {
	if p.Agent != "" && p.Title != "" {
		return p.Title
	}
	if p.Label != "" {
		return p.Label
	}
	return p.Cwd[strings.LastIndex(p.Cwd, "/")+1:]
}

// pane.focus jumps straight to a pane by id, even in another workspace or tab.
// The CLI does not expose it, so send it to the socket directly.
func focusPane(paneID string) error {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		return errors.New("HERDR_SOCKET_PATH is not set")
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	req, err := json.Marshal(map[string]any{"id": "launcher", "method": "pane.focus", "params": map[string]string{"pane_id": paneID}})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("pane.focus %s: %s", paneID, resp.Error.Message)
	}
	return nil
}

// Reads what the pane currently shows, with colors.
func readPane(paneID string) (string, error) {
	b, err := exec.Command(herdrBin(), "pane", "read", paneID, "--source", "visible", "--format", "ansi").Output()
	if err != nil {
		return "", fmt.Errorf("herdr pane read %s: %w", paneID, err)
	}
	return strings.ReplaceAll(string(b), "\r", ""), nil
}

func shortenHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
