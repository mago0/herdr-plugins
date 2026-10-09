package model

import (
	"strings"
	"testing"
)

func outline(t Tree) string {
	var b strings.Builder
	kind := map[Kind]string{KindPane: "p", KindTab: "t", KindWorkspace: "w"}
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		b.WriteString(strings.Repeat("  ", depth) + n.Label + " " + kind[n.Shown] + " " + n.Repo + "\n")
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, n := range t.Roots {
		walk(n, 0)
	}
	for _, n := range t.NoAgent {
		b.WriteString("noagent " + n.Label + "\n")
	}
	for _, n := range t.Hidden {
		b.WriteString("hidden " + n.Label + "\n")
	}
	return b.String()
}

func check(t *testing.T, got Tree, want string) {
	t.Helper()
	want = strings.TrimLeft(want, "\n")
	if o := outline(got); o != want {
		t.Fatalf("tree:\n%s\nwant:\n%s", o, want)
	}
}

func ws(id, label, repo string, n int) Workspace {
	return Workspace{ID: id, Label: label, Repo: repo, Number: n, Status: Idle}
}

func tab(id, label string, n int) Tab {
	return Tab{ID: id, WorkspaceID: strings.Split(id, ":")[0], Label: label, Number: n, Status: Idle}
}

func ag(pane, tabID, name string) Agent {
	return Agent{PaneID: pane, TabID: tabID, WorkspaceID: strings.Split(pane, ":")[0], Name: name, Status: Idle}
}

func run(name, sup string, entries ...Entry) Run { return Run{Name: name, Supervisor: sup, Entries: entries} }

func TestBuildWorkspaceWorkersAndNoAgent(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "migration", "", 1), ws("w2", "add-routes", "infra", 2), ws("w3", "infra", "infra", 3)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), ag("w2:p1", "w2:t1", "add-routes")},
	}
	d := Dispatch{Runs: []Run{run("abc-5-m0", "w1:p1",
		Entry{PaneID: "w2:p1", WorkspaceID: "w2", Worktree: "/src/infra/_worktrees/x", Created: "1"})}}
	got := Build(s, d)
	check(t, got, `
migration w 
  add-routes w infra
noagent infra
`)
	if got.Roots[0].Ticket != "ABC-5" {
		t.Errorf("root ticket = %q", got.Roots[0].Ticket)
	}
	if got.Roots[0].Focus != KindPane || got.Roots[0].ID != "w1:p1" {
		t.Errorf("a row that stands for a workspace still focuses its agent pane: %+v", got.Roots[0])
	}
}

func TestBuildTabWorkerUnderSupervisor(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "scheduler", "scheduler", 1)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w1:t3", "build-images", 3), tab("w1:t4", "add-nodepools", 4)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", "s"), ag("w1:p6", "w1:t3", "a"), ag("w1:p7", "w1:t4", "b")},
	}
	d := Dispatch{Runs: []Run{run("r", "w1:p1",
		Entry{PaneID: "w1:p6", TabID: "w1:t3", WorkspaceID: "w1", Worktree: "/src/scheduler/_worktrees/a", Created: "1"},
		Entry{PaneID: "w1:p7", TabID: "w1:t4", WorkspaceID: "w1", Worktree: "/src/infra/_worktrees/b", Created: "1"})}}
	check(t, Build(s, d), `
scheduler w scheduler
  build-images t scheduler
  add-nodepools t infra
`)
}

func TestBuildTwoSupervisingTabs(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "review-86", "iam", 2), ws("w3", "add-probe", "api", 3)},
		Tabs:       []Tab{tab("w1:t1", "monitor", 1), tab("w1:t9", "planning", 9), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1)},
		Agents: []Agent{ag("w1:p1", "w1:t1", ""), ag("w1:p5", "w1:t9", ""),
			ag("w2:p1", "w2:t1", "review-86"), ag("w3:p1", "w3:t1", "add-probe")},
	}
	d := Dispatch{Runs: []Run{
		run("reviews", "w1:p1", Entry{PaneID: "w2:p1", WorkspaceID: "w2", Created: "1"}),
		run("plan", "w1:p5", Entry{PaneID: "w3:p1", WorkspaceID: "w3", Created: "1"}),
	}}
	got := Build(s, d)
	check(t, got, `
ops w ops
  monitor t ops
    review-86 w iam
  planning t ops
    add-probe w api
`)
	if got.Roots[0].Focus != KindWorkspace || got.Roots[0].ID != "w1" {
		t.Errorf("a workspace container focuses the workspace: %+v", got.Roots[0])
	}
}

func TestBuildTwoAgentsInOneTab(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "add-probe", "api", 2)},
		Tabs:       []Tab{tab("w1:t1", "planning", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", "left"), ag("w1:p2", "w1:t1", "right"), ag("w2:p1", "w2:t1", "add-probe")},
	}
	d := Dispatch{Runs: []Run{run("r", "w1:p1", Entry{PaneID: "w2:p1", WorkspaceID: "w2", Created: "1"})}}
	check(t, Build(s, d), `
ops w ops
  left p ops
    add-probe w api
  right p ops
`)
}

func TestBuildDispatchedTabWithSecondAgent(t *testing.T) {
	// A worker's tab gains a second agent: the tab row stays under the supervisor and holds both.
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "add-probe", "api", 2)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), ag("w2:p1", "w2:t1", "worker"), ag("w2:p2", "w2:t1", "helper")},
	}
	d := Dispatch{Runs: []Run{run("r", "w1:p1", Entry{PaneID: "w2:p1", WorkspaceID: "w2", Created: "1"})}}
	check(t, Build(s, d), `
ops w ops
  add-probe w api
    worker p api
    helper p api
`)
}

func TestBuildHiddenPanes(t *testing.T) {
	hide := ag("w1:p3", "w1:t1", "waker")
	hide.Hide = true
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "loops", "ops", 2)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), ag("w1:p2", "w1:t1", "pr-watch"), hide, ag("w2:p1", "w2:t1", "probe")},
	}
	d := Dispatch{Tracked: map[string]bool{"w1:p2": true, "w2:p1": true}}
	check(t, Build(s, d), `
ops w ops
noagent loops
hidden pr-watch
hidden waker
hidden probe
`)
}

func TestBuildRootOrderAndEmpty(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "solo", "", 1), ws("w2", "lead", "", 2), ws("w3", "worker", "api", 3)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), ag("w2:p1", "w2:t1", ""), ag("w3:p1", "w3:t1", "")},
	}
	d := Dispatch{Runs: []Run{run("r", "w2:p1", Entry{PaneID: "w3:p1", WorkspaceID: "w3", Created: "1"})}}
	check(t, Build(s, d), `
lead w 
  worker w api
solo w 
`)
	if got := Build(Snapshot{}, Dispatch{}); len(got.Roots)+len(got.NoAgent)+len(got.Hidden) != 0 {
		t.Errorf("empty input must give an empty tree, got %+v", got)
	}
}
