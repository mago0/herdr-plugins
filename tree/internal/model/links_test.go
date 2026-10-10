package model

import (
	"reflect"
	"testing"
)

func agent(pane, supervisor string) Agent {
	ws := pane[:2]
	return Agent{PaneID: pane, TabID: ws + ":t1", WorkspaceID: ws, Name: pane, Status: Idle, Supervisor: supervisor}
}

func TestResolveLinksThreeLevels(t *testing.T) {
	agents := []Agent{agent("w1:p1", ""), agent("w2:p1", "w1:p1"), agent("w3:p1", "w2:p1")}
	want := map[string]string{"w2:p1": "w1:p1", "w3:p1": "w2:p1"}
	if got := resolveLinks(agents); !reflect.DeepEqual(got, want) {
		t.Fatalf("parent = %v, want %v", got, want)
	}
}

func TestResolveLinksLeavesOutASupervisorThatIsNotShown(t *testing.T) {
	// The supervisor pane is gone or hidden, or the pane names itself.
	agents := []Agent{agent("w1:p1", "w9:p1"), agent("w2:p1", "w2:p1")}
	if got := resolveLinks(agents); len(got) != 0 {
		t.Fatalf("parent = %v, want none", got)
	}
}

func TestResolveLinksDropsCycle(t *testing.T) {
	agents := []Agent{agent("w1:p1", "w2:p1"), agent("w2:p1", "w1:p1")}
	if got := resolveLinks(agents); len(got) != 1 {
		t.Fatalf("parent = %v, want exactly one link left", got)
	}
}
