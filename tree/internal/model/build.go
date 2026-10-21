package model

import (
	"sort"
	"strings"
)

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

	tabName := map[string]string{}
	for _, t := range s.Tabs {
		tabName[t.ID] = t.Label
	}
	inTab := map[string]int{}
	for _, a := range visible {
		inTab[a.TabID]++
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
		label := AgentLabel(a)
		// The user names a tab for the job in it; Herdr numbers the tabs that have no name.
		// A name the user gave the pane itself is closer still.
		if name := tabName[a.TabID]; inTab[a.TabID] == 1 && strings.Trim(name, "0123456789") != "" {
			label = name
		}
		if a.PaneName != "" {
			label = a.PaneName
		}
		_, worker := up[a.PaneID]
		nodes[a.PaneID] = &Node{
			ID: a.PaneID, Focus: KindPane, Shown: KindPane, TabID: a.TabID, WorkspaceID: a.WorkspaceID,
			Label: label, Status: a.Status, Repo: repo, Where: a.Where, Worktree: a.Worktree,
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

	linked := map[string]bool{}
	for _, w := range workspaces {
		if w.Worktree {
			linked[w.Repo] = true
		}
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
		if len(own) == 0 && w.DispatchParent && linked[w.Repo] && idle(shells[w.ID], w.Repo) {
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

// idle is true when every shell is unnamed and sits at the root of repo: the shell Herdr opens
// with a dispatch parent, which the worker rows of that repo already stand for.
func idle(shells []*Node, repo string) bool {
	for _, p := range shells {
		if p.Label != repo || p.Where != repo {
			return false
		}
	}
	return true
}
