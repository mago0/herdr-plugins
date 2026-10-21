package model

import (
	"fmt"
	"strings"
	"testing"
)

func outline(t Tree) string {
	var b strings.Builder
	kind := map[Kind]string{KindPane: "a", KindShell: "s", KindWorkspace: "e"}
	var walk func(n *Node, depth int)
	walk = func(n *Node, depth int) {
		mark := ""
		if n.Worker {
			mark = " worker"
		}
		fmt.Fprintf(&b, "%s%s %s%s\n", strings.Repeat("  ", depth), kind[n.Shown], n.Label, mark)
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, n := range t.Roots {
		walk(n, 0)
	}
	return b.String()
}

func check(t *testing.T, got Tree, want string) {
	t.Helper()
	want = strings.TrimLeft(want, "\n")
	if o := outline(got); o != want {
		t.Fatalf("efforts:\n%s\nwant:\n%s", o, want)
	}
}

func ws(id, label, repo string, n int) Workspace {
	return Workspace{ID: id, Label: label, Repo: repo, Number: n, Status: Idle}
}

func ag(pane, name string) Agent {
	ws, _, _ := strings.Cut(pane, ":")
	return Agent{PaneID: pane, TabID: ws + ":t1", WorkspaceID: ws, Name: name, Status: Idle}
}

func sh(pane, label, where string) Shell {
	ws, _, _ := strings.Cut(pane, ":")
	return Shell{PaneID: pane, TabID: ws + ":t1", WorkspaceID: ws, Label: label, Where: where}
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

func TestBuildEffortListsShellsThenAgents(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "flosports", 1)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w1:p2", "second")},
		Shells:     []Shell{sh("w1:p3", "logs", "flosports")},
	}
	check(t, Build(s), `
e ops
  s logs
  a lead
  a second
`)
}

func TestBuildWorkerHangsUnderItsSupervisorWithItsShells(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "add-probe", "iam", 2), ws("w3", "notes", "", 3)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w2:p1", "add-probe")},
		Shells:     []Shell{sh("w2:p2", "reviewr", "iam"), sh("w3:p1", "", "~/notes")},
	}
	check(t, Build(under(s, "w1:p1", "w2:p1")), `
e ops
  a lead
    a add-probe worker
      s reviewr
e notes
  s ~/notes
`)
}

func TestBuildALinkInsideOneWorkspaceDoesNotNest(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w1:p2", "helper")},
	}
	check(t, Build(under(s, "w1:p1", "w1:p2")), `
e ops
  a lead
  a helper
`)
}

func TestBuildAWorkerOfAWorkerNests(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "mid", "", 2), ws("w3", "leaf", "", 3)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w2:p1", "mid"), ag("w3:p1", "leaf")},
	}
	s = under(under(s, "w1:p1", "w2:p1"), "w2:p1", "w3:p1")
	check(t, Build(s), `
e ops
  a lead
    a mid worker
      a leaf worker
`)
}

func TestBuildWorkersKeepTheOrderOfTheirWorkspaces(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w3", "b", "", 3), ws("w2", "a", "", 2)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w3:p1", "b"), ag("w2:p1", "a")},
	}
	check(t, Build(under(s, "w1:p1", "w3:p1", "w2:p1")), `
e ops
  a lead
    a a worker
    a b worker
`)
}

func TestBuildAWorkspaceWithAnAgentOfItsOwnIsAnEffortBesideItsWorker(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "side", "", 2)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w2:p1", "w"), ag("w2:p2", "own")},
		Shells:     []Shell{sh("w2:p3", "sh", "")},
	}
	check(t, Build(under(s, "w1:p1", "w2:p1")), `
e ops
  a lead
    a w worker
e side
  s sh
  a own
`)
}

func TestBuildShellsOfAWorkerWorkspaceGoToItsFirstWorker(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "pair", "", 2)},
		Agents:     []Agent{ag("w1:p1", "lead"), ag("w2:p1", "one"), ag("w2:p2", "two")},
		Shells:     []Shell{sh("w2:p3", "sh", "")},
	}
	check(t, Build(under(s, "w1:p1", "w2:p1", "w2:p2")), `
e ops
  a lead
    a one worker
      s sh
    a two worker
`)
}

func TestBuildWorkerWithAGoneSupervisorIsAnEffort(t *testing.T) {
	s := Snapshot{
		Workspaces: []Workspace{ws("w2", "orphan", "", 2)},
		Agents:     []Agent{ag("w2:p1", "orphan")},
	}
	check(t, Build(under(s, "w1:p1", "w2:p1")), `
e orphan
  a orphan
`)
}

func TestBuildEmptyWorkspaceAndHiddenAgent(t *testing.T) {
	hidden := ag("w1:p2", "watch")
	hidden.Hide = true
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "empty", "", 2)},
		Agents:     []Agent{ag("w1:p1", "lead"), hidden},
	}
	check(t, Build(s), `
e ops
  a lead
e empty
`)
}

func TestBuildEffortTakesTheLabelAndLinkOfItsFirstAgent(t *testing.T) {
	lead := ag("w1:p1", "lead")
	lead.Tag, lead.Link = "SRE-923", "https://linear.app/flosports/issue/SRE-923"
	second := ag("w1:p2", "second")
	second.Tag = "SRE-1"
	got := Build(Snapshot{Workspaces: []Workspace{ws("w1", "ops", "", 1)}, Agents: []Agent{lead, second}})
	if e := got.Roots[0]; e.Tag != "SRE-923" || e.Link != lead.Link || e.Focus != KindWorkspace || e.ID != "w1" {
		t.Fatalf("effort = %+v", e)
	}
}

func TestBuildReportedOnlyForAnAgentThatIsNotWorking(t *testing.T) {
	idle, busy := ag("w2:p1", "idle"), ag("w3:p1", "busy")
	idle.Reported, busy.Reported, busy.Status = true, true, Working
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "a", "", 2), ws("w3", "b", "", 3)},
		Agents:     []Agent{ag("w1:p1", "lead"), idle, busy},
	}
	workers := Build(under(s, "w1:p1", "w2:p1", "w3:p1")).Roots[0].Children[0].Children
	if !workers[0].Reported || workers[1].Reported {
		t.Fatalf("reported = %v, %v; want true, false", workers[0].Reported, workers[1].Reported)
	}
}

func TestBuildKeepsTheGitStateOfTheRepoARowShows(t *testing.T) {
	mine, other := ag("w2:p1", "mine"), ag("w3:p1", "other")
	mine.Repo, mine.Git = "iam", Git{Repo: "iam", Branch: "feat", Ahead: 1}
	other.Git = Git{Repo: "iam", Branch: "feat"}
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1), ws("w2", "a", "iam", 2), ws("w3", "b", "ops", 3)},
		Agents:     []Agent{ag("w1:p1", "lead"), mine, other},
	}
	workers := Build(under(s, "w1:p1", "w2:p1", "w3:p1")).Roots[0].Children[0].Children
	if workers[0].Git.Branch != "feat" || workers[0].Git.Ahead != 1 {
		t.Errorf("a worker keeps the Git state of its own repo, got %+v", workers[0].Git)
	}
	if workers[1].Repo != "ops" || workers[1].Git != (Git{}) {
		t.Errorf("a worker with no repo of its own shows its workspace's repo and no branch, got %q %+v", workers[1].Repo, workers[1].Git)
	}
}

func TestBuildAnAgentAloneInANamedTabTakesTheTabName(t *testing.T) {
	named, numbered := ag("w1:p1", "lead"), ag("w1:p2", "second")
	named.Title, numbered.Title = "Herdr layout mockup", "Infra review"
	named.TabID, numbered.TabID = "w1:t1", "w1:t2"
	a, b := ag("w1:p3", "a"), ag("w1:p4", "b")
	a.TabID, b.TabID = "w1:t3", "w1:t3"
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1)},
		Tabs: []Tab{
			{ID: "w1:t1", WorkspaceID: "w1", Label: "Dispatch"},
			{ID: "w1:t2", WorkspaceID: "w1", Label: "2"},
			{ID: "w1:t3", WorkspaceID: "w1", Label: "Pair"},
		},
		Agents: []Agent{named, numbered, a, b},
	}
	check(t, Build(s), `
e ops
  a Dispatch
  a Infra review
  a a
  a b
`)
}

func TestBuildAPaneNameComesBeforeTheTabNameAndTheTitle(t *testing.T) {
	renamed := ag("w1:p1", "lead")
	renamed.Title, renamed.PaneName, renamed.TabID = "Risk sweep prompt", "Risk Sweep", "w1:t1"
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "ops", "", 1)},
		Tabs:       []Tab{{ID: "w1:t1", WorkspaceID: "w1", Label: "Soak"}},
		Agents:     []Agent{renamed},
	}
	check(t, Build(s), `
e ops
  a Risk Sweep
`)
}

func TestBuildADispatchParentWithOnlyItsRootShellIsNoEffort(t *testing.T) {
	parent := ws("w2", "puppet", "puppet", 2)
	parent.DispatchParent = true
	review := ws("w3", "review-puppet-679", "puppet", 3)
	review.Worktree = true
	s := under(Snapshot{
		Workspaces: []Workspace{ws("w1", "Main", "flosports", 1), parent, review},
		Agents:     []Agent{ag("w1:p1", "monitor"), ag("w3:p1", "review")},
		Shells:     []Shell{sh("w2:p1", "", "puppet")},
	}, "w1:p1", "w3:p1")
	check(t, Build(s), `
e Main
  a monitor
    a review worker
`)
}

func TestBuildADispatchParentInUseIsAnEffort(t *testing.T) {
	for name, edit := range map[string]func(*Snapshot){
		"no worktree left":  func(s *Snapshot) { s.Workspaces = s.Workspaces[:1] },
		"a named shell":     func(s *Snapshot) { s.Shells[0].Label = "deploy" },
		"a shell elsewhere": func(s *Snapshot) { s.Shells[0].Where = "puppet/modules" },
		"an agent":          func(s *Snapshot) { s.Agents = []Agent{ag("w2:p2", "lead")} },
	} {
		parent := ws("w2", "puppet", "puppet", 2)
		parent.DispatchParent = true
		review := ws("w3", "review", "puppet", 3)
		review.Worktree = true
		s := Snapshot{Workspaces: []Workspace{parent, review}, Shells: []Shell{sh("w2:p1", "", "puppet")}}
		edit(&s)
		if roots := Build(s).Roots; len(roots) == 0 || roots[0].ID != "w2" {
			t.Errorf("%s: the parent is not an effort", name)
		}
	}
}
