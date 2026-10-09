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
