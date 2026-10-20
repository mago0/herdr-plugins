package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A ledger or inbox file as last read, kept until its time or size changes.
type ledgerFile struct {
	mod     time.Time
	size    int64
	entries []ledgerEntry
}

// ledgerEntry is the part of a dispatch attempt that says whether its supervisor took its result.
// For a line of a worker's inbox, Status holds the message type and Settled its time.
type ledgerEntry struct {
	Pane    string `json:"pane_id"`
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Started string `json:"started"`
	Settled string `json:"settled"`
}

var (
	ledgerMu sync.Mutex
	ledgers  = map[string]ledgerFile{}
)

// readCached is the entries of a ledger file, or of a worker's inbox when lines is true, read
// again only when the file's time or size changes. A file that does not parse has none.
func readCached(p string, lines bool) []ledgerEntry {
	info, err := os.Stat(p)
	if err != nil {
		return nil
	}
	f, ok := ledgers[p]
	if ok && f.mod.Equal(info.ModTime()) && f.size == info.Size() {
		return f.entries
	}
	f = ledgerFile{mod: info.ModTime(), size: info.Size()}
	b, err := os.ReadFile(p)
	switch {
	case err != nil:
	case !lines:
		if json.Unmarshal(b, &f.entries) != nil {
			f.entries = nil
		}
	default:
		for _, line := range strings.Split(string(b), "\n") {
			var m struct{ Type, Ts string }
			if json.Unmarshal([]byte(line), &m) == nil {
				f.entries = append(f.entries, ledgerEntry{Status: m.Type, Settled: m.Ts})
			}
		}
	}
	ledgers[p] = f
	return f.entries
}

// followedUp is true when the supervisor sent agent a follow-up at or after since: new work whose
// result it has not taken yet.
func followedUp(run, agent, since string) bool {
	for _, m := range readCached(filepath.Join(run, "workers", agent, "inbox.jsonl"), true) {
		if m.Status == "followup" && m.Settled >= since {
			return true
		}
	}
	return false
}

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
// settled, which means the supervisor took the worker's worker_done, with no follow-up sent since.
// A ledger that does not parse counts as empty.
func Settled(dir string) map[string]bool {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "ledger.json"))
	type found struct {
		run string
		e   ledgerEntry
	}
	latest := map[string]found{}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	for _, p := range paths {
		for _, e := range readCached(p, false) {
			if e.Pane == "" {
				continue
			}
			if old, ok := latest[e.Pane]; !ok || e.Started >= old.e.Started {
				latest[e.Pane] = found{filepath.Dir(p), e}
			}
		}
	}
	out := map[string]bool{}
	for pane, f := range latest {
		if f.e.Status == "settled" && !followedUp(f.run, f.e.Agent, f.e.Settled) {
			out[pane] = true
		}
	}
	return out
}
