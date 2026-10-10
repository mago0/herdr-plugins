package model

// resolveLinks reads the supervisor of each shown pane. A supervisor that is not shown gives no
// link, and a pane that is its own ancestor becomes a root.
func resolveLinks(agents []Agent) map[string]string {
	shown := map[string]bool{}
	for _, a := range agents {
		shown[a.PaneID] = true
	}
	parent := map[string]string{}
	for _, a := range agents {
		if a.Supervisor != "" && a.Supervisor != a.PaneID && shown[a.Supervisor] {
			parent[a.PaneID] = a.Supervisor
		}
	}
	for _, a := range agents {
		seen := map[string]bool{a.PaneID: true}
		for p := parent[a.PaneID]; p != ""; p = parent[p] {
			if p == a.PaneID {
				delete(parent, a.PaneID)
				break
			}
			if seen[p] {
				break
			}
			seen[p] = true
		}
	}
	return parent
}
