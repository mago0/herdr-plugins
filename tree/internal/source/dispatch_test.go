package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDispatch(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/panes.json", `{"w1:p1":{"role":"supervisor"},"w1:p3":{"role":"tracked"},"w2:p1":{"role":"worker"}}`)
	write(t, dir+"/runs/abc-1-a/supervisor", "w1:p1\n")
	write(t, dir+"/runs/abc-1-a/ledger.json", `[
		{"pane_id":"w2:p1","tab_id":null,"workspace_id":"w2","worktree":"/src/api/_worktrees/x","created":"2026-01-01T00:00:00Z","status":"settled"},
		{"pane_id":"w1:p6","tab_id":"w1:t3","workspace_id":"w1","worktree":"/src/api/_worktrees/y","created":"2026-01-02T00:00:00Z"}]`)
	// Each of these runs is broken in one way and must not stop the others.
	write(t, dir+"/runs/b-no-supervisor/ledger.json", `[]`)
	write(t, dir+"/runs/c-bad-json/supervisor", "w1:p1")
	write(t, dir+"/runs/c-bad-json/ledger.json", `[{"pane_id":`)
	write(t, dir+"/runs/d-wrong-shape/supervisor", "w1:p1")
	write(t, dir+"/runs/d-wrong-shape/ledger.json", `{"pane_id":"w2:p1"}`)
	write(t, dir+"/runs/e-wrong-type/supervisor", "w1:p1")
	write(t, dir+"/runs/e-wrong-type/ledger.json", `[{"pane_id":7}]`)
	write(t, dir+"/runs/z-empty/supervisor", "w4:p1")
	write(t, dir+"/runs/z-empty/ledger.json", `[]`)

	got := Dispatch(dir)
	want := model.Dispatch{
		Tracked: map[string]bool{"w1:p3": true},
		Runs: []model.Run{
			{Name: "abc-1-a", Supervisor: "w1:p1", Entries: []model.Entry{
				{PaneID: "w2:p1", WorkspaceID: "w2", Worktree: "/src/api/_worktrees/x", Created: "2026-01-01T00:00:00Z"},
				{PaneID: "w1:p6", TabID: "w1:t3", WorkspaceID: "w1", Worktree: "/src/api/_worktrees/y", Created: "2026-01-02T00:00:00Z"},
			}},
			{Name: "z-empty", Supervisor: "w4:p1"},
		},
	}
	gj, _ := json.Marshal(got)
	wj, _ := json.Marshal(want)
	if string(gj) != string(wj) {
		t.Fatalf("dispatch:\n%s\nwant:\n%s", gj, wj)
	}
}

func TestDispatchMissingState(t *testing.T) {
	got := Dispatch(filepath.Join(t.TempDir(), "absent"))
	if len(got.Runs) != 0 || len(got.Tracked) != 0 {
		t.Fatalf("no dispatch state must read as empty, got %+v", got)
	}
	dir := t.TempDir()
	write(t, dir+"/panes.json", `not json`)
	if got := Dispatch(dir); len(got.Tracked) != 0 {
		t.Fatalf("a pane index that does not parse means nothing is tracked, got %+v", got)
	}
}

func TestStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if got := StateDir(); got != "/x/state/agent-dispatch" {
		t.Errorf("StateDir() = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := StateDir(); got != "/home/u/.local/state/agent-dispatch" {
		t.Errorf("StateDir() = %q", got)
	}
}
