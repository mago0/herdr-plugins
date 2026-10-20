# SRE Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the supervisor tree in the `herdr-tree` sidebar pane with one entry for each workspace (effort), its shells and agents under it, and workers under the agent that dispatched them.

**Architecture:** The `tree` program keeps its three layers. `internal/source` reads Herdr, Git and (new) the dispatch ledgers. `internal/model` builds efforts from that data with no I/O. `internal/view` flattens efforts to rows, draws them, and runs the Bubble Tea pane. The data source, focus, menus, steps, drag and fold storage stay; the model's tree build, the rows, and the drawing are rewritten.

**Tech Stack:** Go (module `github.com/mago0/herdr-plugins/tree`, `go 1.24.0` directive, Go 1.27 toolchain on this machine), Bubble Tea, Lip Gloss, BurntSushi TOML.

**Spec:** `docs/2026-10-19-sre-sidebar-design.md` (approved 2026-10-19). Read it before Task 1.

## Global Constraints

- Work in `~/Projects/personal/herdr-plugins` on `main`. Run Go commands from `tree/`: `cd tree && go test ./...`.
- `tree/bin/tree` is the binary the live Herdr sidebar runs. The Herdr client restarts the pane when that file changes. Never write `bin/tree` before Task 6. `test/smoke.sh` builds `bin/tree`, so it also runs only in Task 6. For a local binary, build to the session scratchpad: `go build -o "$SCRATCH/tree-new" ./cmd/tree`.
- Commit style of this repo: subject `tree: <lowercase summary>` (or `tree, dispatch: ...`), a short body, then the line `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Do not push.
- Code comments say what a thing is for. Keep them to one or two lines, in the plain style of the existing code. No history, no ticket numbers.
- No em dashes or en dashes in code, comments, docs or commit messages.
- Do not change the Herdr config, sessions or servers.
- Plain theme in tests: `Plain: true`, `ShellGlyph "$"`, `WorktreeGlyph "⎇"`. Plain state dots: `!` blocked, `*` working, `+` done, `o` idle, `~` stalled, `·` unknown.

## Review Focus

1. **A pane id that appears in more than one ledger.** A new dispatch can land on a pane id an old run used. The latest entry by `started` must win, so an old settled entry does not mark a new working worker `reported`. Test: `TestSettledReadsTheLatestDispatchOfEachPane` (Task 2).
2. **A ledger that does not parse** (cut file, hand edit). The pane must skip that file and keep the others. Test: same test, `broken` run (Task 2).
3. **A worker whose supervisor pane is gone.** The worker must not disappear: it becomes a top-level agent of its own workspace, which is then an effort. Test: `TestBuildWorkerWithAGoneSupervisorIsAnEffort` (Task 1).
4. **A workspace whose agents are all workers, with two workers.** Its shells must show once, under its first worker, and not be dropped or doubled. Test: `TestBuildShellsOfAWorkerWorkspaceGoToItsFirstWorker` (Task 1).
5. **A sidebar narrower than the row chrome** (12 columns and less). Rendering must not panic, and no line may be wider than the pane. Test: `TestRenderVeryNarrowPaneStaysInside` (Task 3).

---

### Task 1: Model builds efforts

**Files:**
- Modify: `tree/internal/model/types.go`
- Modify: `tree/internal/model/build.go` (rewrite)
- Modify: `tree/internal/model/labels.go`
- Create: `tree/internal/model/web.go`
- Test: `tree/internal/model/build_test.go` (rewrite), `tree/internal/model/labels_test.go`, `tree/internal/model/answers_test.go:64-86`, `tree/internal/model/web_test.go` (create)

**Interfaces:**
- Consumes: `resolveLinks(agents []Agent) map[string]string` (unchanged, `links.go`), `Answers(kind string, lines []string) []Answer` (unchanged).
- Produces:
  - `type Shell struct { PaneID, TabID, WorkspaceID, Label, Where string }`
  - `Snapshot.Shells []Shell`, `Agent.Reported bool`
  - `KindShell` (a new `Kind` value), `Node.Worker bool`, `Node.Reported bool`
  - `func Build(s Snapshot) Tree`: `Tree.Roots` are efforts (`Shown == KindWorkspace`). Each effort's `Children` are its shells (`Shown == KindShell`) then its top-level agents (`Shown == KindPane`). An agent's `Children` are its workers. A worker's `Children` are its workspace's shells (first worker only) then its own workers.
  - `func AgentLabel(a Agent) string`: title, else name, else kind, else `"agent"`.
  - `func IsProject(link string) bool`, `func PRNumber(link string) string`
  - `KindTab`, `KindGroup`, `Tree.NoAgent`, `Tree.Hidden` stay for now so `internal/view` still compiles. Task 4 removes them.

- [ ] **Step 1: Write the failing tests**

Replace all of `tree/internal/model/build_test.go` with:

```go
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
```

In `tree/internal/model/answers_test.go`, `TestBuildPutsTheAnswersOfABlockedAgentOnItsRow`: the agents are now one level below their effort, and `tab()` and the three-argument `ag()` are gone. Replace the function with:

```go
func TestBuildPutsTheAnswersOfABlockedAgentOnItsRow(t *testing.T) {
	a := ag("w1:p1", "")
	a.Kind, a.Status, a.Dialog, a.BlockerLines = "claude", Blocked, "d1a109", []string{"Allow?", "❯ 1. Yes", "2. No"}
	idle := ag("w2:p1", "")
	idle.Kind, idle.Dialog, idle.BlockerLines = "claude", "d1a109", []string{"Allow?", "❯ 1. Yes", "2. No"}
	unnamed := ag("w3:p1", "")
	unnamed.Kind, unnamed.Status, unnamed.BlockerLines = "claude", Blocked, []string{"Allow?", "❯ 1. Yes", "2. No"}
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "a", "", 1), ws("w2", "b", "", 2), ws("w3", "c", "", 3)},
		Agents:     []Agent{a, idle, unnamed},
	}
	got := Build(s)
	row := func(i int) *Node { return got.Roots[i].Children[0] }
	if len(row(0).Answers) != 2 || row(0).Dialog != "d1a109" {
		t.Fatalf("a blocked row has its answers and its dialog, got %+v", row(0))
	}
	if len(row(1).Answers) != 0 {
		t.Fatalf("a row that is not blocked has no answers, got %+v", row(1).Answers)
	}
	if len(row(2).Answers) != 0 {
		t.Fatalf("with no dialog name Herdr cannot check an answer, so the row has none, got %+v", row(2).Answers)
	}
}
```

Replace the case table in `tree/internal/model/labels_test.go` with:

```go
	cases := []struct {
		a    Agent
		want string
	}{
		{Agent{Name: "add-probe", Kind: "claude", Title: "Review infra"}, "Review infra"},
		{Agent{Name: "add-probe", Kind: "claude"}, "add-probe"},
		{Agent{Kind: "pr-watch-86"}, "pr-watch-86"},
		{Agent{Kind: "codex", Title: "0123456789012345678901234567890123456789"}, "0123456789012345678901234567890123"},
		{Agent{Kind: "claude"}, "claude"},
		{Agent{}, "agent"},
	}
```

Create `tree/internal/model/web_test.go`:

```go
package model

import "testing"

func TestIsProject(t *testing.T) {
	for link, want := range map[string]bool{
		"https://linear.app/flosports/project/gke-aws-eks-migration-f425468449cd": true,
		"https://linear.app/flosports/issue/SRE-194/migrate-job-scheduler":         false,
		"https://example.com/flosports/project/x":                                  false,
		"": false,
	} {
		if got := IsProject(link); got != want {
			t.Errorf("IsProject(%q) = %v, want %v", link, got, want)
		}
	}
}

func TestPRNumber(t *testing.T) {
	for link, want := range map[string]string{
		"https://github.com/flocasts/iam/pull/57":       "#57",
		"https://github.com/flocasts/iam/pull/57/files": "#57",
		"https://github.com/flocasts/iam/issues/57":     "",
		"https://linear.app/flosports/issue/SRE-194":    "",
		"": "",
	} {
		if got := PRNumber(link); got != want {
			t.Errorf("PRNumber(%q) = %q, want %q", link, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd tree && go test ./internal/model/`
Expected: build failure: `undefined: Shell`, `unknown field Shells`, `undefined: KindShell`, `n.Worker undefined`, `undefined: IsProject`.

- [ ] **Step 3: Change the types**

In `tree/internal/model/types.go`:

Change the package comment to `// Package model builds the efforts of the sidebar from plain data. It does no I/O.`

Add `Reported` at the end of `Agent`, after `Dialog string`:

```go
	// Reported is true when the dispatch ledger has the agent's worker_done taken by its supervisor.
	Reported bool
```

Add after the `Agent` type:

```go
// Shell is one pane that hosts no agent.
type Shell struct {
	PaneID, TabID, WorkspaceID string
	// Label is the name the user gave the pane, or "".
	Label string
	// Where is "<repo>/<path in the repo>" inside a repository, else the path with home as ~.
	Where string
}
```

Add `Shells []Shell` to `Snapshot`, after `Agents []Agent`.

Change the `Kind` block to:

```go
const (
	KindPane Kind = iota
	KindTab
	KindWorkspace
	KindGroup
	// KindShell is a pane with no agent.
	KindShell
)
```

In `Node`, delete the `order    [3]int` field and add, before `Children []*Node`:

```go
	// Worker is true for an agent that an agent in another workspace supervises.
	Worker bool
	// Reported is true for a worker whose result its supervisor took and that is not working again.
	Reported bool
```

Change the `Tree` comment to `// Tree is the built view: one effort for each workspace that has its own agent or no agent.`

- [ ] **Step 4: Write the label and link helpers**

Replace `tree/internal/model/labels.go` with:

```go
package model

const titleRunes = 34

// AgentLabel is the row label for an agent: the title its session set, else its name, else its kind.
func AgentLabel(a Agent) string {
	if a.Title != "" {
		r := []rune(a.Title)
		if len(r) > titleRunes {
			r = r[:titleRunes]
		}
		return string(r)
	}
	if a.Name != "" {
		return a.Name
	}
	if a.Kind != "" {
		return a.Kind
	}
	return "agent"
}
```

Create `tree/internal/model/web.go`:

```go
package model

import (
	"net/url"
	"strings"
)

// segments is the path of link split at each slash, when link is a web address on host.
func segments(link, host string) []string {
	u, err := url.Parse(link)
	if err != nil || u.Host != host {
		return nil
	}
	return strings.Split(strings.Trim(u.Path, "/"), "/")
}

// IsProject is true for the address of a Linear project, as opposed to that of an issue.
func IsProject(link string) bool {
	p := segments(link, "linear.app")
	return len(p) >= 3 && p[1] == "project"
}

// PRNumber is "#<n>" for the address of a GitHub pull request, else "".
func PRNumber(link string) string {
	p := segments(link, "github.com")
	if len(p) >= 4 && p[2] == "pull" && p[3] != "" {
		return "#" + p[3]
	}
	return ""
}
```

- [ ] **Step 5: Rewrite Build**

Replace all of `tree/internal/model/build.go` with:

```go
package model

import "sort"

// of is the Git state g when it is that of the repository a row shows.
func of(g Git, repo string) Git {
	if g.Repo != repo {
		return Git{}
	}
	return g
}

// Build turns the live Herdr state into efforts: one for each workspace with an agent of its own
// or no agent, holding its shells, its agents, and under each agent the workers it supervises.
func Build(s Snapshot) Tree {
	workspaces := append([]Workspace(nil), s.Workspaces...)
	sort.SliceStable(workspaces, func(i, j int) bool { return workspaces[i].Number < workspaces[j].Number })
	place := map[string]int{}
	wsByID := map[string]Workspace{}
	for i, w := range workspaces {
		place[w.ID] = i
		wsByID[w.ID] = w
	}

	var visible []Agent
	for _, a := range s.Agents {
		if !a.Hide {
			visible = append(visible, a)
		}
	}
	up := resolveLinks(visible)
	home := map[string]string{}
	for _, a := range visible {
		home[a.PaneID] = a.WorkspaceID
	}
	// A supervisor in the same workspace does not make a worker: both stay agents of that workspace.
	for pane, sup := range up {
		if home[pane] == home[sup] {
			delete(up, pane)
		}
	}

	nodes := map[string]*Node{}
	for _, a := range visible {
		repo := a.Repo
		if repo == "" {
			repo = wsByID[a.WorkspaceID].Repo
		}
		var answers []Answer
		if a.Status == Blocked && a.Dialog != "" {
			answers = Answers(a.Kind, a.BlockerLines)
		}
		_, worker := up[a.PaneID]
		nodes[a.PaneID] = &Node{
			ID: a.PaneID, Focus: KindPane, Shown: KindPane, TabID: a.TabID, WorkspaceID: a.WorkspaceID,
			Label: AgentLabel(a), Status: a.Status, Repo: repo, Where: a.Where, Worktree: a.Worktree,
			Git: of(a.Git, repo), Tag: a.Tag, Link: a.Link, Pending: a.Pending, Stalled: a.Stalled,
			Blocker: a.Blocker, Answers: answers, Dialog: a.Dialog,
			Worker: worker, Reported: a.Reported && a.Status != Working,
		}
	}
	for _, a := range visible {
		if sup, ok := up[a.PaneID]; ok {
			nodes[sup].Children = append(nodes[sup].Children, nodes[a.PaneID])
		}
	}
	for _, n := range nodes {
		sort.SliceStable(n.Children, func(i, j int) bool {
			return place[n.Children[i].WorkspaceID] < place[n.Children[j].WorkspaceID]
		})
	}

	shells := map[string][]*Node{}
	for _, p := range s.Shells {
		label := p.Label
		if label == "" {
			label = p.Where
		}
		shells[p.WorkspaceID] = append(shells[p.WorkspaceID], &Node{
			ID: p.PaneID, Focus: KindPane, Shown: KindShell, TabID: p.TabID, WorkspaceID: p.WorkspaceID,
			Label: label, Status: Unknown, Where: p.Where,
		})
	}

	var tree Tree
	for _, w := range workspaces {
		var own, workers []*Node
		for _, a := range visible {
			if a.WorkspaceID != w.ID {
				continue
			}
			if n := nodes[a.PaneID]; n.Worker {
				workers = append(workers, n)
			} else {
				own = append(own, n)
			}
		}
		// A workspace whose agents all work for agents elsewhere is part of their effort.
		if len(own) == 0 && len(workers) > 0 {
			first := workers[0]
			first.Children = append(append([]*Node(nil), shells[w.ID]...), first.Children...)
			continue
		}
		e := &Node{
			ID: w.ID, Focus: KindWorkspace, Shown: KindWorkspace, WorkspaceID: w.ID,
			Label: w.Label, Status: w.Status, Repo: w.Repo, Where: w.Where, Worktree: w.Worktree, Git: of(w.Git, w.Repo),
		}
		if len(own) > 0 {
			e.Tag, e.Link = own[0].Tag, own[0].Link
		}
		e.Children = append(append([]*Node(nil), shells[w.ID]...), own...)
		tree.Roots = append(tree.Roots, e)
	}
	return tree
}
```

- [ ] **Step 6: Run the model tests**

Run: `cd tree && go test ./internal/model/`
Expected: `ok`.

- [ ] **Step 7: Check that the rest still builds**

Run: `cd tree && go vet ./... && go test ./...`
Expected: vet clean. `internal/view` tests may now fail on behavior (they still expect the tree), but every package must compile. If a package does not compile, fix the reference (the old kinds and tree fields are still there on purpose).

- [ ] **Step 8: Commit**

```bash
cd ~/Projects/personal/herdr-plugins
git add tree/internal/model
git commit -F - <<'EOF'
tree: build efforts in place of the supervisor tree

Build makes one effort for each workspace with an agent of its own or
none, with its shells first, then its agents, and under each agent the
workers in other workspaces that it supervises. Agent rows take the
session title first.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: Source reads shells, worktree branches and the dispatch ledgers

**Files:**
- Modify: `tree/internal/source/herdr.go:69-199`
- Modify: `tree/internal/source/git.go` (rewrite)
- Modify: `tree/internal/source/checkout.go` (add `shellWhere`)
- Create: `tree/internal/source/ledger.go`
- Test: `tree/internal/source/git_test.go`, `tree/internal/source/checkout_test.go`, `tree/internal/source/herdr_test.go`, `tree/internal/source/ledger_test.go` (create)

**Interfaces:**
- Consumes: `model.Shell`, `model.Snapshot.Shells` (Task 1).
- Produces:
  - `Snapshot(c Caller)` fills `Shells` with every pane that has no agent, and gives each agent `Git` against `origin/HEAD` for main checkouts and linked worktrees alike.
  - `func Settled(dir string) map[string]bool`: panes whose latest dispatch is `settled`.
  - `func LedgerDir() string`: `$XDG_STATE_HOME/agent-dispatch/runs`, default `~/.local/state/agent-dispatch/runs`.

- [ ] **Step 1: Write the failing tests**

In `tree/internal/source/git_test.go`:

Change the body of `TestBranchReadsHEAD` so it passes the Git folder:

```go
		if got := branch(filepath.Join(repo, ".git")); got != want {
```

Replace the last two checks of `TestGitStateCountsAgainstTheUpstream` (from `want := model.Git{...}` to the end of the function) with:

```go
	want := model.Git{Repo: "iam", Branch: "main", Ahead: 2, Behind: 1}
	if got := gitState(repo, repo, false, upstream); got != want {
		t.Fatalf("gitState = %+v, want %+v", got, want)
	}
	// A linked worktree has its own branch, counted against the default branch of origin.
	run(repo, "remote", "set-head", "origin", "main")
	wt := filepath.Join(repo, "_worktrees", "feat")
	run(repo, "worktree", "add", "-q", "-b", "feat", wt)
	run(wt, "commit", "-q", "--allow-empty", "-m", "five")
	want = model.Git{Repo: "iam", Branch: "feat", Ahead: 3, Behind: 1}
	if got := gitState(repo, wt, true, base); got != want {
		t.Fatalf("gitState of a linked worktree = %+v, want %+v", got, want)
	}
```

In `TestGitStateCountsAgainOnlyAfterTheBranchOrTheClockMoves`, change the fake and each call:

```go
	count, now = func(string, string) (int, int) { calls++; return calls, 0 }, func() time.Time { return at }
```

and replace every `gitState(repo, false)` in that function with `gitState(repo, repo, false, upstream)`.

Add to `git_test.go`:

```go
func TestGitDirFollowsTheGitFileOfALinkedWorktree(t *testing.T) {
	top := t.TempDir()
	if got := gitDir(top); got != filepath.Join(top, ".git") {
		t.Errorf("a main checkout keeps its Git folder in .git, got %q", got)
	}
	os.WriteFile(filepath.Join(top, ".git"), []byte("gitdir: /src/iam/.git/worktrees/feat\n"), 0o644)
	if got := gitDir(top); got != "/src/iam/.git/worktrees/feat" {
		t.Errorf("gitDir = %q", got)
	}
	os.WriteFile(filepath.Join(top, ".git"), []byte("gitdir: ../iam/.git/worktrees/feat\n"), 0o644)
	if got, want := gitDir(top), filepath.Join(filepath.Dir(top), "iam", ".git", "worktrees", "feat"); got != want {
		t.Errorf("a relative gitdir = %q, want %q", got, want)
	}
}
```

Add to `tree/internal/source/checkout_test.go`:

```go
func TestShellWhere(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "src", "iam")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	os.MkdirAll(filepath.Join(repo, "env", "prod"), 0o755)
	wt := filepath.Join(repo, "_worktrees", "feat")
	os.MkdirAll(wt, 0o755)
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git", "worktrees", "feat")+"\n"), 0o644)
	for dir, want := range map[string]string{
		repo:                                "iam",
		filepath.Join(repo, "env", "prod"): "iam/env/prod",
		wt:                                  "iam",
		filepath.Join(home, "notes"):       "~/notes",
		"":                                  "",
	} {
		if got := shellWhere(dir, home); got != want {
			t.Errorf("shellWhere(%q) = %q, want %q", dir, got, want)
		}
	}
}
```

If `checkout_test.go` does not import `os`, add it.

Add to `tree/internal/source/herdr_test.go` (add `"reflect"` to its imports):

```go
func TestSnapshotListsPanesWithNoAgentAsShells(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "iam")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	deep := filepath.Join(repo, "terraform", "prod")
	os.MkdirAll(deep, 0o755)
	s := serve(t, map[string]string{
		"workspace.list": `{"id":"tree","result":{"workspaces":[]}}`,
		"tab.list":       `{"id":"tree","result":{"tabs":[]}}`,
		"agent.list": `{"id":"tree","result":{"agents":[
			{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1","agent":"claude"}]}}`,
		"pane.list": `{"id":"tree","result":{"panes":[
			{"pane_id":"w1:p1","tab_id":"w1:t1","workspace_id":"w1"},
			{"pane_id":"w1:p2","tab_id":"w1:t1","workspace_id":"w1","label":" shepherd ","cwd":"/elsewhere"},
			{"pane_id":"w1:p3","tab_id":"w1:t1","workspace_id":"w1","cwd":"/elsewhere","foreground_cwd":"` + deep + `"}]}}`,
	})
	got, err := Snapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Shell{
		{PaneID: "w1:p2", TabID: "w1:t1", WorkspaceID: "w1", Label: "shepherd", Where: "/elsewhere"},
		{PaneID: "w1:p3", TabID: "w1:t1", WorkspaceID: "w1", Where: "iam/terraform/prod"},
	}
	if !reflect.DeepEqual(got.Shells, want) {
		t.Fatalf("shells = %+v, want %+v", got.Shells, want)
	}
}
```

Create `tree/internal/source/ledger_test.go`:

```go
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
	if got := LedgerDir(); got != "/state/agent-dispatch/runs" {
		t.Fatalf("LedgerDir = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd tree && go test ./internal/source/`
Expected: build failure: `undefined: upstream`, `undefined: base`, `undefined: gitDir`, `undefined: shellWhere`, `undefined: Settled`, `too many arguments in call to gitState`.

- [ ] **Step 3: Rewrite git.go**

Replace all of `tree/internal/source/git.go` with:

```go
package source

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// countTTL is how long the counts of a checkout stand before Git is asked again.
const countTTL = 10 * time.Second

// What the commits of a checkout are counted against: its upstream branch for a workspace, and
// the default branch of origin for an agent, which is what a dispatched branch starts from.
const (
	upstream = "@{upstream}"
	base     = "origin/HEAD"
)

var (
	now   = time.Now
	count = revCount

	countMu sync.Mutex
	counts  = map[string]counted{}
)

type counted struct {
	branch        string
	ahead, behind int
	at            time.Time
}

// gitState is the branch a checkout has out and how far HEAD is ahead of and behind against.
// main is the main checkout of the repository, and top is the root of a linked worktree.
func gitState(main, top string, linked bool, against string) model.Git {
	if main == "" {
		return model.Git{}
	}
	dir := main
	if linked && top != "" {
		dir = top
	}
	g := model.Git{Repo: filepath.Base(main), Branch: branch(gitDir(dir))}
	if g.Branch == "" {
		return g
	}
	key := dir + "\x00" + against
	countMu.Lock()
	defer countMu.Unlock()
	c, ok := counts[key]
	if !ok || c.branch != g.Branch || now().Sub(c.at) >= countTTL {
		c = counted{branch: g.Branch, at: now()}
		c.ahead, c.behind = count(dir, against)
		counts[key] = c
	}
	g.Ahead, g.Behind = c.ahead, c.behind
	return g
}

// gitDir is the folder that holds the HEAD of a checkout: .git of a main checkout, or the folder
// that the .git file of a linked worktree names.
func gitDir(top string) string {
	p := filepath.Join(top, ".git")
	info, err := os.Lstat(p)
	if err != nil || info.IsDir() {
		return p
	}
	text, _ := os.ReadFile(p)
	d := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(text)), "gitdir:"))
	if !filepath.IsAbs(d) {
		d = filepath.Join(top, d)
	}
	return d
}

// branch is the branch named by the HEAD in a Git folder, or the short commit id of a detached HEAD.
func branch(gitdir string) string {
	text, _ := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	head := strings.TrimSpace(string(text))
	if b, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return b
	}
	if len(head) > 7 {
		return head[:7]
	}
	return ""
}

// revCount asks Git how many commits HEAD is ahead of and behind against, as of the last fetch.
// Both are 0 when against does not resolve.
func revCount(dir, against string) (ahead, behind int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-list", "--left-right", "--count", against+"...HEAD")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	fmt.Sscan(string(out), &behind, &ahead)
	return ahead, behind
}
```

- [ ] **Step 4: Add shellWhere**

Append to `tree/internal/source/checkout.go`:

```go
// shellWhere says where a shell is: "<repo>/<path in the repo>" inside a repository, else the
// path with the home directory as ~.
func shellWhere(dir, home string) string {
	main, top, _ := checkout(dir)
	if main == "" {
		return where("", dir, false, home)
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return where("", dir, false, home)
	}
	return filepath.Join(filepath.Base(main), rel)
}
```

- [ ] **Step 5: Add the ledger reader**

Create `tree/internal/source/ledger.go`:

```go
package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// A ledger file as last read, kept until its time or size changes.
type ledgerFile struct {
	mod     time.Time
	size    int64
	entries []ledgerEntry
}

// ledgerEntry is the part of a dispatch attempt that says whether its supervisor took its result.
type ledgerEntry struct {
	Pane    string `json:"pane_id"`
	Status  string `json:"status"`
	Started string `json:"started"`
}

var (
	ledgerMu sync.Mutex
	ledgers  = map[string]ledgerFile{}
)

// LedgerDir is the folder that holds one folder for each dispatch run.
func LedgerDir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "agent-dispatch", "runs")
}

// Settled reads the ledger of each run in dir and returns the panes whose latest dispatch is
// settled: the supervisor took the worker's worker_done. A ledger that does not parse counts as empty.
func Settled(dir string) map[string]bool {
	paths, _ := filepath.Glob(filepath.Join(dir, "*", "ledger.json"))
	latest := map[string]ledgerEntry{}
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		f, ok := ledgers[p]
		if !ok || !f.mod.Equal(info.ModTime()) || f.size != info.Size() {
			f = ledgerFile{mod: info.ModTime(), size: info.Size()}
			if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &f.entries) != nil {
				f.entries = nil
			}
			ledgers[p] = f
		}
		for _, e := range f.entries {
			if e.Pane == "" {
				continue
			}
			if old, ok := latest[e.Pane]; !ok || e.Started >= old.Started {
				latest[e.Pane] = e
			}
		}
	}
	out := map[string]bool{}
	for pane, e := range latest {
		if e.Status == "settled" {
			out[pane] = true
		}
	}
	return out
}
```

- [ ] **Step 6: Read shells and the new Git state in Snapshot**

In `tree/internal/source/herdr.go`, function `Snapshot`:

Replace the `panes` struct with:

```go
	var panes struct {
		Panes []struct {
			Pane       string             `json:"pane_id"`
			Tab        string             `json:"tab_id"`
			Workspace  string             `json:"workspace_id"`
			Label      string             `json:"label"`
			Focused    bool               `json:"focused"`
			Cwd        string             `json:"cwd"`
			Foreground string             `json:"foreground_cwd"`
			Tokens     map[string]*string `json:"tokens"`
		} `json:"panes"`
	}
```

Replace the workspace Git lines inside `for _, w := range ws.Workspaces`:

```go
			if m.Git = gitState(w.Worktree.Root, w.Worktree.Checkout, w.Worktree.Linked, upstream); m.Git.Branch != "" {
				m.Git.Repo = m.Repo
			}
		} else if main, top, linked := checkout(cwd[first[w.ID]]); main != "" {
			m.Repo = filepath.Base(main)
			m.Where, m.Worktree = where(main, top, linked, home), linked
			m.Git = gitState(main, top, linked, upstream)
		}
```

In the agent loop, replace `Git: gitState(main, linked)` with `Git: gitState(main, top, linked, base)`.

After the agent loop and before `return s, nil`, add:

```go
	isAgent := map[string]bool{}
	for _, a := range agents.Agents {
		isAgent[a.Pane] = true
	}
	for _, p := range panes.Panes {
		if isAgent[p.Pane] {
			continue
		}
		dir := p.Foreground
		if dir == "" {
			dir = p.Cwd
		}
		s.Shells = append(s.Shells, model.Shell{
			PaneID: p.Pane, TabID: p.Tab, WorkspaceID: p.Workspace,
			Label: strings.TrimSpace(p.Label), Where: shellWhere(dir, home),
		})
	}
```

Change the comment above `Snapshot` to `// Snapshot reads the workspaces, tabs, agent panes and shells, and marks the panes that set tree=hide.`

- [ ] **Step 7: Run the source tests**

Run: `cd tree && go test ./internal/source/`
Expected: `ok`. The Git test needs `git` on PATH; it skips without it.

- [ ] **Step 8: Run all tests for compile errors**

Run: `cd tree && go vet ./...`
Expected: clean.

- [ ] **Step 9: Commit**

```bash
cd ~/Projects/personal/herdr-plugins
git add tree/internal/source
git commit -F - <<'EOF'
tree: read shells, worktree branches and the dispatch ledgers

A pane with no agent becomes a shell with its label or where it is. An
agent's branch is read in a linked worktree too and counted against
origin/HEAD. Settled reads the dispatch ledgers for the workers whose
result their supervisor took.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: View draws efforts

All files in `internal/view` change together: the tests share fixtures, so the package compiles only when every file has moved over.

**Files:**
- Modify: `tree/internal/view/rows.go` (rewrite)
- Modify: `tree/internal/view/render.go` (rewrite)
- Modify: `tree/internal/view/theme.go`
- Modify: `tree/internal/view/folds.go:25-37`
- Modify: `tree/internal/view/program.go` (edits listed in Step 7)
- Test: `tree/internal/view/rows_test.go` (rewrite), `tree/internal/view/render_test.go` (rewrite), `tree/internal/view/theme_test.go`, `tree/internal/view/folds_test.go`, `tree/internal/view/program_test.go` (edits listed in Step 1)

**Interfaces:**
- Consumes: `model.Tree.Roots` efforts, `model.KindShell`, `Node.Worker`, `Node.Reported`, `model.IsProject`, `model.PRNumber` (Task 1).
- Produces:
  - `Rows(t model.Tree, st State) []Row`, `State{Folded map[string]bool}` (no `Attention`), `const RollupShell = "shell"`
  - `Render(rows []Row, width, selected int, th Theme) []string`
  - `Theme.ShellGlyph` (default `"$"`, config key `[glyphs] shell`). `Theme.TabGlyph` and `Theme.PaneGlyph` are removed.
  - `SaveFolds` keeps folds of efforts only.

- [ ] **Step 1: Write the failing tests**

Replace all of `tree/internal/view/rows_test.go` with:

```go
package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func effort(id string, kids ...*model.Node) *model.Node {
	return &model.Node{ID: id, Label: id, Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w-" + id, Children: kids}
}

func agent(id, status string, kids ...*model.Node) *model.Node {
	return &model.Node{ID: id, Label: id, Status: status, Focus: model.KindPane, Shown: model.KindPane, TabID: id + ":t", Children: kids}
}

func worker(id, status string, kids ...*model.Node) *model.Node {
	n := agent(id, status, kids...)
	n.Worker = true
	return n
}

func shell(id string) *model.Node {
	return &model.Node{ID: id, Label: id, Status: model.Unknown, Focus: model.KindPane, Shown: model.KindShell}
}

func sample() model.Tree {
	return model.Tree{Roots: []*model.Node{
		effort("lead", agent("a", model.Done), agent("b", model.Idle, worker("b1", model.Blocked))),
		effort("solo"),
		effort("repo1", shell("sh1")),
	}}
}

func show(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		mark := " "
		if r.Folded {
			mark = "+"
		}
		fmt.Fprintf(&b, "%s%s%s %v\n", strings.Repeat("  ", r.Depth), mark, r.Node.Label, r.Rollup)
	}
	return b.String()
}

func expect(t *testing.T, rows []Row, want string) {
	t.Helper()
	if got := show(rows); got != strings.TrimLeft(want, "\n") {
		t.Fatalf("rows:\n%s\nwant:\n%s", got, want)
	}
}

func TestRowsDefault(t *testing.T) {
	expect(t, Rows(sample(), State{}), `
 lead []
   a []
   b []
     b1 []
 solo []
 repo1 []
   sh1 []
`)
}

func TestRowsFoldAndRollup(t *testing.T) {
	st := State{Folded: map[string]bool{"lead": true, "repo1": true}}
	expect(t, Rows(sample(), st), `
+lead [done idle blocked]
 solo []
+repo1 [shell]
`)
}

func TestRowsALeafEffortDoesNotFold(t *testing.T) {
	expect(t, Rows(model.Tree{Roots: []*model.Node{effort("solo")}}, State{Folded: map[string]bool{"solo": true}}), `
 solo []
`)
}

func TestRowsBranchLinesOfWorkers(t *testing.T) {
	tree := model.Tree{Roots: []*model.Node{
		effort("e", agent("L", model.Idle,
			worker("w1", model.Idle, shell("s1"), worker("w1a", model.Idle)),
			worker("w2", model.Idle))),
	}}
	got := map[string]Row{}
	for _, r := range Rows(tree, State{}) {
		got[r.Node.ID] = r
	}
	for _, c := range []struct {
		id    string
		depth int
		last  bool
		trunk []bool
	}{
		{"L", 1, false, nil},
		{"w1", 2, false, nil},
		{"s1", 3, false, []bool{true}},
		{"w1a", 3, true, []bool{true}},
		{"w2", 2, true, nil},
	} {
		r := got[c.id]
		if r.Depth != c.depth || r.Last != c.last || fmt.Sprint(r.Trunk) != fmt.Sprint(c.trunk) {
			t.Errorf("%s: depth=%d last=%v trunk=%v, want depth=%d last=%v trunk=%v",
				c.id, r.Depth, r.Last, r.Trunk, c.depth, c.last, c.trunk)
		}
	}
}

func TestRowsRepoLineOnlyUnderEffortsAndWorkers(t *testing.T) {
	a := agent("a", model.Idle, worker("w", model.Idle))
	a.Repo, a.Children[0].Repo = "ops", "iam"
	x := effort("x", a)
	x.Repo = "flosports"
	tree := model.Tree{Roots: []*model.Node{x, effort("y")}}
	for _, r := range Rows(tree, State{}) {
		want := map[string]string{"x": "flosports", "a": "", "w": "iam", "y": ""}[r.Node.ID]
		if r.Repo != want {
			t.Errorf("%s: repo %q, want %q", r.Node.ID, r.Repo, want)
		}
		if gap := r.Node.ID == "y"; r.Gap != gap {
			t.Errorf("%s: gap %v, want %v", r.Node.ID, r.Gap, gap)
		}
	}
}
```

Replace all of `tree/internal/view/render_test.go` with:

```go
package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func plain() Theme {
	th := DefaultTheme()
	th.Plain, th.ShellGlyph, th.WorktreeGlyph = true, "$", "⎇"
	return th
}

func sp(n int) string { return strings.Repeat(" ", n) }

func same(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func gke() model.Tree {
	job := &model.Node{ID: "w2:p1", Label: "SRE-194 job scheduler", Status: model.Idle, Shown: model.KindPane, Worker: true,
		Repo: "job-scheduler", Worktree: true, Git: model.Git{Repo: "job-scheduler", Branch: "sre-194"}, Tag: "SRE-194",
		Children: []*model.Node{{ID: "w2:p2", Label: "shepherd", Shown: model.KindShell}}}
	lead := &model.Node{ID: "w1:p1", Label: "HANDOFF.md resume", Status: model.Idle, Shown: model.KindPane,
		Children: []*model.Node{job}}
	e := &model.Node{ID: "w1", Label: "GKE Migration", Shown: model.KindWorkspace, Repo: "flosports",
		Git: model.Git{Repo: "flosports", Branch: "main"}, Link: "https://linear.app/flosports/project/gke-f425",
		Children: []*model.Node{{ID: "w1:p2", Label: "Status", Shown: model.KindShell}, lead}}
	return model.Tree{Roots: []*model.Node{e}}
}

func TestRenderAnEffort(t *testing.T) {
	same(t, Render(Rows(gke(), State{}), 44, -1, plain()), []string{
		" ▾ GKE Migration" + sp(22) + "proj ↗",
		sp(5) + "flosports@main",
		sp(5) + "$ Status",
		sp(5) + "o HANDOFF.md resume",
		sp(5) + "└ o SRE-194 job scheduler" + sp(7) + "SRE-194",
		sp(9) + "⎇ job-scheduler@sre-194",
		sp(9) + "$ shepherd",
	})
}

func TestRenderBranchLinesRunPastAWorkerToTheNextOne(t *testing.T) {
	w := func(id string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Shown: model.KindPane, Worker: true, Repo: "iam", Children: kids}
	}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Working, Shown: model.KindPane, Children: []*model.Node{
		w("a", &model.Node{ID: "w2:p2", Label: "reviewr", Shown: model.KindShell}),
		w("b"),
	}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, -1, plain()), []string{
		" ▾ Main",
		"     * lead",
		"     ├ o a",
		"     │   iam",
		"     │   $ reviewr",
		"     └ o b",
		sp(9) + "iam",
	})
}

func TestRenderFoldedEffortShowsTheDotsOfItsAgents(t *testing.T) {
	core := &model.Node{ID: "w3", Label: "Core", Shown: model.KindWorkspace, Tag: "SRE-923",
		Link: "https://linear.app/flosports/issue/SRE-923",
		Children: []*model.Node{
			{ID: "w3:p2", Label: "shepherd", Shown: model.KindShell},
			{ID: "w3:p1", Label: "lead", Status: model.Done, Shown: model.KindPane, Children: []*model.Node{
				{ID: "w4:p1", Label: "sweep", Status: model.Working, Shown: model.KindPane, Worker: true}}},
		}}
	notes := &model.Node{ID: "w5", Label: "notes", Shown: model.KindWorkspace,
		Children: []*model.Node{{ID: "w5:p1", Label: "~/notes", Shown: model.KindShell}}}
	st := State{Folded: map[string]bool{"w3": true, "w5": true}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{core, notes}}, st), 40, -1, plain()), []string{
		" ▸ Core  + *" + sp(19) + "SRE-923 ↗",
		"",
		" ▸ notes  $",
	})
}

func TestRenderWorkerTagAndRepoLine(t *testing.T) {
	review := &model.Node{ID: "w2:p1", Label: "Review infra base services", Status: model.Done, Shown: model.KindPane,
		Worker: true, Repo: "infra-base-services", Reported: true,
		Git:  model.Git{Repo: "infra-base-services", Branch: "main", Ahead: 2, Behind: 1},
		Link: "https://github.com/flocasts/infra-base-services/pull/1079"}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Idle, Shown: model.KindPane, Children: []*model.Node{review}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, -1, plain()), []string{
		" ▾ Main",
		"     o lead",
		"     └ + Review infra base services  #1079",
		sp(9) + "infra-base-services@main -1 +2  reported",
	})
}

func TestRenderNarrowCutsTheTitleAndTheBranchAndKeepsTheTag(t *testing.T) {
	review := &model.Node{ID: "w2:p1", Label: "Review service level objectives", Status: model.Idle, Shown: model.KindPane,
		Worker: true, Repo: "iam", Worktree: true,
		Git:  model.Git{Repo: "iam", Branch: "sre-1006-aws-account-alternate-contacts"},
		Link: "https://github.com/flocasts/iam/pull/57"}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Idle, Shown: model.KindPane, Children: []*model.Node{review}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	got := Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 31, -1, plain())
	same(t, got[2:], []string{
		"     └ o Review service l…  #57",
		sp(9) + "⎇ iam@sre-1006-aws-ac…",
	})
}

func TestRenderVeryNarrowPaneStaysInside(t *testing.T) {
	for _, width := range []int{1, 6, 12} {
		for _, line := range Render(Rows(gke(), State{}), width, 0, plain()) {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %q is %d cells", width, line, w)
			}
		}
	}
}

func TestRenderCursorHeldPromptsAndStall(t *testing.T) {
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Working, Stalled: true, Pending: 2, Shown: model.KindPane}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, 1, plain()), []string{
		" ▾ Main",
		">    ~ lead +2",
	})
}

func TestRenderAStalledDotOnlyForAWorkingAgent(t *testing.T) {
	done := &model.Node{ID: "w1:p1", Label: "done", Status: model.Done, Stalled: true, Shown: model.KindPane}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{done}}
	got := Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, -1, plain())
	same(t, got[1:], []string{"     + done"})
}

func TestEffortAndWorkerTags(t *testing.T) {
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{Link: "https://linear.app/flosports/project/gke-f425", Tag: "SRE-1"}, "proj ↗"},
		{model.Node{Tag: "SRE-923", Link: "https://linear.app/flosports/issue/SRE-923"}, "SRE-923 ↗"},
		{model.Node{Tag: "SRE-923"}, "SRE-923"},
		{model.Node{Link: "https://example.com/x"}, "↗"},
		{model.Node{}, ""},
	} {
		if got := effortTag(&c.n); got != c.want {
			t.Errorf("effortTag(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{Tag: "SRE-194", Link: "https://github.com/flocasts/iam/pull/5"}, "SRE-194"},
		{model.Node{Link: "https://github.com/flocasts/iam/pull/5"}, "#5"},
		{model.Node{Link: "https://linear.app/flosports/issue/SRE-1"}, ""},
	} {
		if got := workerTag(&c.n); got != c.want {
			t.Errorf("workerTag(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
}
```

Before you replace `render_test.go`, copy its current `TestCutLeftKeepsTheEnd` and add it back unchanged at the end of the new file. `cutLeft` does not change.

Replace the body of `TestLoadTheme` in `tree/internal/view/theme_test.go` with:

```go
	def := DefaultTheme()
	if def.ShellGlyph != "$" {
		t.Fatalf("default shell glyph = %q", def.ShellGlyph)
	}
	if got := LoadTheme(filepath.Join(t.TempDir(), "absent")); got.ShellGlyph != def.ShellGlyph || got.Colors[model.Blocked] != def.Colors[model.Blocked] {
		t.Fatalf("no config must give the default theme, got %+v", got)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`
[glyphs]
shell = ">"
[colors]
blocked = "#ff0000"
`), 0o644)
	got := LoadTheme(dir)
	if got.ShellGlyph != ">" || got.WorktreeGlyph != def.WorktreeGlyph {
		t.Errorf("glyphs = %q %q", got.ShellGlyph, got.WorktreeGlyph)
	}
	if got.Colors[model.Blocked] != "#ff0000" || got.Colors[model.Done] != def.Colors[model.Done] {
		t.Errorf("colors = %v", got.Colors)
	}
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`not = [toml`), 0o644)
	if got := LoadTheme(dir); got.ShellGlyph != def.ShellGlyph {
		t.Errorf("a config that does not parse must give the default theme, got %+v", got)
	}
```

Replace the tree and maps in `TestFoldsRoundTrip` (`tree/internal/view/folds_test.go`) with:

```go
	tree := model.Tree{Roots: []*model.Node{{ID: "w1", Children: []*model.Node{{ID: "w1:p1"}}}, {ID: "w2"}}}
	folds := map[string]bool{"w1": true, "w2": false, "w9": true, "w1:p1": true}
	if err := SaveFolds(dir, folds, tree); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"w1": true, "w2": false}
```

and change the failure text to `"folds = %v, want %v (only efforts that are still there keep a fold)"`.

In `tree/internal/view/program_test.go`, make these changes. `sample()`, `effort`, `agent`, `worker` and `shell` come from `rows_test.go`. The rows of `sample()` are `lead, a, b, b1, solo, repo1, sh1`; only `a`, `b` and `b1` take a step.

Replace `TestProgramFoldSavesAndGroupsUnfoldOnEnter` with:

```go
func TestProgramFoldSavesAndEnterFocusesAnEffort(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("space"))
	if len(p.rows) != 4 || !p.rows[0].Folded {
		t.Fatalf("space folds the selected effort, rows:\n%s", show(p.rows))
	}
	if !f.saved["lead"] {
		t.Fatalf("the fold is saved, got %v", f.saved)
	}
	p = send(t, p, key("down"))
	before := len(p.rows)
	p = send(t, p, key("space"))
	if selected(p) != "solo" || len(p.rows) != before {
		t.Fatalf("space on an effort with no panes does nothing, rows:\n%s", show(p.rows))
	}
	_, cmd := p.Update(key("enter"))
	cmd()
	if len(f.focused) != 1 || f.focused[0] != "solo" {
		t.Fatalf("enter on an effort focuses it, focused %v", f.focused)
	}
}
```

Replace `TestProgramAttentionAndQuit` with:

```go
func TestProgramQuit(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	if next, _ := p.Update(key("a")); len(next.(Program).rows) != 7 {
		t.Fatal("a is no longer a key")
	}
	for _, k := range []string{"q", "esc"} {
		if _, cmd := p.Update(key(k)); cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%s must quit", k)
		}
	}
}
```

In `TestProgramKeepsSelectionAcrossReload`, replace `loadedMsg{tree: model.Tree{Roots: []*model.Node{node("lead", model.Idle)}}}` with `loadedMsg{tree: model.Tree{Roots: []*model.Node{effort("lead")}}}`, and replace `"no agents"` with `"no workspaces"`.

In `TestProgramScrolls`, replace `roots = append(roots, node(id, model.Idle))` with `roots = append(roots, effort(id))`.

In `TestSidebarPlacesTheCursorWhenTheFocusedPaneGetsARow`, replace `node("solo", model.Idle)` with `effort("solo")` (both places) and `node("late", model.Working)` with `effort("late")`.

Replace `repos()`, `TestSidebarClickOnARepoLineSelectsItsRow`, `TestSidebarScrollKeepsBothLinesOfTheCursorRow` and `TestSidebarRightClickAsksHerdrForTheMenuOfTheRow` with:

```go
func repos() model.Tree {
	n := func(id, repo string) *model.Node {
		e := effort(id)
		e.Repo = repo
		return e
	}
	return model.Tree{Roots: []*model.Node{n("a", "iam"), n("b", "ops"), n("c", "api")}}
}

func TestSidebarClickOnARepoLineSelectsItsRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	// Lines: a, iam, gap, b, ops, gap, c, api. A gap belongs to the row below it.
	for y, want := range []string{"a", "a", "b", "b", "b", "c", "c", "c"} {
		next, _ := p.Update(click(12, y))
		if got := selected(next.(Program)); got != want {
			t.Errorf("line %d selects %q, want %q", y, got, want)
		}
	}
}

func TestSidebarScrollKeepsBothLinesOfTheCursorRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 4})
	p = send(t, p, key("down"), key("down"))
	v := p.View()
	if !strings.Contains(v, "   c") || !strings.Contains(v, "api") {
		t.Fatalf("the name and repo of the cursor row are both in view:\n%s", v)
	}
	if n := strings.Count(v, "\n") + 1; n > 4 {
		t.Fatalf("the view is %d lines in a pane of 4", n)
	}
}

func TestSidebarRightClickAsksHerdrForTheMenuOfTheRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	next, cmd := p.Update(rightClick(12, 4))
	if cmd == nil {
		t.Fatal("a right-click on a row asks for its menu")
	}
	if got := fmt.Sprint(cmd()); got != "herdr-menu;workspace;w-b;1;;" {
		t.Fatalf("request = %q", got)
	}
	if len(f.focused) != 0 {
		t.Fatal("a right-click must not jump")
	}
	_, cmd = next.(Program).Update(rightClick(12, 4))
	if got := fmt.Sprint(cmd()); got != "herdr-menu;workspace;w-b;2;;" {
		t.Fatalf("second request = %q", got)
	}
}
```

Replace the table in `TestMenuTargetFollowsWhatTheRowShows` with:

```go
		{model.Node{ID: "w1", Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w1"}, "workspace;w1"},
		{model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w1"}, "pane;w1:p1"},
		{model.Node{ID: "w1:p2", Focus: model.KindPane, Shown: model.KindShell, WorkspaceID: "w1"}, "pane;w1:p2"},
```

Replace `places()` with:

```go
func places() model.Tree {
	e := effort("main", agent("a", model.Idle), agent("b", model.Idle))
	e.Where = "~/src/flo"
	e.Children[0].Where, e.Children[1].Where = "iam/_worktrees/a", "ops/_worktrees/b"
	return model.Tree{Roots: []*model.Node{e}}
}
```

Replace `TestMenuRequestNamesTheAgentPaneOnlyForARowThatIsOneAgent` with:

```go
func TestMenuRequestNamesThePaneOnlyForAnAgentRow(t *testing.T) {
	a := &model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w1", Tag: "SRE-923"}
	if got := menuTitle(a, 3); got != "herdr-menu;pane;w1:p1;3;w1:p1;SRE-923" {
		t.Errorf("agent row: %q", got)
	}
	s := &model.Node{ID: "w1:p2", Focus: model.KindPane, Shown: model.KindShell, WorkspaceID: "w1"}
	if got := menuTitle(s, 4); got != "herdr-menu;pane;w1:p2;4;;" {
		t.Errorf("shell row: %q", got)
	}
	e := &model.Node{ID: "w1", Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w1"}
	if got := menuTitle(e, 5); got != "herdr-menu;workspace;w1;5;;" {
		t.Errorf("effort row: %q", got)
	}
}
```

Change the expectations in the step tests:

- `TestSidebarStepJumpsOneRowFromTheFocusedPane`: after `run(t, p, stepUp, stepUp)`, expect `selected(p) == "b1"` and `fmt.Sprint(s.focused) == "[b a b1]"` (up from `a` skips `lead`, wraps, and skips `sh1`, `repo1`, `solo`).
- Replace `TestSidebarStepSkipsGroupRowsAndWraps` with:

```go
func TestSidebarStepSkipsEffortAndShellRowsAndWraps(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "b1"}
	p := run(t, s.start(t), stepDown)
	if selected(p) != "a" {
		t.Fatalf("selected %q, want a past solo, repo1, sh1, the end and lead", selected(p))
	}
	p = run(t, p, stepUp)
	if selected(p) != "b1" || fmt.Sprint(s.focused) != "[a b1]" {
		t.Fatalf("selected %q, focused %v", selected(p), s.focused)
	}
}
```

- Replace `TestSidebarStepSkipsTheRowsOfAFoldedParent` with:

```go
func TestSidebarStepSkipsTheRowsOfAFoldedEffort(t *testing.T) {
	tree := model.Tree{Roots: []*model.Node{
		effort("x", agent("x1", model.Idle)), effort("y", agent("y1", model.Idle)), effort("z", agent("z1", model.Idle)),
	}}
	s := &stepper{fake: &fake{tree: tree}, at: "x1"}
	p := send(t, s.start(t), key("down"), key("down"), key("space"))
	if !p.rows[2].Folded {
		t.Fatalf("y is folded, rows:\n%s", show(p.rows))
	}
	p = run(t, p, stepDown)
	if selected(p) != "z1" || fmt.Sprint(s.focused) != "[z1]" {
		t.Fatalf("selected %q, focused %v, want z1", selected(p), s.focused)
	}
}
```

- `TestSidebarStepsThatArriveTogetherEachMoveOneRow`: the last check becomes `if selected(p) != "a"` with the message `"selected %q, want a: the next step is not held back"`.
- Replace `TestSidebarStepStaysOnARowThatPutsTheFocusOnAnotherRow` with:

```go
func TestSidebarStepStaysOnARowThatPutsTheFocusOnAnotherRow(t *testing.T) {
	// A jump to b1 leaves the focus on b, which has a row of its own.
	s := &stepper{fake: &fake{tree: sample()}, at: "a", lands: map[string]string{"b1": "b"}}
	p := run(t, s.start(t), stepUp)
	p = send(t, p, loadedMsg{tree: s.tree, current: "b"})
	if selected(p) != "b1" {
		t.Fatalf("selected %q, want b1: a reload keeps the row of the step", selected(p))
	}
	p = run(t, p, stepUp)
	if selected(p) != "b" {
		t.Fatalf("selected %q, want b: the step goes on from b1", selected(p))
	}
}
```

Leave the other program tests as they are; they hold with `sample()` above.

- [ ] **Step 2: Run the view tests to see them fail**

Run: `cd tree && go test ./internal/view/`
Expected: build failure, for example `undefined: RollupShell`, `th.ShellGlyph undefined`, `undefined: effortTag`, `unknown field Attention`.

- [ ] **Step 3: Rewrite rows.go**

Replace all of `tree/internal/view/rows.go` with:

```go
// Package view turns the efforts into rows, draws them, and runs the interactive pane.
package view

import "github.com/mago0/herdr-plugins/tree/internal/model"

// Row is one visible row: one line, and a second line under an effort or a worker with a repo.
type Row struct {
	Node  *model.Node
	Depth int
	// HasChildren and Folded are those of an effort. Only an effort folds.
	HasChildren bool
	Folded      bool
	// Last and Trunk place the branch lines of workers: Last is true for the last worker under the
	// same row, and Trunk says for each worker above this row whether its line runs on past it.
	Last  bool
	Trunk []bool
	// Rollup is the dot states of the agents and workers of a folded effort.
	Rollup []string
	// Repo is the repository on the second line of an effort or a worker, or "".
	Repo string
	// Gap is true for each effort after the first, which has a blank line above it.
	Gap bool
}

// Height is the count of lines the row takes on screen.
func (r Row) Height() int {
	h := 1
	if r.Repo != "" {
		h++
	}
	if r.Gap {
		h++
	}
	return h
}

// State is the folds the user has set, by effort id.
type State struct {
	Folded map[string]bool
}

// RollupShell stands in the rollup of a folded effort that holds shells and no agent.
const RollupShell = "shell"

// agentStates is the states of the agents and workers among nodes and below them.
func agentStates(nodes []*model.Node) []string {
	var out []string
	for _, n := range nodes {
		if n.Shown == model.KindPane {
			out = append(out, n.Status)
		}
		out = append(out, agentStates(n.Children)...)
	}
	return out
}

func ofKind(nodes []*model.Node, k model.Kind) []*model.Node {
	var out []*model.Node
	for _, n := range nodes {
		if n.Shown == k {
			out = append(out, n)
		}
	}
	return out
}

// Rows flattens the efforts to the rows on screen.
func Rows(t model.Tree, st State) []Row {
	var out []Row
	var worker func(n *model.Node, depth int, trunk []bool, last bool)
	worker = func(n *model.Node, depth int, trunk []bool, last bool) {
		out = append(out, Row{Node: n, Depth: depth, Last: last, Trunk: trunk, Repo: n.Repo})
		below := append(append([]bool(nil), trunk...), !last)
		for _, s := range ofKind(n.Children, model.KindShell) {
			out = append(out, Row{Node: s, Depth: depth + 1, Trunk: below})
		}
		ws := ofKind(n.Children, model.KindPane)
		for i, w := range ws {
			worker(w, depth+1, below, i == len(ws)-1)
		}
	}
	for i, e := range t.Roots {
		r := Row{Node: e, HasChildren: len(e.Children) > 0, Repo: e.Repo, Gap: i > 0}
		r.Folded = r.HasChildren && st.Folded[e.ID]
		if r.Folded {
			if r.Rollup = agentStates(e.Children); len(r.Rollup) == 0 {
				r.Rollup = []string{RollupShell}
			}
		}
		out = append(out, r)
		if r.Folded {
			continue
		}
		for _, c := range e.Children {
			out = append(out, Row{Node: c, Depth: 1})
			ws := ofKind(c.Children, model.KindPane)
			for j, w := range ws {
				worker(w, 2, nil, j == len(ws)-1)
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Rewrite render.go**

Replace all of `tree/internal/view/render.go` with:

```go
package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// minLabel is the least count of cells a label, repo or branch keeps in a narrow pane.
const minLabel = 4

// maxTag is the most cells a label takes at the right edge.
const maxTag = 12

var (
	bold      = lipgloss.NewStyle().Bold(true)
	faint     = lipgloss.NewStyle().Faint(true)
	plainText = lipgloss.NewStyle()
)

// cut shortens s to n cells, ending in an ellipsis when it was cut.
func cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// cutLeft shortens s to n cells and keeps its end, with an ellipsis in front when it was cut.
func cutLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[1:]
	}
	return "…" + string(r)
}

// Render draws the rows: one line for each row, and a second line under an effort or a worker
// that has a repo. selected is the index of the row under the cursor, or -1.
func Render(rows []Row, width, selected int, th Theme) []string {
	var lines []string
	add := func(line string) {
		line = strings.TrimRight(line, " ")
		if width > 0 {
			line = strings.TrimRight(lipgloss.NewStyle().MaxWidth(width).Render(line), " ")
		}
		lines = append(lines, line)
	}
	for i, r := range rows {
		n := r.Node
		if r.Gap {
			lines = append(lines, "")
		}
		mark := " "
		switch {
		case i != selected:
		case th.Plain:
			mark = ">"
		default:
			mark = bold.Render("▌")
		}
		head, after, right, style := mark+th.paint(lead(r), faint), "", "", plainText
		switch {
		case r.Depth == 0:
			style, right = bold, effortTag(n)
			if r.Folded {
				after = "  " + rollup(r.Rollup, th)
			}
		case n.Shown == model.KindShell:
			head += th.paint(th.ShellGlyph+" ", faint)
			style = faint
		default:
			head += th.dot(state(n)) + " "
			if n.Pending > 0 {
				after = " " + th.paint(th.pending(n.Pending), faint)
			}
			if n.Worker {
				right = workerTag(n)
			}
		}
		used := lipgloss.Width(head) + lipgloss.Width(after)
		tail := 0
		if right != "" {
			tail = 2 + lipgloss.Width(right)
		}
		label := n.Label
		if width > 0 {
			label = cut(label, max(minLabel, width-used-tail))
		}
		line := head + th.paint(label, style) + after
		if right != "" {
			pad := 2
			if width > 0 {
				pad = max(2, width-used-lipgloss.Width(label)-lipgloss.Width(right))
			}
			line += strings.Repeat(" ", pad) + th.paint(right, tagStyle(n))
		}
		add(line)
		if r.Repo != "" {
			under, note := below(r), ""
			if n.Worker && n.Reported {
				note = "  reported"
			}
			room := 0
			if width > 0 {
				room = max(1, width-1-lipgloss.Width(under)-lipgloss.Width(note))
			}
			add(mark + th.paint(under, faint) + repoLine(n, room, th) + th.paint(note, faint))
		}
	}
	return lines
}

// state is the state a row's dot shows: a working agent whose screen stands still is stalled.
func state(n *model.Node) string {
	if n.Stalled && n.Status == model.Working {
		return Stalled
	}
	return n.Status
}

// rollup is the dots of a folded effort's agents and workers, or the shell glyph for an effort
// that holds only shells.
func rollup(states []string, th Theme) string {
	out := make([]string, len(states))
	for i, s := range states {
		if s == RollupShell {
			out[i] = th.paint(th.ShellGlyph, faint)
		} else {
			out[i] = th.dot(s)
		}
	}
	return strings.Join(out, " ")
}

// effortTag is the right edge of an effort: "proj ↗" for a Linear project, else its label, with
// ↗ when it has a link to open.
func effortTag(n *model.Node) string {
	switch {
	case model.IsProject(n.Link):
		return "proj ↗"
	case n.Tag != "" && n.Link != "":
		return cut(n.Tag, maxTag) + " ↗"
	case n.Tag != "":
		return cut(n.Tag, maxTag)
	case n.Link != "":
		return "↗"
	}
	return ""
}

// workerTag is the right edge of a worker: its label, else the number of the pull request it links.
func workerTag(n *model.Node) string {
	if n.Tag != "" {
		return cut(n.Tag, maxTag)
	}
	return model.PRNumber(n.Link)
}

// repoLine is the second line of a row: the worktree glyph for a linked worktree, the repo with its
// branch, and the distance of the branch from its base. room is the cells it can take, or 0 for no
// limit; the branch is cut before the repo, and the counts are kept whole.
func repoLine(n *model.Node, room int, th Theme) string {
	glyph := ""
	if n.Worktree {
		glyph = th.WorktreeGlyph + " "
	}
	name, branch, tail := n.Repo, n.Git.Branch, ""
	if branch != "" {
		if n.Git.Behind > 0 {
			tail += fmt.Sprintf(" -%d", n.Git.Behind)
		}
		if n.Git.Ahead > 0 {
			tail += fmt.Sprintf(" +%d", n.Git.Ahead)
		}
	}
	if room > 0 {
		room -= lipgloss.Width(glyph) + lipgloss.Width(tail)
		if branch == "" {
			name = cut(name, max(minLabel, room))
		} else {
			name = cut(name, max(minLabel, room-1-minLabel))
			branch = cut(branch, max(minLabel, room-1-lipgloss.Width(name)))
		}
	}
	if branch != "" {
		name += "@" + branch
	}
	return th.paint(glyph, faint) + th.repo(name) + th.paint(tail, faint)
}

// guides is the lines of the workers above a row that run on past it.
func guides(trunk []bool) string {
	var b strings.Builder
	for _, runs := range trunk {
		if runs {
			b.WriteString("│ ")
		} else {
			b.WriteString("  ")
		}
	}
	return b.String()
}

// lead is what comes before the dot or glyph of a row: the fold mark of an effort, the indent of
// its panes, and the branch line of a worker, which starts under the dot of the agent above it.
func lead(r Row) string {
	switch r.Depth {
	case 0:
		mark := " "
		if r.HasChildren {
			mark = "▾"
			if r.Folded {
				mark = "▸"
			}
		}
		return mark + " "
	case 1:
		return "    "
	}
	b := "    " + guides(r.Trunk)
	switch {
	case r.Node.Shown != model.KindPane:
		return b + "  "
	case r.Last:
		return b + "└ "
	}
	return b + "├ "
}

// below is what comes before the second line of a row: the indent of an effort's panes, or the
// branch line of a worker running on to the next worker.
func below(r Row) string {
	if r.Depth == 0 {
		return "    "
	}
	b := "    " + guides(r.Trunk)
	if r.Last {
		return b + "    "
	}
	return b + "│   "
}

// tagStyle is the look of a row's right edge: underlined when the row has a link to open.
func tagStyle(n *model.Node) lipgloss.Style {
	if n.Link != "" {
		return faint.Underline(true)
	}
	return faint
}
```

- [ ] **Step 5: Change the theme**

In `tree/internal/view/theme.go`:

In `Theme`, replace the `TabGlyph string` and `PaneGlyph string` fields with:

```go
	// ShellGlyph stands before the name of a pane that holds no agent.
	ShellGlyph string
```

Change the `DefaultTheme` comment to `// DefaultTheme uses Nerd Font glyphs for the worktree and held prompts.` and replace `TabGlyph: "\U000f04e9",` and `PaneGlyph: "",` with `ShellGlyph: "$",`.

In `LoadTheme`, replace the `Tab` and `Pane` fields of the `Glyphs` struct with `Shell string \`toml:"shell"\``, and replace the two `if file.Glyphs.Tab ...` and `if file.Glyphs.Pane ...` blocks with:

```go
	if file.Glyphs.Shell != "" {
		th.ShellGlyph = file.Glyphs.Shell
	}
```

- [ ] **Step 6: Keep folds of efforts only**

In `tree/internal/view/folds.go`, replace the body of `SaveFolds` from `keep := ...` through `mark(t.Hidden)` with:

```go
	keep := map[string]bool{}
	for _, n := range t.Roots {
		keep[n.ID] = true
	}
```

and change its comment to `// SaveFolds writes the folds of the efforts that are still there.`

- [ ] **Step 7: Change the program**

In `tree/internal/view/program.go`:

1. In `Update`, `case tea.KeyMsg`: delete the whole `case "a":` branch. Replace the `case "enter":` branch with:

```go
		case "enter":
			if len(p.rows) == 0 {
				break
			}
			return p, p.jump(p.rows[p.cursor].Node)
```

2. Replace the comment and loop guard of `advance`:

```go
// advance puts the cursor on the next agent or worker row in the direction of by, past the ends of
// the view, and jumps to it. Effort and shell rows do not take a step.
func (p Program) advance(by int) (tea.Model, tea.Cmd) {
	for i := 1; i <= len(p.rows); i++ {
		at := ((p.cursor+by*i)%len(p.rows) + len(p.rows)) % len(p.rows)
		n := p.rows[at].Node
		if n.Shown != model.KindPane {
			continue
		}
```

(the rest of `advance` is unchanged).

3. In `siblings`, change the comment to `// siblings returns the rows at the depth of row i in the same run, from the top.` and replace `if p.rows[j].Depth == depth && p.rows[j].Node.Shown != model.KindGroup {` with `if p.rows[j].Depth == depth {`.

4. In `shift`, change the comment to `// shift moves the effort under the cursor one place down or up, by its workspace in Herdr.` and replace the first guard with:

```go
	if len(p.rows) == 0 || p.rows[p.cursor].Depth != 0 {
		return nil
	}
```

5. In `drop`, change the comment to `// drop moves the effort of row from to the place of the effort that holds row to. Pane rows do not move.` and add as its first statement:

```go
	if p.rows[from].Depth != 0 {
		return nil
	}
```

6. In `mouse`, `case tea.MouseButtonLeft`: replace `if r.Node.Shown == model.KindGroup || (r.HasChildren && m.X < 1+lipgloss.Width(lead(r))) {` with `if r.HasChildren && m.X < 1+lipgloss.Width(lead(r)) {`. Change the comment above `mouse` to: `// mouse handles a click on a row, a drag of an effort, and the wheel. Ctrl with a click opens the link of a row. A press on the fold mark folds an effort. A press elsewhere jumps to the row when the button comes up on it, and moves an effort when it comes up on another one.`

7. In `menuTitle`, replace `if n.Focus == model.KindPane {` with `if n.Shown == model.KindPane {`, and change the last sentence of its comment to `The pane is empty for a row that is not an agent.`

8. In `menuTarget`, add before `case model.KindPane:`:

```go
	case model.KindShell:
		return "pane;" + n.ID
```

and change its comment to `// menuTarget names what a row stands for on screen, as "<kind>;<id>".`

9. In `View`, replace the title lines with `title := " Efforts"` (delete the `if p.state.Attention` block), replace `"   no agents\n"` with `"   no workspaces\n"`, and replace the footer with `foot := " ↑↓ move  ⇧↑↓ reorder  ⏎ jump  ␣ fold  q close"`.

10. In `sidebarView`, replace `" no agents"` with `" no workspaces"` and delete the `if p.state.Attention && len(lines) == 0 { ... }` block.

11. In the `Program` struct comment for `offset`, replace `A row with a repo takes two lines.` with `An effort or a worker with a repo takes two lines.`

- [ ] **Step 8: Run the view tests**

Run: `cd tree && go test ./internal/view/`
Expected: `ok`. If `TestRenderAnEffort` differs only in spaces, count cells with the rule in Task 3 Step 4 (`pad = width - used - label - right`) before you change the test; the test values are worked out from that rule.

- [ ] **Step 9: Run every test and vet**

Run: `cd tree && go vet ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 10: Commit**

```bash
cd ~/Projects/personal/herdr-plugins
git add tree/internal/view
git commit -F - <<'EOF'
tree: draw efforts with their shells, agents and workers

Each effort is one row with its tag at the right edge and its repo line
under it. Shells come first, then agents, and each agent's workers hang
from a branch line under its dot with their repo line and shells. The
step keys stop on agent and worker rows only, and only an effort folds,
drags or reorders.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 4: Wire the ledger in, remove the tree's leftovers, update the docs

**Files:**
- Modify: `tree/cmd/tree/main.go:33-40`
- Modify: `tree/internal/model/types.go` (remove `KindTab`, `KindGroup`, `Tree.NoAgent`, `Tree.Hidden`)
- Modify: `tree/internal/source/herdr.go:201-212` (`Focus`)
- Modify: `tree/herdr-plugin.toml`, `tree/README.md`, `tree/test/smoke.sh`
- Test: `tree/internal/source/herdr_test.go` (`TestFocusByKind`)

**Interfaces:**
- Consumes: `source.Settled`, `source.LedgerDir` (Task 2), `model.Agent.Reported` (Task 1).
- Produces: the final `tree` binary behavior; `Kind` has only `KindPane`, `KindWorkspace`, `KindShell`.

- [ ] **Step 1: Write the failing test**

In `tree/internal/source/herdr_test.go`, `TestFocusByKind`: delete the `model.KindTab` row of the table, and replace the last check with:

```go
	if err := Focus(&recorder{}, &model.Node{Focus: model.Kind(99)}); err == nil {
		t.Error("a row of no known kind has nothing to focus")
	}
```

- [ ] **Step 2: Remove the old kinds and tree fields**

In `tree/internal/model/types.go`, replace the `Kind` block with:

```go
const (
	KindPane Kind = iota
	KindWorkspace
	// KindShell is a pane with no agent.
	KindShell
)
```

and replace `Tree` with:

```go
// Tree is the built view: one effort for each workspace that has its own agent or no agent.
type Tree struct {
	Roots []*Node
}
```

In `tree/internal/source/herdr.go`, `Focus`: delete the `case model.KindTab:` branch and its `return`.

In `tree/internal/view/program.go`, `menuTarget`: delete the `case model.KindTab:` branch and its `return`.

- [ ] **Step 3: Run the tests**

Run: `cd tree && go vet ./... && go test ./...`
Expected: all `ok`. A compile error here names a leftover use of `KindTab`, `KindGroup`, `NoAgent` or `Hidden`; remove it (no code path still needs them after Task 3).

- [ ] **Step 4: Read the ledger on each load**

In `tree/cmd/tree/main.go`, replace the `load` function with:

```go
	load := func() (model.Tree, error) {
		snap, err := source.Snapshot(sock)
		if err != nil {
			return model.Tree{}, err
		}
		// Marks the workers whose result their supervisor took.
		settled := source.Settled(source.LedgerDir())
		for i := range snap.Agents {
			snap.Agents[i].Reported = settled[snap.Agents[i].PaneID]
		}
		current.Store(&[2]string{snap.FocusedPane, snap.FocusedTab})
		return model.Build(snap), nil
	}
```

Change the package comment to `// Command tree shows Herdr workspaces as efforts: their shells, their agents, and the workers each agent dispatched.`

- [ ] **Step 5: Update the plugin manifest**

In `tree/herdr-plugin.toml`, set:

```toml
description = "Workspaces as efforts: their shells, their agents, and the workers each agent dispatched, with the state of each."
```

and under `[[panes]]` set `title = "Efforts"`.

- [ ] **Step 6: Update the smoke test**

In `tree/test/smoke.sh`, replace the line that checks `" ▸ No agent (2)"` with:

```bash
[ "$(grep -c '^     \$ ' <<<"$out")" = 2 ] || { echo "FAIL: expected one shell row under each of the two workspaces"; exit 1; }
grep -q '^ ▾ second$' <<<"$out" || { echo "FAIL: expected the effort row of the workspace 'second'"; exit 1; }
```

Do not run it in this task: it builds `bin/tree`.

- [ ] **Step 7: Update the README**

In `tree/README.md`:

- Replace the first paragraph under `# tree` with: `The Herdr sidebar section that shows each workspace as an effort: the shells and agents in it, and under each agent the workers it dispatched to other workspaces, each with its state.`
- Replace the whole `## What a row is` section with:

```markdown
## What a row is

- **Effort:** one for each workspace, in Herdr's order. The name is bold. The right edge shows the label and link of the workspace's first agent: `SRE-923 ↗` for an issue, `proj ↗` for a Linear project. The line under it is the repo and branch, with the commits ahead of and behind the upstream.
- **Shell:** a pane with no agent, marked `$`. It shows the pane's name, or where it is: `<repo>/<path>` inside a repository, else the path from `~`.
- **Agent:** a pane with an agent. It shows the state dot and the title the session set.
- **Worker:** an agent that an agent in another workspace supervises. It hangs from a branch line under that agent's dot. The right edge shows its label, else the number of the pull request it links. The line under it is the worktree glyph for a linked worktree, the repo and branch, the commits ahead of and behind `origin/HEAD`, and `reported` once its supervisor took its result. The worker's own shells follow, at its text column.

A workspace whose agents are all workers is not an effort: its shells show under its first worker. A supervisor in the same workspace does not make a worker.

A folded effort is one line with a dot for each of its agents and workers, or `$` when it has only shells. Click the fold mark or press space on the effort to fold or open it. Folds are saved.
```

- Replace the first sentence of `## Panes that do not count` with: `A pane that sets the \`tree=hide\` token on itself is left out.`
- In `## Configuration`, replace the `[glyphs]` block with:

```toml
[glyphs]
# The default worktree and pending glyphs are Nerd Font icons. These work in any font.
shell = "$"
worktree = "⎇"
pending = "+"
```

- In `## Use in scripts`, replace the first sentence with: `` `tree --once` prints the efforts as plain text and exits. ``
- In `## Keys`, delete the line for `a` (attention filter), if there is one.

Read the whole README after these edits and fix any other sentence that still describes tab rows, the `No agent` or `Hidden` groups, or the attention filter.

- [ ] **Step 8: Run the tests again**

Run: `cd tree && go vet ./... && go test ./...`
Expected: all `ok`.

- [ ] **Step 9: Commit**

```bash
cd ~/Projects/personal/herdr-plugins
git add tree
git commit -F - <<'EOF'
tree: mark reported workers and describe the effort view

The pane reads the dispatch ledgers on each load. The tab and group
kinds are gone, and the manifest, README and smoke test describe the
effort view.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 5: The herdr-tree skill sets the link with the label

**Files:**
- Modify: `tree/skill/SKILL.md` (live at `~/.agents/skills/herdr-tree` and `~/.claude/skills/herdr-tree` through symlinks)

**Interfaces:**
- Consumes: the `label` and `link` pane tokens the view reads (Tasks 1 to 4).
- Produces: an agent that starts work on an issue or a project sets both tokens on its own pane.

- [ ] **Step 1: Rewrite the skill**

Replace all of `tree/skill/SKILL.md` with:

````markdown
---
name: herdr-tree
description: Label this agent's row in the Herdr sidebar with the ticket or project it works on, and link the row to it. Use when work starts on a ticket, issue or project in this session ("let's get started on SRE-923", "pick up this ticket", "work on the GKE migration project"), when the session moves to different work, and when the user says "label this agent", "set the label", or "clear the label". Requires HERDR_ENV=1.
---

# Herdr sidebar label and link

The Herdr sidebar shows each workspace as an effort. The right edge of the effort row comes from its first agent's pane: the `label` token (an issue key) and the `link` token (the web address of the issue or project). An issue shows `SRE-923 ↗`, a Linear project shows `proj ↗`. A click with ctrl opens the link.

Check `test "${HERDR_ENV:-}" = 1` first. If it fails, do nothing.

## Set them

Do this as soon as the work is known, before other work on it.

For an issue, set the key and the issue's address:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --token label=SRE-923 --token link=https://linear.app/flosports/issue/SRE-923
```

For a project, set only the project's address. A project has no key, and the workspace name already names it:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --token link=https://linear.app/flosports/project/gke-aws-eks-migration-f425468449cd
```

- Take the address from the tracker, not from memory. For Linear, an issue is `https://linear.app/<org>/issue/<KEY>`. A project's address has a slug; read it from the API (`projects(filter: {name: {contains: "..."}}) { nodes { name url } }`).
- Use the key alone for the label, in the form the tracker uses (`SRE-923`, `#42`). The sidebar shows at most 12 characters.
- A session has one label and one link. New work replaces both with the same command. When new work is a project, clear the old label.
- With no ticket or project, set them only when the user asks.

## Clear them

When the user asks, or when the session takes up work with no ticket or project:

```bash
herdr pane report-metadata "$HERDR_PANE_ID" --source tree --clear-token label --clear-token link
```

## Rules

- Set them only on your own pane, `$HERDR_PANE_ID`. A label on another agent's pane is the user's to set, from the row's right-click menu.
- Keep `--source tree`. The menu clears and replaces the label under that source.
- Do not report this to the user as a step. It is bookkeeping.
````

- [ ] **Step 2: Check the command flags**

Run: `herdr pane report-metadata --help`
Expected: it accepts `--token` more than once and `--clear-token` more than once. If it takes only one token per call, change the skill to one call per token and say so in the skill text.

- [ ] **Step 3: Commit**

```bash
cd ~/Projects/personal/herdr-plugins
git add tree/skill/SKILL.md
git commit -F - <<'EOF'
tree: set the link with the label when work starts

The effort row takes its tag from the first agent's label and link. The
skill sets both for an issue, and the link alone for a Linear project.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 6: Check against the live session, then deploy (needs Matt)

**Files:** none changed unless the live check finds a defect.

- [ ] **Step 1: Build outside bin/**

Run: `cd tree && go build -o "$SCRATCH/tree-new" ./cmd/tree` (set `SCRATCH` to the session scratchpad path).
Expected: no output.

- [ ] **Step 2: Print the live view**

Run: `HERDR_SOCKET_PATH="$HOME/.config/herdr/herdr.sock" "$SCRATCH/tree-new" --once`
Expected: one effort for each workspace with an agent of its own or no agent, with the shells first and the workers under their supervisors, as in the spec's mockup. Compare row by row with `herdr workspace list`, `herdr pane list` and `herdr agent list`. Write down each difference.

- [ ] **Step 3: Fix each difference**

For each difference, write a failing test in the package that owns it, fix it, run `go test ./...`, and commit with a `tree: fix ...` subject. If a difference is a question of design, stop and ask Matt.

- [ ] **Step 4: Ask Matt before the deploy**

The next step changes the live sidebar at once. Show Matt the `--once` output and ask for the go-ahead.

- [ ] **Step 5: Deploy and smoke test**

Run: `cd tree && test/smoke.sh`
Expected: `PASS`. It builds `bin/tree`, so the live sidebar restarts with the new view within a second.

- [ ] **Step 6: Check the live sidebar**

Ask Matt to look at the sidebar and try a fold, a click on an agent, a ctrl+click on `↗`, a right-click on a shell, and alt+j / alt+k. Fix what fails as in Step 3.
