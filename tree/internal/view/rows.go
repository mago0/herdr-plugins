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
	// Repo is the repository shown on a second line: set where it is not the repo of the row above.
	Repo string
	// Gap is true for a row with a blank line above it: each root after the first, and the
	// first of the groups.
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
	var walk func(n *model.Node, depth int, trunk []bool, last bool, above string)
	walk = func(n *model.Node, depth int, trunk []bool, last bool, above string) {
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
		if depth == 0 && len(out) > 0 {
			above := out[len(out)-1]
			r.Gap = n.Shown != model.KindGroup || above.Depth > 0 || above.Node.Shown != model.KindGroup
		}
		if n.Shown != model.KindGroup && n.Repo != above {
			r.Repo = n.Repo
		}
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
				walk(c, depth+1, below, i == len(kids)-1, n.Repo)
			}
		}
	}
	for _, n := range t.Roots {
		if !st.Attention || needs(n) {
			walk(n, 0, nil, true, "")
		}
	}
	if st.Attention {
		return out
	}
	if len(t.NoAgent) > 0 {
		walk(group(GroupNoAgent, "No agent", t.NoAgent), 0, nil, true, "")
	}
	if len(t.Hidden) > 0 {
		walk(group(GroupHidden, "Hidden", t.Hidden), 0, nil, true, "")
	}
	return out
}
