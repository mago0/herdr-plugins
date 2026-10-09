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

// Workspace is one Herdr workspace. Where says where its checkout is, and Worktree is true when
// that checkout is a linked Git worktree.
type Workspace struct {
	ID, Label, Status, Repo string
	Number                  int
	Where                   string
	Worktree                bool
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
	// Where and Worktree are those of the checkout that holds the agent's working directory.
	Where    string
	Worktree bool
	// Tag is the text of the pane's label token, such as a ticket key.
	Tag string
}

// Snapshot is the live Herdr state. Agents are in Herdr's order.
type Snapshot struct {
	Workspaces []Workspace
	Tabs       []Tab
	Agents     []Agent
	// FocusedPane and FocusedTab are the pane Herdr has in focus and its tab.
	FocusedPane, FocusedTab string
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
	// Where says where the row's agent works, and Worktree is true for a linked Git worktree.
	Where    string
	Worktree bool
	// Tag is the label a user or agent set on the row's pane. It is drawn at the right edge.
	Tag      string
	Children []*Node
	order    [3]int
}

// Tree is the built view: supervisors and their workers, then the two folded groups.
type Tree struct {
	Roots   []*Node
	NoAgent []*Node
	Hidden  []*Node
}
