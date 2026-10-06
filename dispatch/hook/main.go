// dispatch-hook runs once per Herdr event. It maps the event's pane to a dispatch run through
// the pane index that dispatch.sh maintains, and wakes the run's supervisor when a worker
// blocks or exits, or when a held wake can be delivered.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

type entry struct {
	Run        string `json:"run"`
	Role       string `json:"role"`
	Agent      string `json:"agent,omitempty"`
	Supervisor string `json:"supervisor,omitempty"`
	Pending    bool   `json:"pending,omitempty"`
}

type event struct {
	Data struct {
		PaneID      string `json:"pane_id"`
		AgentStatus string `json:"agent_status"`
	} `json:"data"`
}

func indexPath() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(state, "agent-dispatch", "panes.json")
}

// Writers replace the index by rename, so a read needs no lock.
func readIndex() map[string]entry {
	idx := map[string]entry{}
	if b, err := os.ReadFile(indexPath()); err == nil {
		_ = json.Unmarshal(b, &idx)
	}
	return idx
}

// update applies fn under the same flock that dispatch.sh takes.
func update(fn func(map[string]entry)) {
	p := indexPath()
	lock, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		return
	}
	idx := readIndex()
	fn(idx)
	b, _ := json.Marshal(idx)
	tmp := p + ".hook.tmp"
	if os.WriteFile(tmp, append(b, '\n'), 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

func setPending(pane string, v bool) {
	update(func(idx map[string]entry) {
		if e, ok := idx[pane]; ok {
			e.Pending = v
			idx[pane] = e
		}
	})
}

func herdr(args ...string) ([]byte, error) {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	return exec.Command(bin, args...).Output()
}

// typable reports whether a prompt can be typed into the pane now: an agent is there, the
// user is not in the pane, and no dialog can swallow the input.
func typable(pane string) (ok, focused bool) {
	out, err := herdr("agent", "get", pane)
	if err != nil {
		return false, false
	}
	var r struct {
		Result struct {
			Agent struct {
				Focused bool   `json:"focused"`
				Status  string `json:"agent_status"`
			} `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &r) != nil {
		return false, false
	}
	a := r.Result.Agent
	return !a.Focused && a.Status != "blocked" && a.Status != "unknown" && a.Status != "", a.Focused
}

// wake types line into the supervisor pane, or holds it as pending when that is not safe.
func wake(sup, line, title string) {
	ok, focused := typable(sup)
	if ok {
		if _, err := herdr("agent", "prompt", sup, line); err == nil {
			return
		}
	} else if focused && title != "" {
		_, _ = herdr("notification", "show", title, "--sound", "request")
	}
	setPending(sup, true)
}

func dispatchSh() string {
	return filepath.Join(os.Getenv("HERDR_PLUGIN_ROOT"), "skill", "scripts", "dispatch.sh")
}

func flush(pane string, e entry) {
	if ok, _ := typable(pane); !ok {
		return
	}
	setPending(pane, false)
	line := fmt.Sprintf("DISPATCH|pending|%s - un-acked mail is waiting: %s ps --run %s", e.Run, dispatchSh(), e.Run)
	if _, err := herdr("agent", "prompt", pane, line); err != nil {
		setPending(pane, true)
	}
}

// gone handles an indexed pane that no longer exists: the entry is removed, and a worker's
// supervisor is told, because a worker that had reported worker_done is no longer indexed.
func gone(pane string, e entry) {
	update(func(idx map[string]entry) { delete(idx, pane) })
	if e.Supervisor == "" {
		return
	}
	switch e.Role {
	case "worker":
		wake(e.Supervisor,
			fmt.Sprintf("DISPATCH|exited|%s|%s - worker pane exited with no worker_done: %s ps --run %s", e.Agent, e.Run, dispatchSh(), e.Run),
			"dispatch: "+e.Agent+" exited")
	case "tracked":
		wake(e.Supervisor,
			fmt.Sprintf("DISPATCH|exited|%s|%s - tracked pane is gone", e.Agent, e.Run),
			"dispatch: "+e.Agent+" exited")
	}
}

// sweep finds indexed panes that herdr no longer lists. A closed tab or workspace takes its
// panes with it and emits no pane event.
func sweep(idx map[string]entry) {
	out, err := herdr("pane", "list")
	if err != nil {
		return
	}
	var r struct {
		Result struct {
			Panes []struct {
				PaneID string `json:"pane_id"`
			} `json:"panes"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &r) != nil || len(r.Result.Panes) == 0 {
		return
	}
	live := map[string]bool{}
	for _, p := range r.Result.Panes {
		live[p.PaneID] = true
	}
	for pane, e := range idx {
		if !live[pane] {
			gone(pane, e)
		}
	}
}

// ensureName gives a worker's agent its dispatch name back. Herdr clears agent names when a
// server restart resumes the agent, and the scripts address workers by name.
func ensureName(pane, name string) {
	out, err := herdr("agent", "get", pane)
	if err != nil {
		return
	}
	var r struct {
		Result struct {
			Agent struct {
				Name string `json:"name"`
			} `json:"agent"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &r) == nil && r.Result.Agent.Name != name {
		_, _ = herdr("agent", "rename", pane, name)
	}
}

func main() {
	idx := readIndex()
	if len(idx) == 0 {
		return
	}
	kind := os.Getenv("HERDR_PLUGIN_EVENT")
	switch kind {
	case "pane.focused":
		for pane, e := range idx {
			if e.Role == "supervisor" && e.Pending {
				flush(pane, e)
			}
		}
		return
	case "tab.closed", "workspace.closed":
		sweep(idx)
		return
	}

	var ev event
	if json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_EVENT_JSON")), &ev) != nil {
		return
	}
	pane := ev.Data.PaneID
	e, ok := idx[pane]
	if !ok {
		return
	}
	if kind == "pane.exited" || kind == "pane.closed" {
		gone(pane, e)
		return
	}
	if e.Role == "worker" && e.Agent != "" {
		ensureName(pane, e.Agent)
	}
	switch {
	case e.Role == "supervisor" && e.Pending:
		flush(pane, e)
	case e.Role == "worker" && e.Supervisor != "" && ev.Data.AgentStatus == "blocked":
		wake(e.Supervisor,
			fmt.Sprintf("DISPATCH|blocked|%s|%s - worker is waiting at a dialog: herdr agent read %s --source recent-unwrapped --lines 120", e.Agent, e.Run, e.Agent),
			"dispatch: "+e.Agent+" is blocked")
	}
}
