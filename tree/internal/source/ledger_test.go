package source

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLedger(t *testing.T, dir, run, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, run), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, run, "ledger.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSettledReadsTheLatestDispatchOfEachPane(t *testing.T) {
	dir := t.TempDir()
	writeLedger(t, dir, "r1", `[
		{"pane_id":"w1:p1","status":"settled","started":"2026-10-16T17:58:03Z"},
		{"pane_id":"w2:p1","status":"settled","started":"2026-10-16T17:58:03Z"},
		{"pane_id":null,"status":"pending","started":null}]`)
	writeLedger(t, dir, "r2", `[{"pane_id":"w2:p1","status":"working","started":"2026-10-19T21:23:09Z"}]`)
	writeLedger(t, dir, "broken", `[{"pane_id":`)
	got := Settled(dir)
	if !got["w1:p1"] || got["w2:p1"] || len(got) != 1 {
		t.Fatalf("settled = %v, want only w1:p1: a later working dispatch on w2:p1 wins", got)
	}
}

func TestSettledReadsALedgerAgainWhenItChanges(t *testing.T) {
	dir := t.TempDir()
	path := writeLedger(t, dir, "r1", `[{"pane_id":"w1:p1","status":"working","started":"2026-10-16T17:58:03Z"}]`)
	if Settled(dir)["w1:p1"] {
		t.Fatal("a working dispatch is not settled")
	}
	writeLedger(t, dir, "r1", `[{"pane_id":"w1:p1","status":"settled","started":"2026-10-16T17:58:03Z"}]`)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(path, later, later)
	if !Settled(dir)["w1:p1"] {
		t.Fatal("a changed ledger is read again")
	}
}

func TestSettledWithNoRuns(t *testing.T) {
	if got := Settled(filepath.Join(t.TempDir(), "absent")); len(got) != 0 {
		t.Fatalf("settled = %v", got)
	}
}

func TestLedgerDirFollowsXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got := LedgerDir(); got != "/state/agent-fleet/runs" {
		t.Fatalf("LedgerDir = %q", got)
	}
}

func TestLedgerDirFollowsTheStateLayout(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	want := func(layout, dir string) {
		t.Helper()
		if got := LedgerDir(); got != filepath.Join(state, dir, "runs") {
			t.Fatalf("%s: LedgerDir = %q, want %s/runs", layout, got, dir)
		}
	}
	want("no state", "agent-fleet")
	os.Mkdir(filepath.Join(state, "agent-dispatch"), 0o755)
	want("only agent-dispatch", "agent-dispatch")
	os.Rename(filepath.Join(state, "agent-dispatch"), filepath.Join(state, "agent-fleet"))
	os.Symlink("agent-fleet", filepath.Join(state, "agent-dispatch"))
	want("agent-dispatch a link to agent-fleet", "agent-fleet")
	os.Remove(filepath.Join(state, "agent-dispatch"))
	want("only agent-fleet", "agent-fleet")
}

func TestSettledIsClearedByAFollowUpAfterTheSettle(t *testing.T) {
	dir := t.TempDir()
	writeLedger(t, dir, "r1", `[
		{"pane_id":"w1:p1","agent":"later","status":"settled","started":"2026-10-19T09:00:00Z","settled":"2026-10-19T10:00:00Z"},
		{"pane_id":"w2:p1","agent":"earlier","status":"settled","started":"2026-10-19T09:00:00Z","settled":"2026-10-19T10:00:00Z"}]`)
	inbox := func(agent, body string) {
		os.MkdirAll(filepath.Join(dir, "r1", "workers", agent), 0o755)
		os.WriteFile(filepath.Join(dir, "r1", "workers", agent, "inbox.jsonl"), []byte(body), 0o644)
	}
	inbox("later", `{"id":"m1","ts":"2026-10-19T11:00:00Z","from":"supervisor","type":"followup","subject":"more"}`+"\n")
	inbox("earlier", `{"id":"m2","ts":"2026-10-19T08:00:00Z","from":"supervisor","type":"followup","subject":"first"}`+"\n"+
		`{"id":"m3","ts":"2026-10-19T11:00:00Z","from":"supervisor","type":"reply","subject":"answer"}`+"\n")
	got := Settled(dir)
	if got["w1:p1"] {
		t.Error("a follow-up after the settle is new work whose result the supervisor has not taken")
	}
	if !got["w2:p1"] {
		t.Error("a follow-up before the settle, or a reply, leaves the worker reported")
	}
}
