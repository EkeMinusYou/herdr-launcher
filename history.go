package main

// For --sort frecency, records how often and when each item was chosen in HERDR_PLUGIN_STATE_DIR.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type visit struct {
	Count int   `json:"count"`
	Last  int64 `json:"last"` // unix seconds
}

// Visits by item id. Pane ids go unused once a pane closes, so old entries are dropped on save.
type history map[string]visit

const historyRetention = 90 * 24 * time.Hour

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

func (h history) record(path, id string, now time.Time) error {
	v := h[id]
	v.Count++
	v.Last = now.Unix()
	h[id] = v
	for id, v := range h {
		if now.Sub(time.Unix(v.Last, 0)) > historyRetention {
			delete(h, id)
		}
	}
	b, err := json.Marshal(h)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Counts visits, weighting recent ones more (the same idea as z and fasd).
func (h history) score(id string, now time.Time) float64 {
	v, ok := h[id]
	if !ok {
		return 0
	}
	age := now.Sub(time.Unix(v.Last, 0))
	weight := 0.5
	switch {
	case age < time.Hour:
		weight = 4
	case age < 24*time.Hour:
		weight = 2
	case age < 7*24*time.Hour:
		weight = 1
	}
	return float64(v.Count) * weight
}
