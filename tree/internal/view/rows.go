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
