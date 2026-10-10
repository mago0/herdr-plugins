package source

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// serve answers each connection with the reply for the method it asks for.
func serve(t *testing.T, replies map[string]string) Socket {
	t.Helper()
	dir, err := os.MkdirTemp("", "ht")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadBytes('\n')
				var req struct {
					Method string `json:"method"`
				}
				json.Unmarshal(line, &req)
				// Herdr replies with one line; the fixtures are written over several.
				var reply bytes.Buffer
				json.Compact(&reply, []byte(replies[req.Method]))
				reply.WriteByte('\n')
				conn.Write(reply.Bytes())
			}()
		}
	}()
	return Socket{Path: path}
}

func TestSnapshot(t *testing.T) {
	s := serve(t, map[string]string{
		"workspace.list": `{"id":"tree","result":{"workspaces":[
			{"workspace_id":"w1","label":"ops","number":1,"agent_status":"working","worktree":{"repo_name":"ops"}},
			{"workspace_id":"w2","label":"notes","number":2,"agent_status":"unknown"}]}}`,
		"tab.list": `{"id":"tree","result":{"tabs":[
			{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"monitor","agent_status":"working"}]}}`,
		"agent.list": `{"id":"tree","result":{"agents":[
			{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent":"claude","name":"lead","agent_status":"working","terminal_title_stripped":"Plan"},
			{"pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1","agent":"waker","agent_status":"working"}]}}`,
		"pane.list": `{"id":"tree","result":{"panes":[
			{"pane_id":"w1:p1"},
			{"pane_id":"w1:p2","tokens":{"tree":"hide","other":null}}]}}`,
	})
	got, err := Snapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Snapshot{
		Workspaces: []model.Workspace{
			{ID: "w1", Label: "ops", Number: 1, Status: "working", Repo: "ops"},
			{ID: "w2", Label: "notes", Number: 2, Status: "unknown"},
		},
		Tabs: []model.Tab{{ID: "w1:t1", WorkspaceID: "w1", Number: 1, Label: "monitor", Status: "working"}},
		Agents: []model.Agent{
			{PaneID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1", Kind: "claude", Name: "lead", Status: "working", Title: "Plan"},
			{PaneID: "w1:p2", TabID: "w1:t1", WorkspaceID: "w1", Kind: "waker", Status: "working", Hide: true},
		},
	}
	gj, _ := json.Marshal(got)
	wj, _ := json.Marshal(want)
	if string(gj) != string(wj) {
		t.Fatalf("snapshot:\n%s\nwant:\n%s", gj, wj)
	}
}

func TestCallReadsLargeReply(t *testing.T) {
	big := strings.Repeat("x", 2<<20)
	s := serve(t, map[string]string{"ping": `{"id":"tree","result":{"pad":"` + big + `"}}`})
	var out struct {
		Pad string `json:"pad"`
	}
	if err := s.Call("ping", nil, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pad) != len(big) {
		t.Fatalf("read %d bytes of %d", len(out.Pad), len(big))
	}
}

func TestCallErrors(t *testing.T) {
	s := serve(t, map[string]string{"pane.focus": `{"id":"tree","error":{"code":"pane_not_found","message":"pane w9:p1 not found"}}`})
	err := Focus(s, &model.Node{ID: "w9:p1", Focus: model.KindPane})
	var api *APIError
	if !errors.As(err, &api) || api.Code != "pane_not_found" {
		t.Fatalf("err = %v, want APIError pane_not_found", err)
	}
	if err := (Socket{Path: "/nonexistent/s.sock"}).Call("ping", nil, nil); err == nil {
		t.Fatal("a missing socket must be an error")
	}
}

type recorder struct{ method, params string }

func (r *recorder) Call(method string, params, result any) error {
	b, _ := json.Marshal(params)
	r.method, r.params = method, string(b)
	return nil
}

func TestFocusByKind(t *testing.T) {
	cases := []struct {
		n      model.Node
		method string
		params string
	}{
		{model.Node{ID: "w1:p1", Focus: model.KindPane}, "pane.focus", `{"pane_id":"w1:p1"}`},
		{model.Node{ID: "w1:t2", Focus: model.KindTab}, "tab.focus", `{"tab_id":"w1:t2"}`},
		{model.Node{ID: "w1", Focus: model.KindWorkspace}, "workspace.focus", `{"workspace_id":"w1"}`},
	}
	for _, c := range cases {
		r := &recorder{}
		if err := Focus(r, &c.n); err != nil {
			t.Fatal(err)
		}
		if r.method != c.method || r.params != c.params {
			t.Errorf("Focus(%s) = %s %s, want %s %s", c.n.ID, r.method, r.params, c.method, c.params)
		}
	}
	if err := Focus(&recorder{}, &model.Node{Focus: model.KindGroup}); err == nil {
		t.Error("a group row has nothing to focus")
	}
}

func TestSnapshotNumbersTabsByTheirPlaceInTheTabBar(t *testing.T) {
	s := serve(t, map[string]string{
		"workspace.list": `{"id":"tree","result":{"workspaces":[]}}`,
		"tab.list": `{"id":"tree","result":{"tabs":[
			{"tab_id":"w1:t9","workspace_id":"w1","number":9,"label":"moved first"},
			{"tab_id":"w2:t1","workspace_id":"w2","number":1,"label":"other"},
			{"tab_id":"w1:t1","workspace_id":"w1","number":1,"label":"second"}]}}`,
		"agent.list": `{"id":"tree","result":{"agents":[]}}`,
		"pane.list":  `{"id":"tree","result":{"panes":[]}}`,
	})
	got, err := Snapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{1, 1, 2} {
		if got.Tabs[i].Number != want {
			t.Errorf("tab %s has number %d, want %d", got.Tabs[i].ID, got.Tabs[i].Number, want)
		}
	}
}

func TestSnapshotNamesTheRepoOfAWorkspaceHerdrGivesNoWorktree(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "flosports")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	s := serve(t, map[string]string{
		"workspace.list": `{"id":"tree","result":{"workspaces":[
			{"workspace_id":"w1","label":"migration","number":1},
			{"workspace_id":"w2","label":"notes","number":2}]}}`,
		"tab.list": `{"id":"tree","result":{"tabs":[
			{"tab_id":"w1:t2","workspace_id":"w1","label":"first"},
			{"tab_id":"w2:t1","workspace_id":"w2","label":"notes"},
			{"tab_id":"w1:t1","workspace_id":"w1","label":"second"}]}}`,
		"agent.list": `{"id":"tree","result":{"agents":[]}}`,
		"pane.list": `{"id":"tree","result":{"panes":[
			{"pane_id":"w1:p1","tab_id":"w1:t1","cwd":"` + other + `"},
			{"pane_id":"w1:p2","tab_id":"w1:t2","cwd":"` + repo + `"},
			{"pane_id":"w1:p3","tab_id":"w1:t2","cwd":"` + other + `"},
			{"pane_id":"w2:p1","tab_id":"w2:t1","cwd":"` + other + `"}]}}`,
	})
	got, err := Snapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Workspaces[0].Repo != "flosports" || got.Workspaces[1].Repo != "" {
		t.Fatalf("repos = %q, %q", got.Workspaces[0].Repo, got.Workspaces[1].Repo)
	}
}

func TestSnapshotReadsTheLabelToken(t *testing.T) {
	s := serve(t, map[string]string{
		"workspace.list": `{"id":"tree","result":{"workspaces":[]}}`,
		"tab.list":       `{"id":"tree","result":{"tabs":[]}}`,
		"agent.list": `{"id":"tree","result":{"agents":[
			{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent":"claude"},
			{"pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1","agent":"claude"}]}}`,
		"pane.list": `{"id":"tree","result":{"panes":[
			{"pane_id":"w1:p1","tokens":{"label":"SRE-923"}},
			{"pane_id":"w1:p2","tokens":{"label":null}}]}}`,
	})
	got, err := Snapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents[0].Tag != "SRE-923" || got.Agents[1].Tag != "" {
		t.Fatalf("tags = %q, %q", got.Agents[0].Tag, got.Agents[1].Tag)
	}
}

func TestSnapshotReadsWhatHerdrKnowsAboutSupervision(t *testing.T) {
	s := serve(t, map[string]string{
		"workspace.list": `{"id":"tree","result":{"workspaces":[]}}`,
		"tab.list":       `{"id":"tree","result":{"tabs":[]}}`,
		"agent.list": `{"id":"tree","result":{"agents":[
			{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent":"claude","pending_deliveries":2},
			{"pane_id":"w2:p1","tab_id":"w2:t1","workspace_id":"w2","agent":"claude","agent_status":"blocked",
			 "supervisor_pane_id":"w1:p1","stalled_secs":640,"blocker":"Allow? / 1. Yes"}]}}`,
		"pane.list": `{"id":"tree","result":{"panes":[]}}`,
	})
	got, err := Snapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	lead, worker := got.Agents[0], got.Agents[1]
	if lead.Pending != 2 || lead.Supervisor != "" || lead.Stalled || lead.Blocker != "" {
		t.Fatalf("lead = %+v", lead)
	}
	if worker.Supervisor != "w1:p1" || !worker.Stalled || worker.Blocker != "Allow? / 1. Yes" || worker.Pending != 0 {
		t.Fatalf("worker = %+v", worker)
	}
}

func TestFocused(t *testing.T) {
	s := serve(t, map[string]string{
		"pane.list": `{"id":"tree","result":{"panes":[
			{"pane_id":"w1:p1","tab_id":"w1:t1"},
			{"pane_id":"w1:p2","tab_id":"w1:t2","focused":true}]}}`,
	})
	if pane, tab := Focused(s); pane != "w1:p2" || tab != "w1:t2" {
		t.Fatalf("got %q in %q", pane, tab)
	}
	if pane, tab := Focused(Socket{Path: "/nonexistent"}); pane != "" || tab != "" {
		t.Fatalf("a failed call names no pane, got %q in %q", pane, tab)
	}
}
