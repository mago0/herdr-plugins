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
