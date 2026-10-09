# Supervisor Tree Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Herdr plugin pane that shows every agent as a tree by supervisor, with each agent's state, and jumps to the selected agent.

**Architecture:** One Go binary in a new `tree/` plugin directory. `internal/model` builds the tree from plain structs with no I/O. `internal/source` reads the Herdr socket and the dispatch plugin's state files. `internal/view` flattens the tree to rows, renders them, and runs a Bubble Tea program.

**Tech Stack:** Go, Bubble Tea v1, Lip Gloss v1, BurntSushi/toml, Herdr 0.9.3 socket API.

**Spec:** `tree/docs/specs/2026-10-09-supervisor-tree-design.md`. Read it before any task.

## Global Constraints

- Work in the `herdr-plugins` repository on a branch named `tree-plugin`. All paths below are relative to the repository root.
- Plugin id is `herdr-tree`. `min_herdr_version = "0.9.1"`. Platforms `linux`, `macos`.
- Go module path is `github.com/mago0/herdr-plugins/tree`.
- The plugin is read-only. The only write to Herdr is one focus call. It never writes under `<state>/agent-dispatch/`.
- `internal/model` imports nothing outside the standard library and does no I/O.
- The repository is public. No employer names, ticket keys, host names or home-directory paths in code, tests, fixtures or docs. Use generic names (`ops`, `api`, `ABC-12`).
- Code comments say what the thing is for. One or two lines. No history, no ticket references.
- No em dashes or en dashes anywhere. Hyphens only.
- Run `gofmt -l .` and `go vet ./...` from `tree/` before every commit. Both must print nothing.

## Facts about Herdr 0.9.3 that the code depends on

These were checked against a live server. Do not re-derive them.

- The socket path is in `HERDR_SOCKET_PATH`. A request is one JSON line: `{"id":"x","method":"workspace.list","params":{}}`. The reply is one JSON line: `{"id":"x","result":{...}}` or `{"id":"x","error":{"code":"pane_not_found","message":"..."}}`. One request per connection.
- `workspace.list` -> `result.workspaces[]`: `workspace_id`, `label`, `number`, `agent_status`, optional `worktree.repo_name`.
- `tab.list` with `{}` returns every tab: `result.tabs[]`: `tab_id`, `workspace_id`, `number`, `label`, `agent_status`.
- `agent.list` -> `result.agents[]`: `pane_id`, `tab_id`, `workspace_id`, `agent` (kind), optional `name`, `agent_status`, `terminal_title_stripped`. It does not return tokens.
- `pane.list` with `{}` returns every pane: `result.panes[]`: `pane_id`, optional `tokens` (object, values string or null).
- Focus: `pane.focus {"pane_id"}`, `tab.focus {"tab_id"}`, `workspace.focus {"workspace_id"}`.
- Ids look like `w6G` (workspace), `w6G:t9` (tab), `w6G:p1` (pane). They survive a server restart, and a closed id is not used again.
- A plugin pane command gets `HERDR_PLUGIN_CONTEXT_JSON` (fields `focused_pane_id`, `tab_id`), `HERDR_PLUGIN_STATE_DIR` and `HERDR_PLUGIN_CONFIG_DIR`.
- The sidebar draws the `unknown` state as `·` (U+00B7) in `#6c7086`.
- An isolated Herdr session for tests needs its own `XDG_CONFIG_HOME`. `--session <name>` and `HERDR_CONFIG_PATH` both still load the user's plugins. The socket path must be short (under about 100 bytes).

## Review Focus

1. **A ledger that is valid JSON but the wrong shape** (an object, or entries with a number where a string is expected). Expected: that run is skipped and every other run still loads. Test in Task 5.
2. **A pane narrower than the content** (width 10, or 0 before the first size message). Expected: no panic, no line wider than the pane. Test in Task 7.
3. **Labels with wide characters** (CJK, emoji). Expected: cut by display width, right column still aligned. Test in Task 7.
4. **The tree gets shorter while the cursor is on the last row** (workers released between polls). Expected: the cursor moves to the new last row and `enter` acts on a real row. Test in Task 8.
5. **A socket reply larger than a default buffer** (several hundred agents). Expected: the full reply is read. Test in Task 4.

---

### Task 1: Module, manifest, model types and label helpers

**Files:**
- Create: `tree/herdr-plugin.toml`, `tree/go.mod`, `tree/.gitignore`
- Create: `tree/internal/model/types.go`, `tree/internal/model/labels.go`
- Test: `tree/internal/model/labels_test.go`

**Interfaces:**
- Produces: every type in `types.go` below, plus `AgentLabel(Agent) string`, `TicketKey(runName string) string`, `WorktreeRepo(path string) string`.

- [ ] **Step 1: Branch and scaffold**

```bash
git switch -c tree-plugin
mkdir -p tree/cmd/tree tree/internal/model tree/internal/source tree/internal/view
cd tree && go mod init github.com/mago0/herdr-plugins/tree
printf 'bin/\n' > .gitignore
```

`tree/herdr-plugin.toml`:

```toml
id = "herdr-tree"
name = "Tree"
version = "0.1.0"
min_herdr_version = "0.9.1"
description = "Agents as a tree by supervisor, with the state of each, and a jump to the selected one."
platforms = ["linux", "macos"]

[[build]]
command = ["go", "build", "-o", "bin/tree", "./cmd/tree"]

[[panes]]
id = "tree"
title = "Agents by supervisor"
placement = "popup"
command = ["./bin/tree"]
```

- [ ] **Step 2: Write `tree/internal/model/types.go`**

```go
// Package model builds the supervisor tree from plain data. It does no I/O.
package model

// Agent states, as Herdr reports them in agent_status.
const (
	Blocked = "blocked"
	Working = "working"
	Done    = "done"
	Idle    = "idle"
	Unknown = "unknown"
)

type Workspace struct {
	ID, Label, Status, Repo string
	Number                  int
}

type Tab struct {
	ID, WorkspaceID, Label, Status string
	Number                         int
}

// Agent is one pane that hosts an agent. Hide is true when the pane set the tree=hide token.
type Agent struct {
	PaneID, TabID, WorkspaceID string
	Name, Kind, Title, Status  string
	Hide                       bool
}

// Snapshot is the live Herdr state. Agents are in Herdr's order.
type Snapshot struct {
	Workspaces []Workspace
	Tabs       []Tab
	Agents     []Agent
}

// Entry is one dispatch attempt from a run ledger.
type Entry struct {
	PaneID, TabID, WorkspaceID, Worktree, Created string
}

// Run is one dispatch run: the pane that supervises it and the workers it started.
type Run struct {
	Name, Supervisor string
	Entries          []Entry
}

// Dispatch is the dispatch plugin's state. Tracked holds the panes of loops it tracks.
type Dispatch struct {
	Runs    []Run
	Tracked map[string]bool
}

type Kind int

const (
	KindPane Kind = iota
	KindTab
	KindWorkspace
	KindGroup
)

// Node is one row. Focus is what enter jumps to; Shown is what the row stands for on screen.
type Node struct {
	ID                 string
	Focus, Shown       Kind
	TabID, WorkspaceID string
	Label, Status      string
	Repo, Ticket       string
	Children           []*Node
	order              [3]int
}

// Tree is the built view: supervisors and their workers, then the two folded groups.
type Tree struct {
	Roots   []*Node
	NoAgent []*Node
	Hidden  []*Node
}
```

- [ ] **Step 3: Write the failing test `tree/internal/model/labels_test.go`**

```go
package model

import "testing"

func TestAgentLabel(t *testing.T) {
	cases := []struct {
		a    Agent
		want string
	}{
		{Agent{Name: "add-probe", Kind: "claude", Title: "x"}, "add-probe"},
		{Agent{Kind: "pr-watch-86", Title: "/some/script"}, "pr-watch-86"},
		{Agent{Kind: "claude", Title: "Plan the api redesign"}, "Plan the api redesign"},
		{Agent{Kind: "codex", Title: "0123456789012345678901234567890123456789"}, "0123456789012345678901234567890123"},
		{Agent{Kind: "claude"}, "claude"},
		{Agent{}, "agent"},
	}
	for _, c := range cases {
		if got := AgentLabel(c.a); got != c.want {
			t.Errorf("AgentLabel(%+v) = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestTicketKey(t *testing.T) {
	cases := map[string]string{
		"abc-923-soak":        "ABC-923",
		"ops-12":              "OPS-12",
		"review-20261009":     "",
		"abc-review-20261009": "",
		"cleanup":             "",
		"x-1":                 "",
	}
	for in, want := range cases {
		if got := TicketKey(in); got != want {
			t.Errorf("TicketKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorktreeRepo(t *testing.T) {
	cases := map[string]string{
		"/src/api/_worktrees/abc-1-fix": "api",
		"/src/api":                      "",
		"":                              "",
	}
	for in, want := range cases {
		if got := WorktreeRepo(in); got != want {
			t.Errorf("WorktreeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 4: Run it and see it fail**

Run: `cd tree && go test ./internal/model/`
Expected: build failure, `undefined: AgentLabel`.

- [ ] **Step 5: Write `tree/internal/model/labels.go`**

```go
package model

import (
	"path"
	"regexp"
	"strings"
)

// Agent kinds that say nothing as a row label.
var generic = map[string]bool{
	"": true, "claude": true, "codex": true, "gemini": true, "opencode": true, "omp": true, "pi": true,
}

const titleRunes = 34

// AgentLabel is the row label for a pane: its name, else a specific kind, else its terminal title.
func AgentLabel(a Agent) string {
	if a.Name != "" {
		return a.Name
	}
	if !generic[a.Kind] {
		return a.Kind
	}
	if a.Title != "" {
		r := []rune(a.Title)
		if len(r) > titleRunes {
			r = r[:titleRunes]
		}
		return string(r)
	}
	if a.Kind != "" {
		return a.Kind
	}
	return "agent"
}

var ticketRE = regexp.MustCompile(`(?i)\b([a-z]{2,6})-(\d{1,5})\b`)

// TicketKey is the first issue key in a run name, upper case, or "" when there is none.
func TicketKey(runName string) string {
	m := ticketRE.FindStringSubmatch(runName)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}

// WorktreeRepo is the repository a dispatch worktree belongs to, from <repo>/_worktrees/<branch>.
func WorktreeRepo(worktree string) string {
	i := strings.Index(worktree, "/_worktrees/")
	if i < 0 {
		return ""
	}
	return path.Base(worktree[:i])
}
```

- [ ] **Step 6: Run the tests**

Run: `cd tree && go test ./internal/model/`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
cd tree && gofmt -l . && go vet ./...
git add . && git commit -m "tree: scaffold the plugin, model types and label helpers"
```

---

### Task 2: Parent links from the dispatch state

**Files:**
- Create: `tree/internal/model/links.go`
- Test: `tree/internal/model/links_test.go`

**Interfaces:**
- Consumes: `Agent`, `Dispatch`, `Run`, `Entry`, `TicketKey`, `WorktreeRepo` from Task 1.
- Produces: `resolveLinks(agents []Agent, d Dispatch) links` where `links` has `parent map[string]string` (pane to supervising pane), `repo map[string]string` (pane to worktree repository), `ticket map[string]string` (supervising pane to ticket key). `agents` must already have hidden panes removed.

Rules, from the spec section "Parent":

- The supervisor is the run's supervisor pane, and it must be a live agent pane. No fallback.
- The worker is the entry's `pane_id`. If that pane is not a live agent pane, it falls back to the first live agent of the entry's tab. For an entry with no `tab_id` (placed as a workspace) it then falls back to the first live agent of the entry's workspace.
- Skip an entry that does not resolve, or that resolves to the supervisor itself.
- Latest `created` wins when several entries resolve to one pane. On a tie the later run wins.
- A link that closes a cycle is removed.
- A supervisor gets a ticket only when every one of its runs yields the same key.

- [ ] **Step 1: Write the failing test `tree/internal/model/links_test.go`**

```go
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
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd tree && go test ./internal/model/ -run ResolveLinks`
Expected: build failure, `undefined: resolveLinks`.

- [ ] **Step 3: Write `tree/internal/model/links.go`**

```go
package model

type links struct {
	parent map[string]string // pane -> supervising pane
	repo   map[string]string // pane -> repository of its worktree
	ticket map[string]string // supervising pane -> ticket key
}

// resolveLinks reads the supervisor of each worker from the dispatch runs.
func resolveLinks(agents []Agent, d Dispatch) links {
	live := map[string]bool{}
	firstInTab, firstInWorkspace := map[string]string{}, map[string]string{}
	for _, a := range agents {
		live[a.PaneID] = true
		if _, ok := firstInTab[a.TabID]; !ok {
			firstInTab[a.TabID] = a.PaneID
		}
		if _, ok := firstInWorkspace[a.WorkspaceID]; !ok {
			firstInWorkspace[a.WorkspaceID] = a.PaneID
		}
	}
	// A worker whose pane was replaced is still the agent in the place dispatch put it.
	worker := func(e Entry) string {
		if live[e.PaneID] {
			return e.PaneID
		}
		if e.TabID != "" {
			return firstInTab[e.TabID]
		}
		return firstInWorkspace[e.WorkspaceID]
	}

	l := links{parent: map[string]string{}, repo: map[string]string{}, ticket: map[string]string{}}
	born := map[string]string{}
	keys := map[string]map[string]bool{}
	for _, run := range d.Runs {
		sup := run.Supervisor
		if !live[sup] {
			continue
		}
		if keys[sup] == nil {
			keys[sup] = map[string]bool{}
		}
		keys[sup][TicketKey(run.Name)] = true
		for _, e := range run.Entries {
			child := worker(e)
			if child == "" || child == sup {
				continue
			}
			if prev, seen := born[child]; seen && e.Created < prev {
				continue
			}
			born[child] = e.Created
			l.parent[child] = sup
			l.repo[child] = WorktreeRepo(e.Worktree)
		}
	}
	for sup, ks := range keys {
		if len(ks) != 1 {
			continue
		}
		for k := range ks {
			if k != "" {
				l.ticket[sup] = k
			}
		}
	}
	// A pane that is its own ancestor becomes a root.
	for _, a := range agents {
		seen := map[string]bool{a.PaneID: true}
		for p := l.parent[a.PaneID]; p != ""; p = l.parent[p] {
			if p == a.PaneID {
				delete(l.parent, a.PaneID)
				break
			}
			if seen[p] {
				break
			}
			seen[p] = true
		}
	}
	return l
}
```

- [ ] **Step 4: Run the tests**

Run: `cd tree && go test ./internal/model/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd tree && gofmt -l . && go vet ./...
git add . && git commit -m "tree: resolve supervisor links from dispatch runs"
```

---

### Task 3: Build the tree

**Files:**
- Create: `tree/internal/model/build.go`
- Test: `tree/internal/model/build_test.go`

**Interfaces:**
- Consumes: `resolveLinks` and `links` from Task 2, all types from Task 1.
- Produces: `Build(s Snapshot, d Dispatch) Tree`.

Rules, from the spec sections "Panes that do not count", "Rows for tabs and workspaces" and "Order":

- An agent with `Hide`, or whose pane is in `d.Tracked`, goes to `Tree.Hidden` and takes no part in anything else.
- Tab level: a head is an agent whose parent is not in the same tab. One head: that node takes the tab's label and `Shown = KindTab`. Several: a container node (`ID` is the tab id, `Focus` and `Shown` are `KindTab`, status is the tab's) takes the heads as children; if a head had a parent, the container takes that parent.
- Workspace level, same rule over the tab rows: one head takes the workspace label and `Shown = KindWorkspace`; several get a container (`ID` is the workspace id, `Focus` and `Shown` are `KindWorkspace`). A workspace with no visible agent goes to `Tree.NoAgent`.
- `Repo` is the worktree repository from the link, else the repository of the node's workspace.
- Children are in Herdr order: workspace number, tab number, agent position. Roots with children come before roots without.

- [ ] **Step 1: Write the failing test `tree/internal/model/build_test.go`**

The helper `outline` prints one line per node, indented two spaces per level, as `label kind repo`, where kind is `p`, `t`, `w`.

```go
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
```

Note the trailing space after `w` on rows with an empty repository. Keep it; `outline` writes it.

- [ ] **Step 2: Run it and see it fail**

Run: `cd tree && go test ./internal/model/ -run Build`
Expected: build failure, `undefined: Build`.

- [ ] **Step 3: Write `tree/internal/model/build.go`**

```go
package model

import "sort"

// Build turns the live Herdr state and the dispatch state into the supervisor tree.
func Build(s Snapshot, d Dispatch) Tree {
	workspaces := append([]Workspace(nil), s.Workspaces...)
	sort.SliceStable(workspaces, func(i, j int) bool { return workspaces[i].Number < workspaces[j].Number })
	wsByID := map[string]Workspace{}
	for _, w := range workspaces {
		wsByID[w.ID] = w
	}
	tabs := append([]Tab(nil), s.Tabs...)
	sort.SliceStable(tabs, func(i, j int) bool {
		a, b := wsByID[tabs[i].WorkspaceID].Number, wsByID[tabs[j].WorkspaceID].Number
		if a != b {
			return a < b
		}
		return tabs[i].Number < tabs[j].Number
	})
	tabByID := map[string]Tab{}
	for _, t := range tabs {
		tabByID[t.ID] = t
	}

	var tree Tree
	var visible []Agent
	for _, a := range s.Agents {
		if a.Hide || d.Tracked[a.PaneID] {
			tree.Hidden = append(tree.Hidden, &Node{
				ID: a.PaneID, Focus: KindPane, Shown: KindPane, TabID: a.TabID, WorkspaceID: a.WorkspaceID,
				Label: AgentLabel(a), Status: a.Status, Repo: wsByID[a.WorkspaceID].Repo,
			})
			continue
		}
		visible = append(visible, a)
	}

	l := resolveLinks(visible, d)
	nodes := map[string]*Node{}
	up := map[string]string{}
	for i, a := range visible {
		repo := l.repo[a.PaneID]
		if repo == "" {
			repo = wsByID[a.WorkspaceID].Repo
		}
		nodes[a.PaneID] = &Node{
			ID: a.PaneID, Focus: KindPane, Shown: KindPane, TabID: a.TabID, WorkspaceID: a.WorkspaceID,
			Label: AgentLabel(a), Status: a.Status, Repo: repo, Ticket: l.ticket[a.PaneID],
			order: [3]int{wsByID[a.WorkspaceID].Number, tabByID[a.TabID].Number, i},
		}
		if p := l.parent[a.PaneID]; p != "" {
			up[a.PaneID] = p
		}
	}

	// A container holds several heads. It takes the place of a dispatched head under its supervisor.
	contain := func(c *Node, heads []string) {
		nodes[c.ID] = c
		for _, h := range heads {
			if p, ok := up[h]; ok && up[c.ID] == "" {
				up[c.ID] = p
			}
			up[h] = c.ID
		}
	}

	// Tab rows: one head and the tab row is that agent; several and the tab is a container.
	tabRow := map[string]string{}
	for _, t := range tabs {
		var heads []string
		for _, a := range visible {
			if a.TabID != t.ID {
				continue
			}
			if p, ok := nodes[up[a.PaneID]]; ok && p.TabID == t.ID {
				continue
			}
			heads = append(heads, a.PaneID)
		}
		w := wsByID[t.WorkspaceID]
		switch {
		case len(heads) == 1:
			n := nodes[heads[0]]
			n.Label, n.Shown = t.Label, KindTab
			tabRow[t.ID] = n.ID
		case len(heads) > 1:
			contain(&Node{
				ID: t.ID, Focus: KindTab, Shown: KindTab, TabID: t.ID, WorkspaceID: t.WorkspaceID,
				Label: t.Label, Status: t.Status, Repo: nodes[heads[0]].Repo,
				order: [3]int{w.Number, t.Number, -1},
			}, heads)
			tabRow[t.ID] = t.ID
		}
	}

	// Workspace rows: the same rule over the tab rows.
	for _, w := range workspaces {
		var heads []string
		any := false
		for _, t := range tabs {
			id, ok := tabRow[t.ID]
			if t.WorkspaceID != w.ID || !ok {
				continue
			}
			any = true
			if p, ok := nodes[up[id]]; ok && p.WorkspaceID == w.ID {
				continue
			}
			heads = append(heads, id)
		}
		switch {
		case !any:
			tree.NoAgent = append(tree.NoAgent, &Node{
				ID: w.ID, Focus: KindWorkspace, Shown: KindWorkspace, WorkspaceID: w.ID,
				Label: w.Label, Status: w.Status, Repo: w.Repo,
			})
		case len(heads) == 1:
			n := nodes[heads[0]]
			n.Label, n.Shown = w.Label, KindWorkspace
		case len(heads) > 1:
			contain(&Node{
				ID: w.ID, Focus: KindWorkspace, Shown: KindWorkspace, WorkspaceID: w.ID,
				Label: w.Label, Status: w.Status, Repo: w.Repo,
				order: [3]int{w.Number, -1, -1},
			}, heads)
		}
	}

	less := func(a, b *Node) bool {
		for i := range a.order {
			if a.order[i] != b.order[i] {
				return a.order[i] < b.order[i]
			}
		}
		return a.ID < b.ID
	}
	for id, n := range nodes {
		if p, ok := nodes[up[id]]; ok {
			p.Children = append(p.Children, n)
		} else {
			tree.Roots = append(tree.Roots, n)
		}
	}
	for _, n := range nodes {
		sort.Slice(n.Children, func(i, j int) bool { return less(n.Children[i], n.Children[j]) })
	}
	sort.Slice(tree.Roots, func(i, j int) bool {
		a, b := tree.Roots[i], tree.Roots[j]
		if (len(a.Children) > 0) != (len(b.Children) > 0) {
			return len(a.Children) > 0
		}
		return less(a, b)
	})
	return tree
}
```

- [ ] **Step 4: Run the tests**

Run: `cd tree && go test ./internal/model/`
Expected: `ok`. If `TestBuildDispatchedTabWithSecondAgent` fails on the label of the container, check that the workspace loop relabels a single head even when that head is a tab container.

- [ ] **Step 5: Commit**

```bash
cd tree && gofmt -l . && go vet ./...
git add . && git commit -m "tree: build the supervisor tree with tab and workspace rows"
```

---

### Task 4: Herdr socket client and snapshot

**Files:**
- Create: `tree/internal/source/herdr.go`
- Test: `tree/internal/source/herdr_test.go`

**Interfaces:**
- Consumes: `model.Snapshot`, `model.Workspace`, `model.Tab`, `model.Agent`, `model.Node`, `model.Kind*`.
- Produces:
  - `type Caller interface { Call(method string, params, result any) error }`
  - `type Socket struct{ Path string }` implementing `Caller`
  - `type APIError struct{ Code, Message string }` implementing `error`
  - `func Snapshot(c Caller) (model.Snapshot, error)`
  - `func Focus(c Caller, n *model.Node) error`

- [ ] **Step 1: Write the failing test `tree/internal/source/herdr_test.go`**

```go
package source

import (
	"bufio"
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
				conn.Write([]byte(replies[req.Method] + "\n"))
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
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd tree && go test ./internal/source/`
Expected: build failure, `undefined: Socket`.

- [ ] **Step 3: Write `tree/internal/source/herdr.go`**

```go
// Package source reads the live Herdr state and the dispatch plugin's state files.
package source

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
				Repo string `json:"repo_name"`
			} `json:"worktree"`
		} `json:"workspaces"`
	}
	var tabs struct {
		Tabs []struct {
			ID        string `json:"tab_id"`
			Workspace string `json:"workspace_id"`
			Number    int    `json:"number"`
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
		} `json:"agents"`
	}
	var panes struct {
		Panes []struct {
			Pane   string             `json:"pane_id"`
			Tokens map[string]*string `json:"tokens"`
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
	for _, p := range panes.Panes {
		if v := p.Tokens["tree"]; v != nil && *v == "hide" {
			hide[p.Pane] = true
		}
	}
	var s model.Snapshot
	for _, w := range ws.Workspaces {
		m := model.Workspace{ID: w.ID, Label: w.Label, Number: w.Number, Status: w.Status}
		if w.Worktree != nil {
			m.Repo = w.Worktree.Repo
		}
		s.Workspaces = append(s.Workspaces, m)
	}
	for _, t := range tabs.Tabs {
		s.Tabs = append(s.Tabs, model.Tab{ID: t.ID, WorkspaceID: t.Workspace, Number: t.Number, Label: t.Label, Status: t.Status})
	}
	for _, a := range agents.Agents {
		s.Agents = append(s.Agents, model.Agent{
			PaneID: a.Pane, TabID: a.Tab, WorkspaceID: a.Workspace,
			Name: a.Name, Kind: a.Kind, Title: a.Title, Status: a.Status, Hide: hide[a.Pane],
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
```

The `model.Agent` field order in the struct literal of the test differs from `types.go`. That is fine: the test compares JSON, and both sides marshal the same type.

- [ ] **Step 4: Run the tests**

Run: `cd tree && go test ./internal/source/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd tree && gofmt -l . && go vet ./...
git add . && git commit -m "tree: read the Herdr snapshot over the socket and focus a row"
```

---

### Task 5: Dispatch state reader

**Files:**
- Create: `tree/internal/source/dispatch.go`
- Test: `tree/internal/source/dispatch_test.go`

**Interfaces:**
- Consumes: `model.Dispatch`, `model.Run`, `model.Entry`.
- Produces:
  - `func StateDir() string` - `<XDG_STATE_HOME or ~/.local/state>/agent-dispatch`
  - `func Dispatch(stateDir string) model.Dispatch` - never fails; a part that cannot be read is left out.

File layout it reads:

```
<stateDir>/panes.json                 {"<pane id>": {"role": "tracked" | "worker" | "supervisor", ...}}
<stateDir>/runs/<run>/supervisor      one line: the supervisor pane id
<stateDir>/runs/<run>/ledger.json     [{"pane_id","tab_id","workspace_id","worktree","created", ...}]
```

`tab_id` is JSON `null` for a worker placed as a workspace.

- [ ] **Step 1: Write the failing test `tree/internal/source/dispatch_test.go`**

```go
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
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd tree && go test ./internal/source/ -run 'Dispatch|StateDir'`
Expected: build failure, `undefined: Dispatch`.

- [ ] **Step 3: Write `tree/internal/source/dispatch.go`**

```go
package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// StateDir is where the dispatch plugin keeps its runs and pane index.
func StateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(base, "agent-dispatch")
}

// Dispatch reads every run and the pane index. A run or index that cannot be read is left out.
func Dispatch(stateDir string) model.Dispatch {
	d := model.Dispatch{Tracked: map[string]bool{}}

	var index map[string]struct {
		Role string `json:"role"`
	}
	if b, err := os.ReadFile(filepath.Join(stateDir, "panes.json")); err == nil && json.Unmarshal(b, &index) == nil {
		for pane, v := range index {
			if v.Role == "tracked" {
				d.Tracked[pane] = true
			}
		}
	}

	dirs, _ := os.ReadDir(filepath.Join(stateDir, "runs"))
	names := make([]string, 0, len(dirs))
	for _, e := range dirs {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		run := filepath.Join(stateDir, "runs", name)
		sup, err := os.ReadFile(filepath.Join(run, "supervisor"))
		if err != nil || strings.TrimSpace(string(sup)) == "" {
			continue
		}
		ledger, err := os.ReadFile(filepath.Join(run, "ledger.json"))
		if err != nil {
			continue
		}
		var entries []struct {
			Pane      string `json:"pane_id"`
			Tab       string `json:"tab_id"`
			Workspace string `json:"workspace_id"`
			Worktree  string `json:"worktree"`
			Created   string `json:"created"`
		}
		if json.Unmarshal(ledger, &entries) != nil {
			continue
		}
		r := model.Run{Name: name, Supervisor: strings.TrimSpace(string(sup))}
		for _, e := range entries {
			r.Entries = append(r.Entries, model.Entry{
				PaneID: e.Pane, TabID: e.Tab, WorkspaceID: e.Workspace, Worktree: e.Worktree, Created: e.Created,
			})
		}
		d.Runs = append(d.Runs, r)
	}
	return d
}
```

- [ ] **Step 4: Run the tests**

Run: `cd tree && go test ./internal/source/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd tree && gofmt -l . && go vet ./...
git add . && git commit -m "tree: read dispatch runs and the tracked pane index"
```

---

### Task 6: Rows, fold and the attention filter

**Files:**
- Create: `tree/internal/view/rows.go`
- Test: `tree/internal/view/rows_test.go`

**Interfaces:**
- Consumes: `model.Tree`, `model.Node`, `model.Kind*`, status constants.
- Produces:
  - `type Row struct { Node *model.Node; Depth int; HasChildren, Folded, Last bool; Trunk []bool; Rollup []string }`
  - `type State struct { Folded map[string]bool; Attention bool }`
  - `const GroupNoAgent = "group:no-agent"`, `const GroupHidden = "group:hidden"`
  - `func Rows(t model.Tree, st State) []Row`

Rules:

- `State.Folded[id]` is the stored fold for a node. With no stored value, a group is folded and every other node is open.
- A folded parent has `Rollup`: the state of each descendant, in tree order. A group has no rollup.
- The two groups are rows at depth 0 after the roots: `No agent (n)` and `Hidden (n)`. A group with no members has no row.
- With `Attention` on, a node is kept when its state is `blocked` or `done`, or when a descendant is kept. The groups are left out.
- `Last` is true for the last visible child of its parent. `Trunk` has one value for each ancestor below the root, top down: true when that ancestor has a later sibling, so a guide line runs past this row. Both are computed over the visible children, after the attention filter.

- [ ] **Step 1: Write the failing test `tree/internal/view/rows_test.go`**

```go
package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func node(id, status string, kids ...*model.Node) *model.Node {
	return &model.Node{ID: id, Label: id, Status: status, Shown: model.KindWorkspace, Children: kids}
}

func sample() model.Tree {
	return model.Tree{
		Roots: []*model.Node{
			node("lead", model.Working,
				node("a", model.Done),
				node("b", model.Idle, node("b1", model.Blocked))),
			node("solo", model.Idle),
		},
		NoAgent: []*model.Node{node("repo1", model.Unknown), node("repo2", model.Unknown)},
		Hidden:  []*model.Node{node("watch", model.Working)},
	}
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
+No agent (2) []
+Hidden (1) []
`)
}

func TestRowsFoldAndRollup(t *testing.T) {
	st := State{Folded: map[string]bool{"lead": true, GroupHidden: false}}
	expect(t, Rows(sample(), st), `
+lead [done idle blocked]
 solo []
+No agent (2) []
 Hidden (1) []
   watch []
`)
}

func TestRowsAttention(t *testing.T) {
	expect(t, Rows(sample(), State{Attention: true}), `
 lead []
   a []
   b []
     b1 []
`)
	quiet := model.Tree{Roots: []*model.Node{node("solo", model.Idle)}, Hidden: []*model.Node{node("w", model.Done)}}
	if rows := Rows(quiet, State{Attention: true}); len(rows) != 0 {
		t.Fatalf("nothing needs attention, got %s", show(rows))
	}
}

func TestRowsGuides(t *testing.T) {
	deep := model.Tree{Roots: []*model.Node{
		node("lead", model.Idle,
			node("a", model.Idle, node("a1", model.Idle)),
			node("b", model.Idle, node("b1", model.Idle))),
	}}
	got := map[string]Row{}
	for _, r := range Rows(deep, State{}) {
		got[r.Node.ID] = r
	}
	cases := []struct {
		id    string
		last  bool
		trunk []bool
	}{
		{"a", false, nil},
		{"b", true, nil},
		{"a1", true, []bool{true}},  // a has a later sibling: its guide line runs past a1
		{"b1", true, []bool{false}}, // b is the last child: nothing runs past b1
	}
	for _, c := range cases {
		r := got[c.id]
		if r.Last != c.last || fmt.Sprint(r.Trunk) != fmt.Sprint(c.trunk) {
			t.Errorf("%s: last=%v trunk=%v, want last=%v trunk=%v", c.id, r.Last, r.Trunk, c.last, c.trunk)
		}
	}
	// With the filter on, the last kept child is the last one.
	deep.Roots[0].Children[0].Children[0].Status = model.Blocked
	for _, r := range Rows(deep, State{Attention: true}) {
		if r.Node.ID == "a" && !r.Last {
			t.Errorf("a is the only kept child of lead and must be last")
		}
	}
}

func TestRowsEmptyGroupsAndLeafFold(t *testing.T) {
	only := model.Tree{Roots: []*model.Node{node("solo", model.Idle)}}
	// A stored fold on a node with no children must not mark it folded.
	expect(t, Rows(only, State{Folded: map[string]bool{"solo": true}}), `
 solo []
`)
}
```

- [ ] **Step 2: Run it and see it fail**

Run: `cd tree && go test ./internal/view/`
Expected: build failure, `undefined: Rows`.

- [ ] **Step 3: Write `tree/internal/view/rows.go`**

```go
// Package view turns the tree into rows, draws them, and runs the interactive pane.
package view

import (
	"fmt"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Row is one visible line of the tree.
type Row struct {
	Node        *model.Node
	Depth       int
	HasChildren bool
	Folded      bool
	// Last and Trunk place the guide lines: the branch to this row and the lines that run past it.
	Last   bool
	Trunk  []bool
	Rollup []string
}

// State is what the user has set: which rows are folded and whether the attention filter is on.
type State struct {
	Folded    map[string]bool
	Attention bool
}

const (
	GroupNoAgent = "group:no-agent"
	GroupHidden  = "group:hidden"
)

func (st State) folded(n *model.Node) bool {
	if v, ok := st.Folded[n.ID]; ok {
		return v
	}
	return n.Shown == model.KindGroup
}

// needs is true for a row the user should look at, or one that leads to such a row.
func needs(n *model.Node) bool {
	if n.Status == model.Blocked || n.Status == model.Done {
		return true
	}
	for _, c := range n.Children {
		if needs(c) {
			return true
		}
	}
	return false
}

func statuses(nodes []*model.Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Status)
		out = append(out, statuses(n.Children)...)
	}
	return out
}

func group(id, name string, members []*model.Node) *model.Node {
	return &model.Node{
		ID: id, Focus: model.KindGroup, Shown: model.KindGroup,
		Label: fmt.Sprintf("%s (%d)", name, len(members)), Children: members,
	}
}

// Rows flattens the tree to the lines on screen.
func Rows(t model.Tree, st State) []Row {
	var out []Row
	var walk func(n *model.Node, depth int, trunk []bool, last bool)
	walk = func(n *model.Node, depth int, trunk []bool, last bool) {
		kids := n.Children
		if st.Attention {
			kids = nil
			for _, c := range n.Children {
				if needs(c) {
					kids = append(kids, c)
				}
			}
		}
		r := Row{Node: n, Depth: depth, HasChildren: len(kids) > 0, Last: last, Trunk: trunk}
		r.Folded = r.HasChildren && st.folded(n)
		if r.Folded && n.Shown != model.KindGroup {
			r.Rollup = statuses(kids)
		}
		out = append(out, r)
		if !r.Folded {
			below := trunk
			if depth > 0 {
				below = append(append([]bool(nil), trunk...), !last)
			}
			for i, c := range kids {
				walk(c, depth+1, below, i == len(kids)-1)
			}
		}
	}
	for _, n := range t.Roots {
		if !st.Attention || needs(n) {
			walk(n, 0, nil, true)
		}
	}
	if st.Attention {
		return out
	}
	if len(t.NoAgent) > 0 {
		walk(group(GroupNoAgent, "No agent", t.NoAgent), 0, nil, true)
	}
	if len(t.Hidden) > 0 {
		walk(group(GroupHidden, "Hidden", t.Hidden), 0, nil, true)
	}
	return out
}
```

- [ ] **Step 4: Run the tests**

Run: `cd tree && go test ./internal/view/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd tree && gofmt -l . && go vet ./...
git add . && git commit -m "tree: flatten the tree to rows with fold, rollup and attention"
```

---

### Task 7: Rendering and the theme

**Files:**
- Create: `tree/internal/view/theme.go`, `tree/internal/view/render.go`
- Test: `tree/internal/view/render_test.go`, `tree/internal/view/theme_test.go`

**Interfaces:**
- Consumes: `Row` from Task 6, `model.Node`, `model.Kind*`, status constants.
- Produces:
  - `type Theme struct { Plain bool; Colors map[string]string; TabGlyph, PaneGlyph string }`
  - `func DefaultTheme() Theme`
  - `func LoadTheme(configDir string) Theme` - `DefaultTheme` with overrides from `<configDir>/config.toml`
  - `func Render(rows []Row, width, selected int, th Theme) []string` - one string per row

Layout of one line:

```
<margin><lead><dot> <label><glyph><pad><right>  <rollup>
```

- `margin`: one cell. The selected row has `▌` (`>` when `Plain`), others a space.
- `lead` at depth 0: the fold mark and a space. The fold mark is `▾` open with children, `▸` folded, a space with no children.
- `lead` below depth 0 draws the guide lines, dim: two spaces, then for each value in `Row.Trunk` either `│` and two spaces or three spaces, then the branch `├` (or `└` when `Row.Last`), then `─`, or `▸` when the row is folded, then a space. An open parent below a root has no mark: its children are in view. A child's branch sits under its parent's dot.
- `dot`: `●` in the state color, `·` for `unknown`. When `Plain`: `!` blocked, `*` working, `+` done, `o` idle, `·` unknown. A group row has no dot and no space after it.
- `glyph`: at depth 1 or more, a space and the tab glyph for `Shown == KindTab`, the pane glyph for `KindPane`. None at depth 0.
- `right`: the ticket at depth 0, the repository below. None for a group.
- `pad`: spaces up to the right column. The right column is two cells after the widest left part, moved left when the pane is too narrow, and never left of cell 8.
- A label that does not fit is cut and ends in `…`.
- `rollup`: for a folded row, one dot per state, separated by spaces, after two spaces.
- No line is wider than `width`. Trailing spaces are removed.

- [ ] **Step 1: Add the dependencies**

```bash
cd tree
go get github.com/charmbracelet/bubbletea@v1 github.com/charmbracelet/lipgloss@v1 github.com/BurntSushi/toml@latest
```

- [ ] **Step 2: Write the failing tests**

`tree/internal/view/render_test.go`:

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
	th.Plain, th.TabGlyph, th.PaneGlyph = true, "⇥", "›"
	return th
}

func sp(n int) string { return strings.Repeat(" ", n) }

func same(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRenderOpenTree(t *testing.T) {
	review := &model.Node{ID: "r", Label: "review-86", Status: model.Done, Repo: "iam", Shown: model.KindWorkspace}
	build := &model.Node{ID: "b", Label: "build", Status: model.Idle, Repo: "api", Shown: model.KindTab}
	ops := &model.Node{ID: "o", Label: "ops", Status: model.Working, Ticket: "ABC-1", Shown: model.KindWorkspace,
		Children: []*model.Node{review, build}}
	tree := model.Tree{Roots: []*model.Node{ops}, NoAgent: []*model.Node{{ID: "x", Label: "x"}, {ID: "y", Label: "y"}}}
	got := Render(Rows(tree, State{}), 40, 1, plain())
	same(t, got, []string{
		" ▾ * ops" + sp(11) + "ABC-1",
		">  ├─ + review-86" + sp(2) + "iam",
		"   └─ o build ⇥" + sp(4) + "api",
		" ▸ No agent (2)",
	})
}

func TestRenderFoldedParentShowsDescendantDots(t *testing.T) {
	ops := &model.Node{ID: "o", Label: "ops", Status: model.Working, Ticket: "ABC-1", Shown: model.KindWorkspace,
		Children: []*model.Node{
			{ID: "a", Label: "a", Status: model.Done},
			{ID: "b", Label: "b", Status: model.Blocked},
		}}
	rows := Rows(model.Tree{Roots: []*model.Node{ops}}, State{Folded: map[string]bool{"o": true}})
	same(t, Render(rows, 40, -1, plain()), []string{" ▸ * ops" + sp(2) + "ABC-1" + sp(2) + "+ !"})
}

func TestRenderPaneGlyphAndUnknownDot(t *testing.T) {
	tab := &model.Node{ID: "t", Label: "planning", Status: model.Unknown, Shown: model.KindTab, Children: []*model.Node{
		{ID: "p", Label: "left", Status: model.Idle, Repo: "ops", Shown: model.KindPane}}}
	got := Render(Rows(model.Tree{Roots: []*model.Node{tab}}, State{}), 40, -1, plain())
	same(t, got, []string{
		" ▾ · planning",
		"   └─ o left ›" + sp(2) + "ops",
	})
}

func TestRenderGuideLines(t *testing.T) {
	n := func(id string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Shown: model.KindWorkspace, Children: kids}
	}
	tree := model.Tree{Roots: []*model.Node{n("lead", n("a", n("a1"), n("a2")), n("b", n("b1")))}}
	rows := Rows(tree, State{Folded: map[string]bool{"b": true}})
	same(t, Render(rows, 40, -1, plain()), []string{
		" ▾ o lead",
		"   ├─ o a",
		"   │  ├─ o a1",
		"   │  └─ o a2",
		"   └▸ o b" + sp(8) + "o",
	})
}

func TestRenderNarrowAndWide(t *testing.T) {
	long := &model.Node{ID: "l", Label: "a-very-long-workspace-label-that-does-not-fit", Status: model.Idle, Ticket: "ABC-12345",
		Children: []*model.Node{{ID: "c", Label: "日本語のラベルは幅が広い", Status: model.Done, Repo: "repository"}}}
	rows := Rows(model.Tree{Roots: []*model.Node{long}}, State{})
	for _, width := range []int{0, 1, 5, 10, 24, 60} {
		for _, th := range []Theme{plain(), DefaultTheme()} {
			for _, line := range Render(rows, width, 0, th) {
				if w := lipgloss.Width(line); width > 0 && w > width {
					t.Errorf("width %d plain=%v: line is %d cells: %q", width, th.Plain, w, line)
				}
			}
		}
	}
	// At a width that fits the right column, a cut label ends in an ellipsis and the column still lines up.
	got := Render(rows, 30, -1, plain())
	if !strings.Contains(got[0], "…") || !strings.HasSuffix(got[0], "ABC-12345") {
		t.Errorf("root line = %q", got[0])
	}
	if !strings.Contains(got[1], "…") || !strings.HasSuffix(got[1], "repository") {
		t.Errorf("child line = %q", got[1])
	}
	a, b := strings.Index(got[0], "ABC-12345"), strings.Index(got[1], "repository")
	if lipgloss.Width(got[0][:a]) != lipgloss.Width(got[1][:b]) {
		t.Errorf("right column is not aligned:\n%q\n%q", got[0], got[1])
	}
}

func TestRenderNoRows(t *testing.T) {
	if got := Render(nil, 40, 0, plain()); len(got) != 0 {
		t.Fatalf("no rows, got %q", got)
	}
}
```

`tree/internal/view/theme_test.go`:

```go
package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func TestLoadTheme(t *testing.T) {
	def := DefaultTheme()
	if got := LoadTheme(filepath.Join(t.TempDir(), "absent")); got.TabGlyph != def.TabGlyph || got.Colors[model.Blocked] != def.Colors[model.Blocked] {
		t.Fatalf("no config must give the default theme, got %+v", got)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`
[glyphs]
tab = "⇥"
[colors]
blocked = "#ff0000"
`), 0o644)
	got := LoadTheme(dir)
	if got.TabGlyph != "⇥" || got.PaneGlyph != def.PaneGlyph {
		t.Errorf("glyphs = %q %q", got.TabGlyph, got.PaneGlyph)
	}
	if got.Colors[model.Blocked] != "#ff0000" || got.Colors[model.Done] != def.Colors[model.Done] {
		t.Errorf("colors = %v", got.Colors)
	}
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`not = [toml`), 0o644)
	if got := LoadTheme(dir); got.TabGlyph != def.TabGlyph {
		t.Errorf("a config that does not parse must give the default theme, got %+v", got)
	}
}
```

- [ ] **Step 3: Run them and see them fail**

Run: `cd tree && go test ./internal/view/`
Expected: build failure, `undefined: DefaultTheme`.

- [ ] **Step 4: Write `tree/internal/view/theme.go`**

The colors are Catppuccin Mocha values. Only `unknown` was read from a real Herdr sidebar; the user checks the other four by eye in Task 9 and can override them in the config file.

```go
package view

import (
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Theme is how states and row kinds are drawn. Plain draws with no color, for tests and scripts.
type Theme struct {
	Plain     bool
	Colors    map[string]string
	TabGlyph  string
	PaneGlyph string
}

// DefaultTheme uses Nerd Font glyphs for tab and pane rows.
func DefaultTheme() Theme {
	return Theme{
		Colors: map[string]string{
			model.Blocked: "#f38ba8",
			model.Working: "#f9e2af",
			model.Done:    "#a6e3a1",
			model.Idle:    "#89b4fa",
			model.Unknown: "#6c7086",
		},
		TabGlyph:  "\U000f04e9",
		PaneGlyph: "",
	}
}

// LoadTheme applies the [glyphs] and [colors] tables of <configDir>/config.toml to the default theme.
func LoadTheme(configDir string) Theme {
	th := DefaultTheme()
	var file struct {
		Glyphs struct {
			Tab  string `toml:"tab"`
			Pane string `toml:"pane"`
		} `toml:"glyphs"`
		Colors map[string]string `toml:"colors"`
	}
	if _, err := toml.DecodeFile(filepath.Join(configDir, "config.toml"), &file); err != nil {
		return th
	}
	if file.Glyphs.Tab != "" {
		th.TabGlyph = file.Glyphs.Tab
	}
	if file.Glyphs.Pane != "" {
		th.PaneGlyph = file.Glyphs.Pane
	}
	for state, color := range file.Colors {
		if _, known := th.Colors[state]; known && color != "" {
			th.Colors[state] = color
		}
	}
	return th
}

var plainDots = map[string]string{
	model.Blocked: "!", model.Working: "*", model.Done: "+", model.Idle: "o",
}

func (th Theme) dot(status string) string {
	if _, known := th.Colors[status]; !known {
		status = model.Unknown
	}
	if th.Plain {
		if g, ok := plainDots[status]; ok {
			return g
		}
		return "·"
	}
	g := "●"
	if status == model.Unknown {
		g = "·"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(th.Colors[status])).Render(g)
}

func (th Theme) paint(s string, st lipgloss.Style) string {
	if th.Plain || s == "" {
		return s
	}
	return st.Render(s)
}
```

- [ ] **Step 5: Write `tree/internal/view/render.go`**

```go
package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

const minRightColumn = 8

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
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

// Render draws one line per row. selected is the index of the row under the cursor, or -1.
func Render(rows []Row, width, selected int, th Theme) []string {
	type parts struct {
		lead, label, glyph, right string
		dot                       bool
	}
	ps := make([]parts, len(rows))
	maxLeft, maxRight := 0, 0
	for i, r := range rows {
		n := r.Node
		p := parts{lead: lead(r), label: n.Label, dot: n.Shown != model.KindGroup}
		if p.dot {
			if r.Depth == 0 {
				p.right = n.Ticket
			} else {
				p.right = n.Repo
				switch n.Shown {
				case model.KindTab:
					p.glyph = " " + th.TabGlyph
				case model.KindPane:
					p.glyph = " " + th.PaneGlyph
				}
			}
		}
		ps[i] = p
		maxLeft = max(maxLeft, lipgloss.Width(p.lead)+dotCells(p.dot)+lipgloss.Width(p.label)+lipgloss.Width(p.glyph))
		maxRight = max(maxRight, lipgloss.Width(p.right))
	}

	// The right column starts two cells after the widest left part, or earlier in a narrow pane.
	col := maxLeft + 2
	if width > 0 {
		col = min(col, width-1-maxRight-1)
	}
	col = max(col, minRightColumn)

	lines := make([]string, len(rows))
	for i, r := range rows {
		p := ps[i]
		label := cut(p.label, col-2-lipgloss.Width(p.lead)-dotCells(p.dot)-lipgloss.Width(p.glyph))
		used := lipgloss.Width(p.lead) + dotCells(p.dot) + lipgloss.Width(label) + lipgloss.Width(p.glyph)

		var b strings.Builder
		switch {
		case i != selected:
			b.WriteString(" ")
		case th.Plain:
			b.WriteString(">")
		default:
			b.WriteString(bold.Render("▌"))
		}
		b.WriteString(th.paint(p.lead, faint))
		if p.dot {
			b.WriteString(th.dot(r.Node.Status) + " ")
		}
		if r.Depth == 0 && p.dot {
			b.WriteString(th.paint(label, bold))
		} else {
			b.WriteString(label)
		}
		b.WriteString(th.paint(p.glyph, faint))
		if p.right != "" || len(r.Rollup) > 0 {
			b.WriteString(strings.Repeat(" ", max(1, col-used)))
			b.WriteString(th.paint(p.right, faint))
		}
		if len(r.Rollup) > 0 {
			dots := make([]string, len(r.Rollup))
			for j, s := range r.Rollup {
				dots[j] = th.dot(s)
			}
			b.WriteString("  " + strings.Join(dots, " "))
		}
		line := strings.TrimRight(b.String(), " ")
		if width > 0 {
			line = lipgloss.NewStyle().MaxWidth(width).Render(line)
		}
		lines[i] = strings.TrimRight(line, " ")
	}
	return lines
}

// lead is the fold mark of a root, or the guide lines and branch of a row below one.
func lead(r Row) string {
	mark := ""
	if r.HasChildren {
		mark = "▾"
		if r.Folded {
			mark = "▸"
		}
	}
	if r.Depth == 0 {
		if mark == "" {
			mark = " "
		}
		return mark + " "
	}
	var b strings.Builder
	b.WriteString("  ")
	for _, runs := range r.Trunk {
		if runs {
			b.WriteString("│  ")
		} else {
			b.WriteString("   ")
		}
	}
	if r.Last {
		b.WriteString("└")
	} else {
		b.WriteString("├")
	}
	// Below a root only a folded row is marked: an open parent has its children in view.
	if r.Folded {
		return b.String() + "▸ "
	}
	return b.String() + "─ "
}

func dotCells(dot bool) int {
	if dot {
		return 2
	}
	return 0
}
```

`min` and `max` are Go 1.21 builtins. If `go.mod` names an older Go version after `go get`, run `go mod edit -go=1.22`.

- [ ] **Step 6: Run the tests**

Run: `cd tree && go test ./internal/view/`
Expected: `ok`.

If `TestRenderFoldedParentShowsDescendantDots` fails on spacing, check the pad: the left part `▸ * ops` is 7 cells, the right column is at 9, so the pad is 2.

In `TestRenderGuideLines` the widest left part is `  │  ├─ o a1` at 12 cells, so the right column is at 14. The folded row `  └▸ o b` is 8 cells, the pad is 6, the right column is empty, and the rollup follows two spaces later: 8 spaces in all before its dot.

If `TestRenderNarrowAndWide` fails at width 0 with a panic, `cut` is being called with a negative width somewhere it indexes; `cut` must return `""` for `n <= 0`.

- [ ] **Step 7: Commit**

```bash
cd tree && go mod tidy && gofmt -l . && go vet ./...
git add . && git commit -m "tree: render rows with state dots, kind glyphs and a right column"
```

---

### Task 8: The interactive pane and the binary

**Files:**
- Create: `tree/internal/view/folds.go`, `tree/internal/view/program.go`, `tree/cmd/tree/main.go`
- Test: `tree/internal/view/folds_test.go`, `tree/internal/view/program_test.go`

**Interfaces:**
- Consumes: `Rows`, `State`, `Row`, `Render`, `Theme`, `LoadTheme` (Tasks 6, 7); `source.Socket`, `source.Snapshot`, `source.Dispatch`, `source.StateDir`, `source.Focus` (Tasks 4, 5); `model.Build` (Task 3).
- Produces:
  - `func LoadFolds(dir string) map[string]bool`
  - `func SaveFolds(dir string, folds map[string]bool, t model.Tree) error`
  - `type Deps struct { Load func() (model.Tree, error); Focus func(*model.Node) error; Save func(map[string]bool, model.Tree); OriginPane, OriginTab string }`
  - `func New(d Deps, folds map[string]bool, th Theme) Program` - `Program` implements `tea.Model`
  - binary `tree` with flag `--once`

Behavior:

- Keys: `up`/`k`, `down`/`j` move. `enter` focuses the row and quits; on a group it folds or unfolds. `space` folds or unfolds a row with children. `a` switches the attention filter. `q`, `esc`, `ctrl+c` quit.
- The tree reloads once a second. The cursor stays on the same node by id. When that node is gone, the cursor keeps its position, limited to the last row.
- On the first load the cursor goes to the row of `OriginPane`, else the first row in `OriginTab`.
- A focus error (the agent is gone) is shown as one line, the tree reloads, and the pane stays open.
- A load error is shown as one line and the last good tree stays on screen.
- Folds are saved after each change. Ids that are not in the tree are removed on save. The two group ids are always kept.

- [ ] **Step 1: Write the failing tests**

`tree/internal/view/folds_test.go`:

```go
package view

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func TestFoldsRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if got := LoadFolds(dir); len(got) != 0 {
		t.Fatalf("no file must read as no folds, got %v", got)
	}
	tree := model.Tree{Roots: []*model.Node{{ID: "w1:p1", Children: []*model.Node{{ID: "w2:p1"}}}}}
	folds := map[string]bool{"w1:p1": true, "w2:p1": false, "w9:p9": true, GroupHidden: false}
	if err := SaveFolds(dir, folds, tree); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"w1:p1": true, "w2:p1": false, GroupHidden: false}
	if got := LoadFolds(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("folds = %v, want %v (the id of a node that is gone is removed)", got, want)
	}
}
```

`tree/internal/view/program_test.go`:

```go
package view

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

type fake struct {
	tree     model.Tree
	focused  []string
	focusErr error
	saved    map[string]bool
}

func (f *fake) deps() Deps {
	return Deps{
		Load:  func() (model.Tree, error) { return f.tree, nil },
		Focus: func(n *model.Node) error { f.focused = append(f.focused, n.ID); return f.focusErr },
		Save:  func(folds map[string]bool, _ model.Tree) { f.saved = folds },
	}
}

func start(t *testing.T, f *fake, d Deps) Program {
	t.Helper()
	p := New(d, nil, plain())
	return send(t, p, loadedMsg{tree: f.tree})
}

func send(t *testing.T, p Program, msgs ...tea.Msg) Program {
	t.Helper()
	for _, m := range msgs {
		next, _ := p.Update(m)
		p = next.(Program)
	}
	return p
}

func key(s string) tea.Msg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func selected(p Program) string { return p.rows[p.cursor].Node.ID }

func TestProgramMoveAndFocus(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	if selected(p) != "lead" {
		t.Fatalf("starts on %q", selected(p))
	}
	p = send(t, p, key("up"))
	if selected(p) != "lead" {
		t.Fatalf("up at the top stays, got %q", selected(p))
	}
	p = send(t, p, key("down"), key("j"))
	if selected(p) != "b" {
		t.Fatalf("selected %q, want b", selected(p))
	}
	_, cmd := p.Update(key("enter"))
	msg := cmd()
	if len(f.focused) != 1 || f.focused[0] != "b" {
		t.Fatalf("focused %v, want [b]", f.focused)
	}
	if _, quit := p.Update(msg); quit == nil || quit() != tea.Quit() {
		t.Fatal("a good focus closes the pane")
	}
}

func TestProgramFocusErrorStaysOpen(t *testing.T) {
	f := &fake{tree: sample(), focusErr: errors.New("pane_not_found: gone")}
	p := start(t, f, f.deps())
	_, cmd := p.Update(key("enter"))
	next, reload := p.Update(cmd())
	p = next.(Program)
	if reload == nil {
		t.Fatal("a failed focus reloads the tree")
	}
	if _, ok := reload().(loadedMsg); !ok {
		t.Fatal("a failed focus must not close the pane")
	}
	if !strings.Contains(p.View(), "pane_not_found") {
		t.Errorf("the error is shown:\n%s", p.View())
	}
}

func TestProgramFoldSavesAndGroupsUnfoldOnEnter(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("space"))
	if len(p.rows) != 4 || !p.rows[0].Folded {
		t.Fatalf("space folds the selected parent, rows:\n%s", show(p.rows))
	}
	if !f.saved["lead"] {
		t.Fatalf("the fold is saved, got %v", f.saved)
	}
	p = send(t, p, key("down"), key("down"))
	if selected(p) != GroupNoAgent {
		t.Fatalf("selected %q", selected(p))
	}
	p = send(t, p, key("enter"))
	if len(f.focused) != 0 {
		t.Fatal("enter on a group must not focus anything")
	}
	if p.rows[p.cursor].Folded {
		t.Fatalf("enter on a group unfolds it, rows:\n%s", show(p.rows))
	}
	p = send(t, p, key("space"))
	before := len(p.rows)
	p = send(t, p, key("up"), key("space"))
	if selected(p) != "solo" || len(p.rows) != before {
		t.Fatalf("space on a row with no children does nothing, rows:\n%s", show(p.rows))
	}
}

func TestProgramAttentionAndQuit(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("a"))
	if len(p.rows) != 4 {
		t.Fatalf("attention rows:\n%s", show(p.rows))
	}
	p = send(t, p, key("a"))
	if len(p.rows) != 7 {
		t.Fatalf("attention off rows:\n%s", show(p.rows))
	}
	for _, k := range []string{"q", "esc"} {
		if _, cmd := p.Update(key(k)); cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%s must quit", k)
		}
	}
}

func TestProgramKeepsSelectionAcrossReload(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("down"), key("down"), key("down"), key("down"))
	if selected(p) != "solo" {
		t.Fatalf("selected %q", selected(p))
	}
	// A worker goes away: the cursor follows the same node to its new line.
	shorter := sample()
	shorter.Roots[0].Children = shorter.Roots[0].Children[:1]
	p = send(t, p, loadedMsg{tree: shorter})
	if selected(p) != "solo" {
		t.Fatalf("after reload selected %q, want solo", selected(p))
	}
	// The selected node and everything after it go away: the cursor lands on the last row.
	p = send(t, p, loadedMsg{tree: model.Tree{Roots: []*model.Node{node("lead", model.Idle)}}})
	if p.cursor != 0 || selected(p) != "lead" {
		t.Fatalf("cursor %d on %q, want the last row", p.cursor, selected(p))
	}
	// Nothing left at all: no panic on any key.
	p = send(t, p, loadedMsg{tree: model.Tree{}}, key("down"), key("space"), key("enter"))
	if !strings.Contains(p.View(), "no agents") {
		t.Errorf("empty view:\n%s", p.View())
	}
}

func TestProgramStartsOnOrigin(t *testing.T) {
	f := &fake{tree: sample()}
	f.tree.Roots[0].Children[1].TabID = "w5:t2"
	d := f.deps()
	d.OriginPane = "b1"
	if p := start(t, f, d); selected(p) != "b1" {
		t.Fatalf("starts on %q, want the origin pane", selected(p))
	}
	d.OriginPane, d.OriginTab = "w5:p9", "w5:t2"
	if p := start(t, f, d); selected(p) != "b" {
		t.Fatalf("starts on %q, want the first row of the origin tab", selected(p))
	}
}

func TestProgramLoadErrorKeepsTree(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, loadedMsg{err: errors.New("dial unix: no such file")})
	if len(p.rows) != 7 || !strings.Contains(p.View(), "no such file") {
		t.Fatalf("a load error keeps the last tree and shows the error:\n%s", p.View())
	}
}

func TestProgramScrolls(t *testing.T) {
	var roots []*model.Node
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		roots = append(roots, node(id, model.Idle))
	}
	f := &fake{tree: model.Tree{Roots: roots}}
	p := start(t, f, f.deps())
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 8})
	for i := 0; i < 9; i++ {
		p = send(t, p, key("down"))
	}
	view := p.View()
	if !strings.Contains(view, "> ") || !strings.Contains(view, " j") || strings.Contains(view, " a\n") {
		t.Fatalf("the view follows the cursor to the last row:\n%s", view)
	}
	if n := strings.Count(view, "\n"); n > 8 {
		t.Fatalf("the view is %d lines in a pane of 8", n)
	}
}
```

- [ ] **Step 2: Run them and see them fail**

Run: `cd tree && go test ./internal/view/`
Expected: build failure, `undefined: LoadFolds`.

- [ ] **Step 3: Write `tree/internal/view/folds.go`**

```go
package view

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

const foldsFile = "folds.json"

// LoadFolds reads the fold of each row the user has set. A missing or broken file is no folds.
func LoadFolds(dir string) map[string]bool {
	folds := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(dir, foldsFile)); err == nil {
		json.Unmarshal(b, &folds)
	}
	if folds == nil {
		folds = map[string]bool{}
	}
	return folds
}

// SaveFolds writes the folds of the rows that are still in the tree.
func SaveFolds(dir string, folds map[string]bool, t model.Tree) error {
	keep := map[string]bool{GroupNoAgent: true, GroupHidden: true}
	var mark func(nodes []*model.Node)
	mark = func(nodes []*model.Node) {
		for _, n := range nodes {
			keep[n.ID] = true
			mark(n.Children)
		}
	}
	mark(t.Roots)
	mark(t.NoAgent)
	mark(t.Hidden)
	out := map[string]bool{}
	for id, v := range folds {
		if keep[id] {
			out[id] = v
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, foldsFile), b, 0o644)
}
```

- [ ] **Step 4: Write `tree/internal/view/program.go`**

```go
package view

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Deps is what the pane needs from outside: the tree, the jump, and where folds are kept.
type Deps struct {
	Load       func() (model.Tree, error)
	Focus      func(*model.Node) error
	Save       func(folds map[string]bool, t model.Tree)
	OriginPane string
	OriginTab  string
}

type (
	loadedMsg struct {
		tree model.Tree
		err  error
	}
	focusedMsg struct{ err error }
	tickMsg    struct{}
)

// Lines of the view that are not tree rows: title, blank, footer.
const chrome = 3

type Program struct {
	deps   Deps
	theme  Theme
	state  State
	tree   model.Tree
	rows   []Row
	sel    string
	cursor int
	offset int
	width  int
	height int
	placed bool
	err    error
}

func New(d Deps, folds map[string]bool, th Theme) Program {
	if folds == nil {
		folds = map[string]bool{}
	}
	return Program{deps: d, theme: th, state: State{Folded: folds}, width: 80, height: 24}
}

func (p Program) Init() tea.Cmd { return tea.Batch(p.load(), tick()) }

func (p Program) load() tea.Cmd {
	return func() tea.Msg {
		t, err := p.deps.Load()
		return loadedMsg{tree: t, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (p Program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
		p.scroll()
	case tickMsg:
		return p, tea.Batch(p.load(), tick())
	case loadedMsg:
		p.err = m.err
		if m.err == nil {
			p.tree = m.tree
			p.rebuild()
			if !p.placed {
				p.placed = true
				p.place()
			}
		}
	case focusedMsg:
		if m.err == nil {
			return p, tea.Quit
		}
		p.err = m.err
		return p, p.load()
	case tea.KeyMsg:
		switch m.String() {
		case "q", "esc", "ctrl+c":
			return p, tea.Quit
		case "up", "k":
			p.move(-1)
		case "down", "j":
			p.move(1)
		case " ":
			p.toggle()
		case "a":
			p.state.Attention = !p.state.Attention
			p.rebuild()
		case "enter":
			if len(p.rows) == 0 {
				break
			}
			n := p.rows[p.cursor].Node
			if n.Shown == model.KindGroup {
				p.toggle()
				break
			}
			focus := p.deps.Focus
			return p, func() tea.Msg { return focusedMsg{err: focus(n)} }
		}
	}
	return p, nil
}

// rebuild recomputes the rows and keeps the cursor on the node it was on.
func (p *Program) rebuild() {
	p.rows = Rows(p.tree, p.state)
	for i, r := range p.rows {
		if r.Node.ID == p.sel {
			p.cursor = i
			break
		}
	}
	p.settle()
}

// settle keeps the cursor on a real row and the view on the cursor.
func (p *Program) settle() {
	p.cursor = max(0, min(p.cursor, len(p.rows)-1))
	p.sel = ""
	if len(p.rows) > 0 {
		p.sel = p.rows[p.cursor].Node.ID
	}
	p.scroll()
}

func (p *Program) scroll() {
	body := max(1, p.height-chrome)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+body {
		p.offset = p.cursor - body + 1
	}
	p.offset = max(0, min(p.offset, max(0, len(p.rows)-body)))
}

func (p *Program) move(by int) {
	p.cursor += by
	p.settle()
}

// place puts the cursor on the agent the pane was opened from, else on that agent's tab.
func (p *Program) place() {
	for i, r := range p.rows {
		if r.Node.ID == p.deps.OriginPane {
			p.cursor = i
			p.settle()
			return
		}
	}
	for i, r := range p.rows {
		if p.deps.OriginTab != "" && r.Node.TabID == p.deps.OriginTab {
			p.cursor = i
			p.settle()
			return
		}
	}
}

func (p *Program) toggle() {
	if len(p.rows) == 0 || !p.rows[p.cursor].HasChildren {
		return
	}
	r := p.rows[p.cursor]
	p.state.Folded[r.Node.ID] = !r.Folded
	p.rebuild()
	if p.deps.Save != nil {
		p.deps.Save(p.state.Folded, p.tree)
	}
}

func (p Program) View() string {
	var b strings.Builder
	title := " Agents by supervisor"
	if p.state.Attention {
		title += "  (attention)"
	}
	b.WriteString(p.theme.paint(title, bold) + "\n\n")
	if len(p.rows) == 0 {
		b.WriteString("   no agents\n")
	}
	lines := Render(p.rows, p.width, p.cursor, p.theme)
	body := max(1, p.height-chrome)
	for _, line := range lines[min(p.offset, len(lines)):min(len(lines), p.offset+body)] {
		b.WriteString(line + "\n")
	}
	foot := " ↑↓ move  ⏎ jump  ␣ fold  a attention  q close"
	if p.err != nil {
		foot = " error: " + p.err.Error()
	}
	b.WriteString(p.theme.paint(cut(foot, p.width), faint))
	return b.String()
}
```

- [ ] **Step 5: Run the tests**

Run: `cd tree && go test ./internal/view/`
Expected: `ok`.

If `TestProgramMoveAndFocus` fails on `quit() != tea.Quit()`: `tea.Quit` is a function that returns `tea.QuitMsg{}`; compare the returned message with `tea.Quit()` as the test does, or with `tea.QuitMsg{}`.

- [ ] **Step 6: Write `tree/cmd/tree/main.go`**

```go
// Command tree shows Herdr agents as a tree by supervisor.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mago0/herdr-plugins/tree/internal/model"
	"github.com/mago0/herdr-plugins/tree/internal/source"
	"github.com/mago0/herdr-plugins/tree/internal/view"
)

const onceWidth = 100

func main() {
	once := flag.Bool("once", false, "print the tree as plain text and exit")
	flag.Parse()

	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		fmt.Fprintln(os.Stderr, "tree: not in a Herdr pane (HERDR_SOCKET_PATH is not set)")
		os.Exit(1)
	}
	sock := source.Socket{Path: path}
	load := func() (model.Tree, error) {
		snap, err := source.Snapshot(sock)
		if err != nil {
			return model.Tree{}, err
		}
		return model.Build(snap, source.Dispatch(source.StateDir())), nil
	}
	theme := view.LoadTheme(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"))
	stateDir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	folds := map[string]bool{}
	if stateDir != "" {
		folds = view.LoadFolds(stateDir)
	}

	if *once {
		t, err := load()
		if err != nil {
			fmt.Fprintln(os.Stderr, "tree:", err)
			os.Exit(1)
		}
		theme.Plain = true
		for _, line := range view.Render(view.Rows(t, view.State{Folded: folds}), onceWidth, -1, theme) {
			fmt.Println(line)
		}
		return
	}

	// The pane and tab the tree was opened from, so the cursor starts there.
	var origin struct {
		Pane string `json:"focused_pane_id"`
		Tab  string `json:"tab_id"`
	}
	json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &origin)

	deps := view.Deps{
		Load:       load,
		Focus:      func(n *model.Node) error { return source.Focus(sock, n) },
		OriginPane: origin.Pane,
		OriginTab:  origin.Tab,
	}
	if stateDir != "" {
		deps.Save = func(f map[string]bool, t model.Tree) { view.SaveFolds(stateDir, f, t) }
	}
	if _, err := tea.NewProgram(view.New(deps, folds, theme), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tree:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 7: Build and run it once against the live session**

```bash
cd tree && go build -o bin/tree ./cmd/tree && ./bin/tree --once
env -u HERDR_SOCKET_PATH ./bin/tree --once; echo "exit $?"
```

Expected: the first command prints the tree of the live Herdr session as plain text, with `No agent (n)` and, when dispatch has tracked loops, `Hidden (n)` at the end. The second prints `tree: not in a Herdr pane (HERDR_SOCKET_PATH is not set)` and `exit 1`.

- [ ] **Step 8: Commit**

```bash
cd tree && go mod tidy && gofmt -l . && go vet ./... && go test ./...
git add . && git commit -m "tree: interactive pane, saved folds and the tree binary"
```

---

### Task 9: Smoke test, docs, and a check in a real pane

**Files:**
- Create: `tree/test/smoke.sh`, `tree/README.md`
- Modify: `README.md` (repository root, the plugin table), `dispatch/README.md` (new section at the end)

**Interfaces:**
- Consumes: the `tree` binary from Task 8.

- [ ] **Step 1: Write `tree/test/smoke.sh`**

It starts a Herdr server that shares nothing with the user's sessions, and checks that `tree --once` reads it over the socket. A workspace with only a shell has no agent, so the expected output is the `No agent` line.

```bash
#!/usr/bin/env bash
# Smoke test: the tree binary against an isolated Herdr session. Needs herdr, tmux and go.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d /tmp/tree-smoke.XXXXXX)"
tmux_socket="tree-smoke-$$"

# Run a command against the isolated session only.
iso() {
  env -u HERDR_SOCKET_PATH -u HERDR_ENV -u HERDR_PANE_ID -u HERDR_TAB_ID -u HERDR_WORKSPACE_ID \
    -u HERDR_BIN_PATH -u HERDR_CONFIG_PATH -u TMUX XDG_CONFIG_HOME="$work/cfg" XDG_STATE_HOME="$work/state" "$@"
}

cleanup() {
  iso herdr server stop >/dev/null 2>&1 || true
  tmux -L "$tmux_socket" kill-server >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

mkdir -p "$work/cfg/herdr" "$work/state" "$work/a" "$work/b"
(cd "$here" && go build -o bin/tree ./cmd/tree)

iso tmux -L "$tmux_socket" new-session -d -s smoke -x 120 -y 30 -c "$work/a" herdr
sock="$work/cfg/herdr/herdr.sock"
for _ in $(seq 1 50); do [ -S "$sock" ] && break; sleep 0.1; done
[ -S "$sock" ] || { echo "FAIL: the isolated Herdr server did not start"; exit 1; }
[ "$(iso herdr plugin list | head -1)" = "No plugins installed." ] || { echo "FAIL: the session is not isolated"; exit 1; }

iso herdr workspace create --cwd "$work/b" --label second --no-focus >/dev/null

out="$(iso env HERDR_SOCKET_PATH="$sock" "$here/bin/tree" --once)"
echo "$out"
[ "$out" = " ▸ No agent (2)" ] || { echo "FAIL: expected one 'No agent (2)' line"; exit 1; }

if iso "$here/bin/tree" --once >/dev/null 2>&1; then
  echo "FAIL: tree must exit non-zero outside Herdr"; exit 1
fi
echo "PASS"
```

- [ ] **Step 2: Run it**

```bash
chmod +x tree/test/smoke.sh && tree/test/smoke.sh
```

Expected: the line ` ▸ No agent (2)`, then `PASS`. Then `herdr session list` must show only the sessions that were there before.

If the server does not start, the socket path is too long for a Unix socket: `mktemp` must stay under `/tmp`.

- [ ] **Step 3: Write `tree/README.md`**

````markdown
# tree

Agents in [Herdr](https://herdr.dev) as a tree by supervisor.

Herdr's sidebar groups workspaces by repository. Supervised work has another shape: one agent dispatches workers into several repositories, and a worker can dispatch its own. This pane shows each agent under the agent that started it, with its state, and jumps to the one you select.

```
 ▾ ● ops
   ├─ ● review monitor ⇥                     ops
   │  ├─ ● review-86                         iam
   │  └─ ● review-234                        billing
   └─ ● planning ⇥                           ops
      └─ ● add-probe                         api
 ▾ ● platform migration
   ├─ ● add-vpc-routes                       infra
   └─ ● migrate-scheduler                    scheduler
      └─ ● build-images ⇥                    scheduler
 ▸ No agent (8)
 ▸ Hidden (7)
```

The supervisor of each worker comes from the [dispatch](../dispatch/) plugin's run ledgers. Without dispatch, every agent is a root and the pane is a flat list with states.

## Install

```sh
herdr plugin install mago0/herdr-plugins/tree
```

Requirements: Herdr 0.9.1 or later, Go 1.22 or later to build on install.

Bind a key in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+t"
type = "shell"
command = "herdr plugin pane open --plugin herdr-tree --entrypoint tree --placement popup"
description = "open supervisor tree"
```

Use `--placement split` or `tab` for a view that stays open.

## Keys

| Key | Action |
|---|---|
| `↑` `↓`, `k` `j` | Move. |
| `enter` | Go to the selected agent and close. On a group, fold or unfold. |
| `space` | Fold or unfold. A folded row shows one dot for each agent below it. |
| `a` | Show only agents that are blocked or done, with the rows above them. |
| `q`, `esc` | Close. |

## What a row is

A row is one pane that hosts an agent. Terminals have no row.

- A workspace with one agent is one row with the workspace's name.
- A worker that dispatch placed as a tab is a row under its supervisor, marked with the tab glyph.
- A workspace with supervising agents in several tabs is a row with one row for each tab under it.
- A tab with several agents is a row with one row for each pane under it, marked with the pane glyph.
- `No agent (n)` holds the workspaces with no agent. In practice these are the main checkouts that Herdr keeps as parents of worktree workspaces.

The right column is the ticket key on a root, when every run that agent supervises names the same key. Below a root it is the repository.

## Panes that do not count

Two kinds of agent pane are left out and counted in `Hidden (n)`:

- Loops that dispatch tracks (`dispatch.sh track`). Nothing to set up.
- A pane that sets the `tree=hide` token on itself:

  ```sh
  herdr pane report-metadata "$HERDR_PANE_ID" --source my-loop --token tree=hide
  ```

## Configuration

`$(herdr plugin config-dir herdr-tree)/config.toml`. Every key is optional.

```toml
[glyphs]
# The defaults are Nerd Font icons. These two work in any font.
tab = "⇥"
pane = "›"

[colors]
blocked = "#f38ba8"
working = "#f9e2af"
done = "#a6e3a1"
idle = "#89b4fa"
unknown = "#6c7086"
```

## Use in scripts

`tree --once` prints the tree as plain text and exits. States are `!` blocked, `*` working, `+` done, `o` idle, `·` unknown. It must run inside a Herdr pane.

## Test

```sh
go test ./...
test/smoke.sh    # starts and stops its own Herdr server; needs tmux
```
````

- [ ] **Step 4: Add the plugin to the root `README.md`**

Add one row to the table, after the `dispatch` row:

```markdown
| [tree](tree/) | Agents as a tree by supervisor, with the state of each, and a jump to the selected one. |
```

Add one line to the install block:

```sh
herdr plugin install mago0/herdr-plugins/tree
```

- [ ] **Step 5: Add the contract section to `dispatch/README.md`**

Append at the end of the file:

```markdown
## State read by other plugins

The [tree](../tree/) plugin reads the state below and writes none of it. Treat a change to these names as a breaking change.

| File | Fields |
|---|---|
| `runs/<run>/supervisor` | the supervisor pane id |
| `runs/<run>/ledger.json` | `pane_id`, `tab_id`, `workspace_id`, `worktree`, `created` |
| `panes.json` | `role` (the value `tracked`) |

It also reads the run directory's name for an issue key.
```

- [ ] **Step 6: Link the plugin and check it in a real pane**

This step needs the user. It changes their Herdr plugin list, so ask before `plugin link`.

```bash
herdr plugin link "$(pwd)/tree"
herdr plugin pane open --plugin herdr-tree --entrypoint tree --placement popup
```

Check each item and report the result of each one:

1. The popup opens, the cursor is on the agent it was opened from, and the popup closes when `q` is pressed.
2. `enter` on a worker in another workspace moves Herdr to that workspace and that pane, and the popup closes.
3. `enter` on a tab worker moves to its tab.
4. `space` on a parent folds it, the descendant dots appear, and the fold is still there after the popup is closed and opened again.
5. A new dispatch shows under its supervisor within about one second, with no restart.
6. **Colors.** Put the popup next to the sidebar. For each of `blocked`, `working`, `done` and `idle`, compare the dot of one agent in both. If one differs, set the right value in `DefaultTheme` in `tree/internal/view/theme.go`, and note it in the commit message. Only `unknown` was read from a real sidebar before this step.
7. The tab and pane glyphs take one cell each and the right column lines up on those rows.

- [ ] **Step 7: Commit**

```bash
cd tree && gofmt -l . && go vet ./... && go test ./...
git add -A && git commit -m "tree: smoke test, README and the dispatch state contract"
```

---

## Self-review notes

Spec coverage, by section:

| Spec section | Task |
|---|---|
| Node; panes that do not count | 3 (model), 4 (token read), 5 (tracked read) |
| Parent | 2 |
| Rows for tabs and workspaces; order | 3 |
| Labels, ticket key, right column | 1, 2, 3, 7 |
| Display, colors, glyph configuration | 7 |
| Interaction, fold state, start position, scrolling | 8 |
| Plugin shape, refresh, dependencies, dispatch contract | 1, 8, 9 |
| Failure behavior | 4, 5, 8 |
| Testing, `--once`, smoke | every task; 8; 9 |
