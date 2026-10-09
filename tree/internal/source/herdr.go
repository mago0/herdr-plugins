// Package source reads the live Herdr state and the dispatch plugin's state files.
package source

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
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
		} `json:"agents"`
	}
	var panes struct {
		Panes []struct {
			Pane    string             `json:"pane_id"`
			Tab     string             `json:"tab_id"`
			Focused bool               `json:"focused"`
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
	for _, w := range ws.Workspaces {
		m := model.Workspace{ID: w.ID, Label: w.Label, Number: w.Number, Status: w.Status}
		if w.Worktree != nil {
			m.Repo = w.Worktree.Repo
			m.Where, m.Worktree = where(w.Worktree.Root, w.Worktree.Checkout, w.Worktree.Linked, home), w.Worktree.Linked
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
		s.Agents = append(s.Agents, model.Agent{
			PaneID: a.Pane, TabID: a.Tab, WorkspaceID: a.Workspace,
			Name: a.Name, Kind: a.Kind, Title: a.Title, Status: a.Status, Hide: hide[a.Pane],
			Where: where(main, top, linked, home), Worktree: linked, Tag: tag[a.Pane],
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
