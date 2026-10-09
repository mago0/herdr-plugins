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
				Where: a.Where, Worktree: a.Worktree,
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
			Where: a.Where, Worktree: a.Worktree,
			order: [3]int{wsByID[a.WorkspaceID].Number, tabByID[a.TabID].Number, i},
		}
		if p := l.parent[a.PaneID]; p != "" {
			up[a.PaneID] = p
		}
	}

	// A container holds several heads. When its dispatched heads share one supervisor it takes their
	// place under that supervisor; heads of different supervisors stay under their own.
	contain := func(c *Node, heads []string) bool {
		supervisors := map[string]bool{}
		for _, h := range heads {
			if p, ok := up[h]; ok {
				supervisors[p] = true
			}
		}
		shared := len(supervisors) == 1
		var inside []string
		for _, h := range heads {
			if _, dispatched := up[h]; !dispatched || shared {
				inside = append(inside, h)
			}
		}
		if len(inside) == 0 {
			return false
		}
		nodes[c.ID] = c
		for _, h := range inside {
			if p, ok := up[h]; ok {
				up[c.ID] = p
			}
			up[h] = c.ID
		}
		return true
	}
	// A container can close a loop that the pane links did not have. The node that is its own
	// ancestor becomes a root, so no row drops out of the tree.
	unloop := func() {
		ids := make([]string, 0, len(up))
		for id := range up {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			seen := map[string]bool{id: true}
			for p := up[id]; p != ""; p = up[p] {
				if p == id {
					delete(up, id)
					break
				}
				if seen[p] {
					break
				}
				seen[p] = true
			}
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
			if contain(&Node{
				ID: t.ID, Focus: KindTab, Shown: KindTab, TabID: t.ID, WorkspaceID: t.WorkspaceID,
				Label: t.Label, Status: t.Status, Repo: nodes[heads[0]].Repo,
				Where: nodes[heads[0]].Where, Worktree: nodes[heads[0]].Worktree,
				order: [3]int{w.Number, t.Number, -1},
			}, heads) {
				tabRow[t.ID] = t.ID
			}
		}
	}
	unloop()

	occupied := map[string]bool{}
	for _, a := range visible {
		occupied[a.WorkspaceID] = true
	}

	// Workspace rows: the same rule over the tab rows.
	for _, w := range workspaces {
		var heads []string
		for _, t := range tabs {
			id, ok := tabRow[t.ID]
			if t.WorkspaceID != w.ID || !ok {
				continue
			}
			if p, ok := nodes[up[id]]; ok && p.WorkspaceID == w.ID {
				continue
			}
			heads = append(heads, id)
		}
		switch {
		case !occupied[w.ID]:
			tree.NoAgent = append(tree.NoAgent, &Node{
				ID: w.ID, Focus: KindWorkspace, Shown: KindWorkspace, WorkspaceID: w.ID,
				Label: w.Label, Status: w.Status, Repo: w.Repo, Where: w.Where, Worktree: w.Worktree,
			})
		case len(heads) == 1:
			n := nodes[heads[0]]
			n.Label, n.Shown = w.Label, KindWorkspace
		case len(heads) > 1:
			contain(&Node{
				ID: w.ID, Focus: KindWorkspace, Shown: KindWorkspace, WorkspaceID: w.ID,
				Label: w.Label, Status: w.Status, Repo: w.Repo, Where: w.Where, Worktree: w.Worktree,
				order: [3]int{w.Number, -1, -1},
			}, heads)
		}
	}
	unloop()

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
