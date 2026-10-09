package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// StateDir is where the dispatch plugin keeps its runs and pane index.
func StateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "agent-dispatch")
}

// Dispatch reads every run and the pane index. A run or index that cannot be read is left out.
func Dispatch(stateDir string) model.Dispatch {
	d := model.Dispatch{Tracked: map[string]bool{}}

	var index map[string]struct {
		Role string `json:"role"`
	}
	if b, err := os.ReadFile(filepath.Join(stateDir, "panes.json")); err == nil && json.Unmarshal(b, &index) == nil {
		for pane, v := range index {
			if v.Role == "tracked" {
				d.Tracked[pane] = true
			}
		}
	}

	dirs, _ := os.ReadDir(filepath.Join(stateDir, "runs"))
	names := make([]string, 0, len(dirs))
	for _, e := range dirs {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		run := filepath.Join(stateDir, "runs", name)
		sup, err := os.ReadFile(filepath.Join(run, "supervisor"))
		if err != nil || strings.TrimSpace(string(sup)) == "" {
			continue
		}
		ledger, err := os.ReadFile(filepath.Join(run, "ledger.json"))
		if err != nil {
			continue
		}
		var entries []struct {
			Pane      string `json:"pane_id"`
			Tab       string `json:"tab_id"`
			Workspace string `json:"workspace_id"`
			Worktree  string `json:"worktree"`
			Created   string `json:"created"`
		}
		if json.Unmarshal(ledger, &entries) != nil {
			continue
		}
		r := model.Run{Name: name, Supervisor: strings.TrimSpace(string(sup))}
		for _, e := range entries {
			r.Entries = append(r.Entries, model.Entry{
				PaneID: e.Pane, TabID: e.Tab, WorkspaceID: e.Workspace, Worktree: e.Worktree, Created: e.Created,
			})
		}
		d.Runs = append(d.Runs, r)
	}
	return d
}
