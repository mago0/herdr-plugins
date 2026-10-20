package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// A ledger file as last read, kept until its time or size changes.
type ledgerFile struct {
	mod     time.Time
	size    int64
	entries []ledgerEntry
}

// ledgerEntry is the part of a dispatch attempt that says whether its supervisor took its result.
type ledgerEntry struct {
	Pane    string `json:"pane_id"`
	Status  string `json:"status"`
	Started string `json:"started"`
}

var (
	ledgerMu sync.Mutex
	ledgers  = map[string]ledgerFile{}
)

// LedgerDir is the folder that holds one folder for each dispatch run.
func LedgerDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "agent-dispatch", "runs")
}

// Settled reads the ledger of each run in dir and returns the panes whose latest dispatch is
// settled: the supervisor took the worker's worker_done. A ledger that does not parse counts as empty.
func Settled(dir string) map[string]bool {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "ledger.json"))
	latest := map[string]ledgerEntry{}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		f, ok := ledgers[p]
		if !ok || !f.mod.Equal(info.ModTime()) || f.size != info.Size() {
			f = ledgerFile{mod: info.ModTime(), size: info.Size()}
			if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &f.entries) != nil {
				f.entries = nil
			}
			ledgers[p] = f
		}
		for _, e := range f.entries {
			if e.Pane == "" {
				continue
			}
			if old, ok := latest[e.Pane]; !ok || e.Started >= old.Started {
				latest[e.Pane] = e
			}
		}
	}
	out := map[string]bool{}
	for pane, e := range latest {
		if e.Status == "settled" {
			out[pane] = true
		}
	}
	return out
}
