// Package model builds the efforts of the sidebar from plain data. It does no I/O.
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
	// Link is the web address of the pane's link token, such as that of the ticket.
	Link string
	// Supervisor is the pane that supervises this one, or "".
	Supervisor string
	// Repo is the repository of the checkout that holds the agent's working directory.
	Repo string
	// Pending counts the prompts Herdr holds for this pane until it is safe to type them.
	Pending int
	// Stalled is true when the agent reports working and its screen does not change.
	Stalled bool
	// Blocker is what a blocked agent waits for, when Herdr knows it. BlockerLines is the same
	// text as the lines of the agent's screen, and Dialog is the name Herdr gives those lines.
	Blocker      string
	BlockerLines []string
	Dialog       string
	// Reported is true when the dispatch ledger has the agent's worker_done taken by its supervisor.
	Reported bool
}

// Shell is one pane that hosts no agent.
type Shell struct {
	PaneID, TabID, WorkspaceID string
	// Label is the name the user gave the pane, or "".
	Label string
	// Where is "<repo>/<path in the repo>" inside a repository, else the path with home as ~.
	Where string
}

// Snapshot is the live Herdr state. Agents are in Herdr's order.
type Snapshot struct {
	Workspaces []Workspace
	Tabs       []Tab
	Agents     []Agent
	Shells     []Shell
	// FocusedPane and FocusedTab are the pane Herdr has in focus and its tab.
	FocusedPane, FocusedTab string
}

type Kind int

const (
	KindPane Kind = iota
	KindTab
	KindWorkspace
	KindGroup
	// KindShell is a pane with no agent.
	KindShell
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
	// Link is the web address that the pane of the row names for its work, or "".
	Link string
	// Pending, Stalled and Blocker are those of the row's agent.
	Pending int
	Stalled bool
	Blocker string
	// Answers are the choices of the dialog a blocked agent shows, when one key picks each.
	// Dialog names that dialog, so that Herdr can refuse an answer for a dialog that is gone.
	Answers  []Answer
	Dialog   string
	// Worker is true for an agent that an agent in another workspace supervises.
	Worker bool
	// Reported is true for a worker whose result its supervisor took and that is not working again.
	Reported bool
	Children []*Node
}

// Tree is the built view: one effort for each workspace that has its own agent or no agent.
type Tree struct {
	Roots   []*Node
	NoAgent []*Node
	Hidden  []*Node
}
