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
	Git                     Git
}

// Git is the state of a main checkout. Repo names its repository. Branch is the branch, or a
// short commit id when HEAD is detached. Ahead and Behind count commits against the upstream.
type Git struct {
	Repo, Branch  string
	Ahead, Behind int
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
	Git      Git
	// Tag is the text of the pane's label token, such as a ticket key.
	Tag string
	// Supervisor is the pane that supervises this one, or "".
	Supervisor string
	// Repo is the repository of the checkout that holds the agent's working directory.
	Repo string
	// Pending counts the prompts Herdr holds for this pane until it is safe to type them.
	Pending int
	// Stalled is true when the agent reports working and its screen does not change.
	Stalled bool
	// Blocker is what a blocked agent waits for, when Herdr knows it.
	Blocker string
}

// Snapshot is the live Herdr state. Agents are in Herdr's order.
type Snapshot struct {
	Workspaces []Workspace
	Tabs       []Tab
	Agents     []Agent
	// FocusedPane and FocusedTab are the pane Herdr has in focus and its tab.
	FocusedPane, FocusedTab string
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
	Repo               string
	// Where says where the row's agent works, and Worktree is true for a linked Git worktree.
	Where    string
	Worktree bool
	// Git is set when the row works in the main checkout of Repo.
	Git Git
	// Tag is the label a user or agent set on the row's pane. It is drawn at the right edge.
	Tag string
	// Pending, Stalled and Blocker are those of the row's agent.
	Pending int
	Stalled bool
	Blocker string
	// Answers are the choices of the dialog a blocked agent shows, when one key picks each.
	Answers  []Answer
	Children []*Node
	order    [3]int
}

// Tree is the built view: supervisors and their workers, then the two folded groups.
type Tree struct {
	Roots   []*Node
	NoAgent []*Node
	Hidden  []*Node
}
