package main

// Talks to herdr over its socket API: lists workspaces and panes, reads screens, and changes focus.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

type workspace struct {
	ID          string `json:"workspace_id"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	TabCount    int    `json:"tab_count"`
	PaneCount   int    `json:"pane_count"`
	ActiveTabID string `json:"active_tab_id"`
	AgentStatus string `json:"agent_status"`
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

type sessionSnapshot struct {
	FocusedWorkspaceID string       `json:"focused_workspace_id"`
	Workspaces         []workspace  `json:"workspaces"`
	Tabs               []tab        `json:"tabs"`
	Panes              []pane       `json:"panes"`
	Layouts            []paneLayout `json:"layouts"`
}

type paneLayout struct {
	TabID         string `json:"tab_id"`
	FocusedPaneID string `json:"focused_pane_id"`
}

// Sends one request to the herdr socket and decodes its result into out (skipped when out is nil).
// Talking to the socket directly avoids starting a herdr CLI process per call.
func socketCall(method string, params, out any) error {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		return errors.New("HERDR_SOCKET_PATH is not set")
	}
	// Build the request before connecting: if it arrives a moment after the connection opens,
	// herdr only picks it up about 100ms later.
	req, err := json.Marshal(map[string]any{"id": "launcher", "method": method, "params": params})
	if err != nil {
		return err
	}
	conn, err := dialHerdr(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return err
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", method, resp.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

// Turns the current panes into items. Anything that fails to load is left out and logged.
// herdr returns panes in workspace order, which is kept as is.
func paneItems() []item {
	var (
		wsList struct {
			Workspaces []workspace `json:"workspaces"`
		}
		tabList struct {
			Tabs []tab `json:"tabs"`
		}
		paneList struct {
			Panes []pane `json:"panes"`
		}
		wg sync.WaitGroup
	)
	wg.Go(func() { logError(socketCall("workspace.list", map[string]any{}, &wsList)) })
	wg.Go(func() { logError(socketCall("tab.list", map[string]any{}, &tabList)) })
	wg.Go(func() { logError(socketCall("pane.list", map[string]any{}, &paneList)) })
	wg.Wait()

	wsLabel := map[string]string{}
	for _, w := range wsList.Workspaces {
		wsLabel[w.ID] = w.Label
	}
	tabLabel := map[string]string{}
	for _, t := range tabList.Tabs {
		tabLabel[t.ID] = t.Label
	}
	branches := gitBranches(paneList.Panes)

	// Align the workspace and tab columns.
	wsWidth, tabWidth := 0, 0
	for _, p := range paneList.Panes {
		wsWidth = max(wsWidth, ansi.StringWidth(wsLabel[p.WorkspaceID]))
		tabWidth = max(tabWidth, ansi.StringWidth(tabLabel[p.TabID]))
	}

	// When opened as an overlay the launcher is itself a pane, so skip it and treat the pane it was opened from as current.
	self, here := os.Getenv("HERDR_PANE_ID"), launchContext.FocusedPaneID
	var items []item
	for _, p := range paneList.Panes {
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

// Lists each workspace once, using the focused pane of its active tab for the preview.
func workspaceItems() ([]item, error) {
	var resp struct {
		Snapshot sessionSnapshot `json:"snapshot"`
	}
	if err := socketCall("session.snapshot", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return itemsFromWorkspaces(resp.Snapshot, launchContext, os.Getenv("HERDR_PANE_ID")), nil
}

func itemsFromWorkspaces(snapshot sessionSnapshot, ctx pluginContext, self string) []item {
	panes := make(map[string]pane, len(snapshot.Panes))
	for _, p := range snapshot.Panes {
		panes[p.ID] = p
	}
	tabs := make(map[string]tab, len(snapshot.Tabs))
	for _, t := range snapshot.Tabs {
		tabs[t.ID] = t
	}
	focusedPane := make(map[string]string, len(snapshot.Layouts))
	for _, layout := range snapshot.Layouts {
		focusedPane[layout.TabID] = layout.FocusedPaneID
	}
	// An overlay focuses its own pane. The invocation context identifies the underlying one.
	currentWorkspace := ctx.WorkspaceID
	if currentWorkspace == "" {
		currentWorkspace = panes[ctx.FocusedPaneID].WorkspaceID
	}
	if currentWorkspace == "" {
		currentWorkspace = snapshot.FocusedWorkspaceID
	}
	branches := map[string]string{}
	var items []item
	for _, w := range snapshot.Workspaces {
		focused := w.ID == currentWorkspace
		if currentWorkspace == "" {
			focused = w.Focused
		}
		paneID := focusedPane[w.ActiveTabID]
		if p, ok := panes[ctx.FocusedPaneID]; focused && ok && p.WorkspaceID == w.ID && p.ID != self {
			paneID = p.ID
		}
		p, hasPane := panes[paneID]
		hasPane = hasPane && paneID != self && p.WorkspaceID == w.ID
		paneCount := w.PaneCount
		if p, ok := panes[self]; ok && p.WorkspaceID == w.ID {
			paneCount = max(paneCount-1, 0)
		}
		desc := fmt.Sprintf("tabs: %d, panes: %d%s", w.TabCount, paneCount, current(focused))
		extra := ""
		branchColumn, agentColumn, taskColumn := "", "", ""
		var preview func() (string, error)
		if hasPane {
			if p.Cwd != "" {
				if _, ok := branches[p.Cwd]; !ok {
					branches[p.Cwd] = gitBranch(p.Cwd)
				}
			}
			branch := branches[p.Cwd]
			tabName, name := tabs[p.TabID].Label, paneName(p)
			desc += fmt.Sprintf("\nactive: %s › %s\ncwd: %s", tabName, name, shortenHome(p.Cwd))
			if branch != "" {
				desc += "\nbranch: " + branch
			}
			branchColumn = branch
			agentColumn = p.Agent
			if name != "" && name != w.Label && name != branch && name != p.Agent {
				taskColumn = name
			}
			extra = tabName + " " + p.Cwd
			preview = func() (string, error) { return readPane(paneID) }
		}
		items = append(items, item{
			id:      "workspace:" + w.ID,
			kind:    "workspace",
			status:  w.AgentStatus,
			title:   w.Label,
			columns: []column{{w.Label, fg}, {branchColumn, subtle}, {agentColumn, kindColor(agentColumn)}, {taskColumn, fg}},
			extra:   extra,
			group:   w.ID,
			focused: focused,
			desc:    desc,
			run:     func() error { return afterClose("focus-workspace", w.ID) },
			preview: preview,
		})
	}
	return alignWorkspaceColumns(items)
}

// Align the workspace, branch and agent columns independently, including missing fields.
func alignWorkspaceColumns(items []item) []item {
	widths := [3]int{}
	for _, it := range items {
		for i := range widths {
			widths[i] = max(widths[i], ansi.StringWidth(it.columns[i].text))
		}
	}
	for i := range items {
		cols := items[i].columns
		last := len(cols) - 1
		for last > 0 && cols[last].text == "" {
			last--
		}
		for j := 0; j < last; j++ {
			if widths[j] > 0 {
				cols[j].text = pad(cols[j].text, widths[j]) + "  "
			}
		}
	}
	return items
}

// Looks up the git branch of each pane's cwd. Empty outside a git repository.
func gitBranches(panes []pane) map[string]string {
	branches := map[string]string{}
	for _, p := range panes {
		if _, ok := branches[p.Cwd]; !ok {
			branches[p.Cwd] = gitBranch(p.Cwd)
		}
	}
	return branches
}

// Reads the current branch from HEAD instead of running git, which is much faster for many panes.
// Like git branch --show-current, it is empty for a detached HEAD. Worktrees and submodules have a
// .git file pointing at their git directory.
func gitBranch(dir string) string {
	for d := dir; ; d = filepath.Dir(d) {
		gitPath := filepath.Join(d, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			gitDir := gitPath
			if !info.IsDir() {
				b, err := os.ReadFile(gitPath)
				if err != nil {
					return ""
				}
				target, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir: ")
				if !ok {
					return ""
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(d, target)
				}
				gitDir = target
			}
			head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			if err != nil {
				return ""
			}
			branch, ok := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: refs/heads/")
			if !ok {
				return "" // detached HEAD
			}
			return branch
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// What herdr tells a plugin pane about the pane that was focused when it opened.
type pluginContext struct {
	WorkspaceID    string `json:"workspace_id"`
	FocusedPaneID  string `json:"focused_pane_id"`
	FocusedPaneCwd string `json:"focused_pane_cwd"`
}

var launchContext = func() pluginContext {
	var ctx pluginContext
	json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx)
	return ctx
}()

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
	if p.Cwd == "" {
		return ""
	}
	return filepath.Base(p.Cwd)
}

// pane.focus jumps straight to a pane by id, even in another workspace or tab.
// The CLI does not expose it, so it goes through the socket.
func focusPane(paneID string) error {
	return socketCall("pane.focus", map[string]string{"pane_id": paneID}, nil)
}

func focusWorkspace(workspaceID string) error {
	return socketCall("workspace.focus", map[string]string{"workspace_id": workspaceID}, nil)
}

// Reads what the pane currently shows, with colors.
func readPane(paneID string) (string, error) {
	var resp struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	params := map[string]string{"pane_id": paneID, "source": "visible", "format": "ansi"}
	if err := socketCall("pane.read", params, &resp); err != nil {
		return "", err
	}
	return strings.ReplaceAll(resp.Read.Text, "\r", ""), nil
}

func shortenHome(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
