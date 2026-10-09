package model

import (
	"reflect"
	"testing"
)

func agent(pane string) Agent {
	ws := pane[:2]
	return Agent{PaneID: pane, TabID: ws + ":t1", WorkspaceID: ws, Name: pane, Status: Idle}
}

func TestResolveLinksThreeLevels(t *testing.T) {
	agents := []Agent{agent("w1:p1"), agent("w2:p1"), agent("w3:p1")}
	d := Dispatch{Runs: []Run{
		{Name: "abc-1-top", Supervisor: "w1:p1", Entries: []Entry{
			{PaneID: "w2:p1", WorkspaceID: "w2", Worktree: "/src/api/_worktrees/a", Created: "2026-01-01T00:00:00Z"}}},
		{Name: "abc-1-sub", Supervisor: "w2:p1", Entries: []Entry{
			{PaneID: "w3:p1", WorkspaceID: "w3", Worktree: "/src/web/_worktrees/b", Created: "2026-01-02T00:00:00Z"}}},
	}}
	l := resolveLinks(agents, d)
	want := map[string]string{"w2:p1": "w1:p1", "w3:p1": "w2:p1"}
	if !reflect.DeepEqual(l.parent, want) {
		t.Fatalf("parent = %v, want %v", l.parent, want)
	}
	if l.repo["w2:p1"] != "api" || l.repo["w3:p1"] != "web" {
		t.Errorf("repo = %v", l.repo)
	}
	if l.ticket["w1:p1"] != "ABC-1" {
		t.Errorf("ticket = %v", l.ticket)
	}
}

func TestResolveLinksDeadEnds(t *testing.T) {
	agents := []Agent{agent("w1:p1"), agent("w2:p1")}
	d := Dispatch{Runs: []Run{
		// The supervisor pane is gone: no link, and no fallback.
		{Name: "a", Supervisor: "w9:p1", Entries: []Entry{{PaneID: "w2:p1", WorkspaceID: "w2", Created: "1"}}},
		// The worker and its workspace are gone.
		{Name: "b", Supervisor: "w1:p1", Entries: []Entry{{PaneID: "w8:p1", WorkspaceID: "w8", Created: "1"}}},
		// A tab worker whose tab is gone must not fall back to its supervisor's workspace.
		{Name: "c", Supervisor: "w1:p1", Entries: []Entry{{PaneID: "w1:p7", TabID: "w1:t7", WorkspaceID: "w1", Created: "1"}}},
	}}
	if l := resolveLinks(agents, d); len(l.parent) != 0 {
		t.Fatalf("parent = %v, want none", l.parent)
	}
}

func TestResolveLinksFallback(t *testing.T) {
	// The worker's pane was replaced: the entry follows its tab, then its workspace.
	tabWorker := Agent{PaneID: "w1:p9", TabID: "w1:t3", WorkspaceID: "w1", Name: "t", Status: Idle}
	agents := []Agent{agent("w1:p1"), tabWorker, agent("w2:p5")}
	d := Dispatch{Runs: []Run{{Name: "r", Supervisor: "w1:p1", Entries: []Entry{
		{PaneID: "w1:p3", TabID: "w1:t3", WorkspaceID: "w1", Created: "1"},
		{PaneID: "w2:p1", WorkspaceID: "w2", Created: "1"},
	}}}}
	want := map[string]string{"w1:p9": "w1:p1", "w2:p5": "w1:p1"}
	if l := resolveLinks(agents, d); !reflect.DeepEqual(l.parent, want) {
		t.Fatalf("parent = %v, want %v", l.parent, want)
	}
}

func TestResolveLinksLatestEntryWins(t *testing.T) {
	agents := []Agent{agent("w1:p1"), agent("w2:p1"), agent("w3:p1")}
	d := Dispatch{Runs: []Run{
		{Name: "new", Supervisor: "w2:p1", Entries: []Entry{{PaneID: "w3:p1", WorkspaceID: "w3", Created: "2026-02-01"}}},
		{Name: "old", Supervisor: "w1:p1", Entries: []Entry{{PaneID: "w3:p1", WorkspaceID: "w3", Created: "2026-01-01"}}},
	}}
	if l := resolveLinks(agents, d); l.parent["w3:p1"] != "w2:p1" {
		t.Fatalf("parent = %v, want w3:p1 under w2:p1", l.parent)
	}
}

func TestResolveLinksDropsCycle(t *testing.T) {
	agents := []Agent{agent("w1:p1"), agent("w2:p1")}
	d := Dispatch{Runs: []Run{
		{Name: "a", Supervisor: "w1:p1", Entries: []Entry{{PaneID: "w2:p1", WorkspaceID: "w2", Created: "1"}}},
		{Name: "b", Supervisor: "w2:p1", Entries: []Entry{{PaneID: "w1:p1", WorkspaceID: "w1", Created: "1"}}},
	}}
	l := resolveLinks(agents, d)
	if len(l.parent) != 1 {
		t.Fatalf("parent = %v, want exactly one link left", l.parent)
	}
}

func TestResolveLinksTicketNeedsAgreement(t *testing.T) {
	agents := []Agent{agent("w1:p1"), agent("w2:p1")}
	d := Dispatch{Runs: []Run{
		{Name: "abc-1-x", Supervisor: "w1:p1"},
		{Name: "abc-2-y", Supervisor: "w1:p1"},
		{Name: "abc-7-x", Supervisor: "w2:p1"},
		{Name: "abc-7-y", Supervisor: "w2:p1"},
	}}
	l := resolveLinks(agents, d)
	if _, ok := l.ticket["w1:p1"]; ok {
		t.Errorf("w1:p1 has runs for two tickets and must show none, got %v", l.ticket)
	}
	if l.ticket["w2:p1"] != "ABC-7" {
		t.Errorf("ticket = %v", l.ticket)
	}
}
