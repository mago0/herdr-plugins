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

// under sets the supervisor of each named pane.
func under(s Snapshot, supervisor string, panes ...string) Snapshot {
	agents := append([]Agent(nil), s.Agents...)
	for i := range agents {
		for _, p := range panes {
			if agents[i].PaneID == p {
				agents[i].Supervisor = supervisor
			}
		}
	}
	s.Agents = agents
	return s
}

// in sets the repository of the checkout a pane works in.
func in(s Snapshot, pane, repo string) Snapshot {
	agents := append([]Agent(nil), s.Agents...)
	for i := range agents {
		if agents[i].PaneID == pane {
			agents[i].Repo = repo
		}
	}
	s.Agents = agents
	return s
}

func TestBuildWorkspaceWorkersAndNoAgent(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "migration", "", 1), ws("w2", "add-routes", "infra", 2), ws("w3", "infra", "infra", 3)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), ag("w2:p1", "w2:t1", "add-routes")},
	}
	got := Build(under(s, "w1:p1", "w2:p1"))
	check(t, got, `
migration w 
  add-routes w infra
noagent infra
`)
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
	// A worker in a tab of its supervisor's workspace shows the repository of its own checkout.
	s = in(under(s, "w1:p1", "w1:p6", "w1:p7"), "w1:p7", "infra")
	check(t, Build(s), `
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
	got := Build(under(under(s, "w1:p1", "w2:p1"), "w1:p5", "w3:p1"))
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

func TestBuildTheOneSupervisorOfATabStandsForIt(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "add-probe", "api", 2)},
		Tabs:       []Tab{tab("w1:t1", "planning", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", "left"), ag("w1:p2", "w1:t1", "right"), ag("w2:p1", "w2:t1", "add-probe")},
	}
	// The other pane of the tab hangs under the supervisor, with its workers.
	check(t, Build(under(s, "w1:p1", "w2:p1")), `
ops w ops
  right p ops
  add-probe w api
`)
}

func TestBuildATabWithNoSupervisorOrSeveralHoldsItsPanes(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "a", "api", 2), ws("w3", "b", "api", 3)},
		Tabs:       []Tab{tab("w1:t1", "planning", 1), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1)},
		Agents: []Agent{ag("w1:p1", "w1:t1", "left"), ag("w1:p2", "w1:t1", "right"),
			ag("w2:p1", "w2:t1", "a"), ag("w3:p1", "w3:t1", "b")},
	}
	check(t, Build(s), `
ops w ops
  left p ops
  right p ops
a w api
b w api
`)
	check(t, Build(under(under(s, "w1:p1", "w2:p1"), "w1:p2", "w3:p1")), `
ops w ops
  left p ops
    a w api
  right p ops
    b w api
`)
}

func TestBuildWorkerTabWithSecondAgent(t *testing.T) {
	// A worker's tab gains a second agent: the tab row stays under the supervisor and holds both.
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "add-probe", "api", 2)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), ag("w2:p1", "w2:t1", "worker"), ag("w2:p2", "w2:t1", "helper")},
	}
	check(t, Build(under(s, "w1:p1", "w2:p1")), `
ops w ops
  add-probe w api
    worker p api
    helper p api
`)
}

func TestBuildHiddenPanes(t *testing.T) {
	hidden := func(pane, tabID, name string) Agent {
		a := ag(pane, tabID, name)
		a.Hide = true
		return a
	}
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "loops", "ops", 2)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", ""), hidden("w1:p3", "w1:t1", "waker"), hidden("w2:p1", "w2:t1", "probe")},
	}
	check(t, Build(s), `
ops w ops
noagent loops
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
	check(t, Build(under(s, "w2:p1", "w3:p1")), `
solo w 
lead w 
  worker w api
`)
	if got := Build(Snapshot{}); len(got.Roots)+len(got.NoAgent)+len(got.Hidden) != 0 {
		t.Errorf("empty input must give an empty tree, got %+v", got)
	}
}

func TestBuildHeadsOfDifferentSupervisorsStayApart(t *testing.T) {
	// One workspace holds a worker of each of two supervisors: each stays under its own.
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "a", "", 1), ws("w2", "b", "", 2), ws("w3", "shared", "api", 3)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1), tab("w3:t1", "one", 1), tab("w3:t2", "two", 2)},
		Agents: []Agent{ag("w1:p1", "w1:t1", ""), ag("w2:p1", "w2:t1", ""),
			ag("w3:p1", "w3:t1", "x"), ag("w3:p2", "w3:t2", "y")},
	}
	check(t, Build(under(under(s, "w1:p1", "w3:p1"), "w2:p1", "w3:p2")), `
a w 
  one t api
b w 
  two t api
`)
}

func TestBuildContainerCycleKeepsEveryAgent(t *testing.T) {
	// A worker supervises a pane in its own supervisor's tab. No agent may drop out of the tree.
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "ops", 1), ws("w2", "worker", "api", 2)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", "lead"), ag("w1:p2", "w1:t1", "side"), ag("w2:p1", "w2:t1", "w")},
	}
	seen := map[string]bool{}
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			seen[n.ID] = true
			walk(n.Children)
		}
	}
	got := Build(under(under(s, "w1:p1", "w2:p1"), "w2:p1", "w1:p2"))
	walk(got.Roots)
	for _, pane := range []string{"w1:p1", "w1:p2", "w2:p1"} {
		if !seen[pane] {
			t.Errorf("%s is not in the tree:\n%s", pane, outline(got))
		}
	}
	if len(got.NoAgent) != 0 {
		t.Errorf("both workspaces hold agents, got no-agent rows:\n%s", outline(got))
	}
}

func TestBuildGivesARowTheGitStateOfTheRepoItShows(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{{ID: "w1", Label: "ops", Repo: "flosports", Number: 1}},
		Tabs:       []Tab{{ID: "w1:t1", WorkspaceID: "w1", Number: 1}, {ID: "w1:t2", WorkspaceID: "w1", Number: 2}},
		Agents: []Agent{
			{PaneID: "w1:p1", TabID: "w1:t1", WorkspaceID: "w1", Git: Git{Repo: "flosports", Branch: "main", Ahead: 1}},
			{PaneID: "w1:p2", TabID: "w1:t2", WorkspaceID: "w1", Git: Git{Repo: "iam", Branch: "feat"}},
		},
	}
	got := map[string]Git{}
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			got[n.ID] = n.Git
			walk(n.Children)
		}
	}
	walk(Build(s).Roots)
	if want := (Git{Repo: "flosports", Branch: "main", Ahead: 1}); got["w1:p1"] != want {
		t.Errorf("row in its repo has %+v, want %+v", got["w1:p1"], want)
	}
	if got["w1:p2"] != (Git{}) {
		t.Errorf("row in another repo has %+v, want none", got["w1:p2"])
	}
}

func TestRootsKeepTheOrderOfTheirWorkspaces(t *testing.T) {
	// A supervisor does not go above a space that is before it in Herdr.
	s := under(Snapshot{
		Workspaces: []Workspace{ws("w1", "solo", "", 1), ws("w2", "lead", "", 2), ws("w3", "worker", "", 3), ws("w4", "new", "", 4)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1), tab("w4:t1", "1", 1)},
		Agents:     []Agent{ag("w1:p1", "w1:t1", "a"), ag("w2:p1", "w2:t1", "b"), ag("w3:p1", "w3:t1", "c"), ag("w4:p1", "w4:t1", "d")},
	}, "w2:p1", "w3:p1")
	check(t, Build(s), `
solo w 
lead w 
  worker w 
new w 
`)
}

func TestBuildKeepsTheLinkOfAnAgent(t *testing.T) {
	a := ag("w1:p1", "w1:t1", "a")
	a.Link = "https://example.com/1"
	got := Build(Snapshot{Workspaces: []Workspace{ws("w1", "one", "", 1)}, Tabs: []Tab{tab("w1:t1", "1", 1)}, Agents: []Agent{a}})
	if got.Roots[0].Link != a.Link {
		t.Fatalf("link = %q", got.Roots[0].Link)
	}
}
