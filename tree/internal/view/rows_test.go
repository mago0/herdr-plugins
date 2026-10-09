package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func node(id, status string, kids ...*model.Node) *model.Node {
	return &model.Node{ID: id, Label: id, Status: status, Shown: model.KindWorkspace, Children: kids}
}

func sample() model.Tree {
	return model.Tree{
		Roots: []*model.Node{
			node("lead", model.Working,
				node("a", model.Done),
				node("b", model.Idle, node("b1", model.Blocked))),
			node("solo", model.Idle),
		},
		NoAgent: []*model.Node{node("repo1", model.Unknown), node("repo2", model.Unknown)},
		Hidden:  []*model.Node{node("watch", model.Working)},
	}
}

func show(rows []Row) string {
	var b strings.Builder
	for _, r := range rows {
		mark := " "
		if r.Folded {
			mark = "+"
		}
		fmt.Fprintf(&b, "%s%s%s %v\n", strings.Repeat("  ", r.Depth), mark, r.Node.Label, r.Rollup)
	}
	return b.String()
}

func expect(t *testing.T, rows []Row, want string) {
	t.Helper()
	if got := show(rows); got != strings.TrimLeft(want, "\n") {
		t.Fatalf("rows:\n%s\nwant:\n%s", got, want)
	}
}

func TestRowsDefault(t *testing.T) {
	expect(t, Rows(sample(), State{}), `
 lead []
   a []
   b []
     b1 []
 solo []
+No agent (2) []
+Hidden (1) []
`)
}

func TestRowsFoldAndRollup(t *testing.T) {
	st := State{Folded: map[string]bool{"lead": true, GroupHidden: false}}
	expect(t, Rows(sample(), st), `
+lead [done idle blocked]
 solo []
+No agent (2) []
 Hidden (1) []
   watch []
`)
}

func TestRowsAttention(t *testing.T) {
	expect(t, Rows(sample(), State{Attention: true}), `
 lead []
   a []
   b []
     b1 []
`)
	quiet := model.Tree{Roots: []*model.Node{node("solo", model.Idle)}, Hidden: []*model.Node{node("w", model.Done)}}
	if rows := Rows(quiet, State{Attention: true}); len(rows) != 0 {
		t.Fatalf("nothing needs attention, got %s", show(rows))
	}
}

func TestRowsGuides(t *testing.T) {
	deep := model.Tree{Roots: []*model.Node{
		node("lead", model.Idle,
			node("a", model.Idle, node("a1", model.Idle)),
			node("b", model.Idle, node("b1", model.Idle))),
	}}
	got := map[string]Row{}
	for _, r := range Rows(deep, State{}) {
		got[r.Node.ID] = r
	}
	cases := []struct {
		id    string
		last  bool
		trunk []bool
	}{
		{"a", false, nil},
		{"b", true, nil},
		{"a1", true, []bool{true}},  // a has a later sibling: its guide line runs past a1
		{"b1", true, []bool{false}}, // b is the last child: nothing runs past b1
	}
	for _, c := range cases {
		r := got[c.id]
		if r.Last != c.last || fmt.Sprint(r.Trunk) != fmt.Sprint(c.trunk) {
			t.Errorf("%s: last=%v trunk=%v, want last=%v trunk=%v", c.id, r.Last, r.Trunk, c.last, c.trunk)
		}
	}
	// With the filter on, the last kept child is the last one.
	deep.Roots[0].Children[0].Children[0].Status = model.Blocked
	for _, r := range Rows(deep, State{Attention: true}) {
		if r.Node.ID == "a" && !r.Last {
			t.Errorf("a is the only kept child of lead and must be last")
		}
	}
}

func TestRowsEmptyGroupsAndLeafFold(t *testing.T) {
	only := model.Tree{Roots: []*model.Node{node("solo", model.Idle)}}
	// A stored fold on a node with no children must not mark it folded.
	expect(t, Rows(only, State{Folded: map[string]bool{"solo": true}}), `
 solo []
`)
}
