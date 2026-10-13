package main

// For sort = "frecency", records chosen items in HERDR_PLUGIN_STATE_DIR and ranks them the way
// zoxide ranks directories (src/db in ajeetdsouza/zoxide):
//   - choosing an item adds 1 to its rank and updates its last access time
//   - when the ranks add up to more than maxRankTotal, every rank is scaled down so the total
//     becomes 90% of it, and items whose rank drops below 1 are forgotten
//   - the score is the rank weighted by how recently the item was chosen
//   - items that no longer exist are forgotten once they have not been chosen for 90 days

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type visit struct {
	Rank float64 `json:"rank"`
	Last int64   `json:"last"` // unix seconds
}

// Visits by item id: "command:<name>" for commands and the pane id for panes.
type history map[string]visit

const (
	maxRankTotal = 10000 // zoxide's default _ZO_MAXAGE
	staleAfter   = 90 * 24 * time.Hour
)

func historyPath() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "history.json")
}

// Treats an unreadable file as empty history.
func loadHistory(path string) history {
	h := history{}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &h)
	}
	return h
}

// Records that id was chosen, then ages and prunes the history and saves it.
// listed holds the ids of everything that was listed this time, and sources the kinds that were
// loaded, so that an item is only treated as gone when its kind was actually checked.
func (h history) record(path, id string, now time.Time, listed map[string]bool, sources []string) error {
	v := h[id]
	v.Rank++
	v.Last = now.Unix()
	h[id] = v
	h.age()
	h.forgetStale(now, listed, sources)
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (h history) age() {
	total := 0.0
	for _, v := range h {
		total += v.Rank
	}
	if total <= maxRankTotal {
		return
	}
	factor := 0.9 * maxRankTotal / total
	for id, v := range h {
		v.Rank *= factor
		if v.Rank < 1 {
			delete(h, id)
			continue
		}
		h[id] = v
	}
}

func (h history) forgetStale(now time.Time, listed map[string]bool, sources []string) {
	checked := map[string]bool{}
	for _, s := range sources {
		checked[s] = true
	}
	for id, v := range h {
		source := "panes"
		if strings.HasPrefix(id, "command:") {
			source = "commands"
		}
		if checked[source] && !listed[id] && now.Sub(time.Unix(v.Last, 0)) > staleAfter {
			delete(h, id)
		}
	}
}

// The rank weighted by how recently the item was chosen, with zoxide's weights.
func (h history) score(id string, now time.Time) float64 {
	v, ok := h[id]
	if !ok {
		return 0
	}
	age := now.Sub(time.Unix(v.Last, 0))
	switch {
	case age < time.Hour:
		return v.Rank * 4
	case age < 24*time.Hour:
		return v.Rank * 2
	case age < 7*24*time.Hour:
		return v.Rank * 0.5
	default:
		return v.Rank * 0.25
	}
}
