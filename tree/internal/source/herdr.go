// Package source reads the live Herdr state and the dispatch plugin's state files.
package source

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Caller makes one request to the Herdr socket API.
type Caller interface {
	Call(method string, params, result any) error
}

// Socket is the Herdr API socket named by HERDR_SOCKET_PATH.
type Socket struct{ Path string }

// APIError is an error reply from Herdr, such as pane_not_found.
type APIError struct{ Code, Message string }

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

func (s Socket) Call(method string, params, result any) error {
	conn, err := net.DialTimeout("unix", s.Path, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if params == nil {
		params = struct{}{}
	}
	req := map[string]any{"id": "tree", "method": method, "params": params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if len(line) == 0 {
		if err == nil {
			err = errors.New("empty reply")
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *APIError       `json:"error"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if reply.Error != nil {
		return reply.Error
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(reply.Result, result)
}

// Snapshot reads the workspaces, tabs and agent panes, and marks the panes that set tree=hide.
func Snapshot(c Caller) (model.Snapshot, error) {
	var ws struct {
		Workspaces []struct {
			ID       string `json:"workspace_id"`
			Label    string `json:"label"`
			Number   int    `json:"number"`
			Status   string `json:"agent_status"`
			Worktree *struct {
				Repo     string `json:"repo_name"`
				Root     string `json:"repo_root"`
				Checkout string `json:"checkout_path"`
				Linked   bool   `json:"is_linked_worktree"`
			} `json:"worktree"`
		} `json:"workspaces"`
	}
	var tabs struct {
		Tabs []struct {
			ID        string `json:"tab_id"`
			Workspace string `json:"workspace_id"`
			Label     string `json:"label"`
			Status    string `json:"agent_status"`
		} `json:"tabs"`
	}
	var agents struct {
		Agents []struct {
			Pane      string `json:"pane_id"`
			Tab       string `json:"tab_id"`
			Workspace string `json:"workspace_id"`
			Kind      string `json:"agent"`
			Name      string `json:"name"`
			Status    string `json:"agent_status"`
			Title     string `json:"terminal_title_stripped"`
			Cwd       string `json:"cwd"`
			// Set by a Herdr server that supervises agents; an older server leaves them out.
			Supervisor string   `json:"supervisor_pane_id"`
			Pending    int      `json:"pending_deliveries"`
			Stalled    *int     `json:"stalled_secs"`
			Blocker    string   `json:"blocker"`
			Lines      []string `json:"blocker_lines"`
			Dialog     string   `json:"dialog"`
		} `json:"agents"`
	}
	var panes struct {
		Panes []struct {
			Pane    string             `json:"pane_id"`
			Tab     string             `json:"tab_id"`
			Focused bool               `json:"focused"`
			Cwd     string             `json:"cwd"`
			Tokens  map[string]*string `json:"tokens"`
		} `json:"panes"`
	}
	for _, call := range []struct {
		method string
		out    any
	}{{"workspace.list", &ws}, {"tab.list", &tabs}, {"agent.list", &agents}, {"pane.list", &panes}} {
		if err := c.Call(call.method, nil, call.out); err != nil {
			return model.Snapshot{}, err
		}
	}

	hide := map[string]bool{}
	tag := map[string]string{}
	home, _ := os.UserHomeDir()
	var s model.Snapshot
	for _, p := range panes.Panes {
		if p.Focused {
			s.FocusedPane, s.FocusedTab = p.Pane, p.Tab
		}
		if v := p.Tokens["tree"]; v != nil && *v == "hide" {
			hide[p.Pane] = true
		}
		if v := p.Tokens["label"]; v != nil {
			tag[p.Pane] = strings.TrimSpace(*v)
		}
	}
	// Herdr names a workspace's repository only where a worktree action recorded it. For the
	// others, the checkout of the first pane in the first tab stands in.
	first := map[string]string{}
	for _, t := range tabs.Tabs {
		if _, ok := first[t.Workspace]; !ok {
			first[t.Workspace] = t.ID
		}
	}
	cwd := map[string]string{}
	for _, p := range panes.Panes {
		if _, ok := cwd[p.Tab]; !ok {
			cwd[p.Tab] = p.Cwd
		}
	}
	for _, w := range ws.Workspaces {
		m := model.Workspace{ID: w.ID, Label: w.Label, Number: w.Number, Status: w.Status}
		if w.Worktree != nil {
			m.Repo = w.Worktree.Repo
			m.Where, m.Worktree = where(w.Worktree.Root, w.Worktree.Checkout, w.Worktree.Linked, home), w.Worktree.Linked
			if m.Git = gitState(w.Worktree.Root, w.Worktree.Linked); m.Git.Branch != "" {
				m.Git.Repo = m.Repo
			}
		} else if main, top, linked := checkout(cwd[first[w.ID]]); main != "" {
			m.Repo = filepath.Base(main)
			m.Where, m.Worktree = where(main, top, linked, home), linked
			m.Git = gitState(main, linked)
		}
		s.Workspaces = append(s.Workspaces, m)
	}
	// Herdr lists tabs in tab bar order. Its own number stays with a tab that is moved.
	place := map[string]int{}
	for _, t := range tabs.Tabs {
		place[t.Workspace]++
		s.Tabs = append(s.Tabs, model.Tab{ID: t.ID, WorkspaceID: t.Workspace, Number: place[t.Workspace], Label: t.Label, Status: t.Status})
	}
	for _, a := range agents.Agents {
		main, top, linked := checkout(a.Cwd)
		repo := ""
		if main != "" {
			repo = filepath.Base(main)
		}
		s.Agents = append(s.Agents, model.Agent{
			PaneID: a.Pane, TabID: a.Tab, WorkspaceID: a.Workspace,
			Name: a.Name, Kind: a.Kind, Title: a.Title, Status: a.Status, Hide: hide[a.Pane],
			Where: where(main, top, linked, home), Worktree: linked, Git: gitState(main, linked), Tag: tag[a.Pane],
			Supervisor: a.Supervisor, Repo: repo, Pending: a.Pending, Stalled: a.Stalled != nil, Blocker: a.Blocker,
			BlockerLines: a.Lines, Dialog: a.Dialog,
		})
	}
	return s, nil
}

// Focus moves Herdr to the pane, tab or workspace a row stands for.
func Focus(c Caller, n *model.Node) error {
	switch n.Focus {
	case model.KindPane:
		return c.Call("pane.focus", map[string]string{"pane_id": n.ID}, nil)
	case model.KindTab:
		return c.Call("tab.focus", map[string]string{"tab_id": n.ID}, nil)
	case model.KindWorkspace:
		return c.Call("workspace.focus", map[string]string{"workspace_id": n.ID}, nil)
	}
	return errors.New("nothing to focus")
}

// Focused returns the pane Herdr has in focus and its tab. Both are empty when the call fails.
func Focused(c Caller) (pane, tab string) {
	var panes struct {
		Panes []struct {
			Pane    string `json:"pane_id"`
			Tab     string `json:"tab_id"`
			Focused bool   `json:"focused"`
		} `json:"panes"`
	}
	if err := c.Call("pane.list", nil, &panes); err != nil {
		return "", ""
	}
	for _, p := range panes.Panes {
		if p.Focused {
			return p.Pane, p.Tab
		}
	}
	return "", ""
}
