package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func effort(id string, kids ...*model.Node) *model.Node {
	return &model.Node{ID: id, Label: id, Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w-" + id, Children: kids}
}

func agent(id, status string, kids ...*model.Node) *model.Node {
	return &model.Node{ID: id, Label: id, Status: status, Focus: model.KindPane, Shown: model.KindPane, TabID: id + ":t", Children: kids}
}

func worker(id, status string, kids ...*model.Node) *model.Node {
	n := agent(id, status, kids...)
	n.Worker = true
	return n
}

func shell(id string) *model.Node {
	return &model.Node{ID: id, Label: id, Status: model.Unknown, Focus: model.KindPane, Shown: model.KindShell}
}

func sample() model.Tree {
	return model.Tree{Roots: []*model.Node{
		effort("lead", agent("a", model.Done), agent("b", model.Idle, worker("b1", model.Blocked))),
		effort("solo"),
		effort("repo1", shell("sh1")),
	}}
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
 repo1 []
   sh1 []
`)
}

func TestRowsFoldAndRollup(t *testing.T) {
	st := State{Folded: map[string]bool{"lead": true, "repo1": true}}
	expect(t, Rows(sample(), st), `
+lead [done idle blocked]
 solo []
+repo1 [shell]
`)
}

func TestRowsALeafEffortDoesNotFold(t *testing.T) {
	expect(t, Rows(model.Tree{Roots: []*model.Node{effort("solo")}}, State{Folded: map[string]bool{"solo": true}}), `
 solo []
`)
}

func TestRowsBranchLinesOfWorkers(t *testing.T) {
	tree := model.Tree{Roots: []*model.Node{
		effort("e", agent("L", model.Idle,
			worker("w1", model.Idle, shell("s1"), worker("w1a", model.Idle)),
			worker("w2", model.Idle))),
	}}
	got := map[string]Row{}
	for _, r := range Rows(tree, State{}) {
		got[r.Node.ID] = r
	}
	for _, c := range []struct {
		id    string
		depth int
		last  bool
		trunk []bool
	}{
		{"L", 1, false, nil},
		{"w1", 2, false, nil},
		{"s1", 3, false, []bool{true}},
		{"w1a", 3, true, []bool{true}},
		{"w2", 2, true, nil},
	} {
		r := got[c.id]
		if r.Depth != c.depth || r.Last != c.last || fmt.Sprint(r.Trunk) != fmt.Sprint(c.trunk) {
			t.Errorf("%s: depth=%d last=%v trunk=%v, want depth=%d last=%v trunk=%v",
				c.id, r.Depth, r.Last, r.Trunk, c.depth, c.last, c.trunk)
		}
	}
}

func TestRowsRepoLineOnlyUnderEffortsAndWorkers(t *testing.T) {
	a := agent("a", model.Idle, worker("w", model.Idle))
	a.Repo, a.Children[0].Repo = "ops", "iam"
	x := effort("x", a)
	x.Repo = "flosports"
	tree := model.Tree{Roots: []*model.Node{x, effort("y")}}
	for _, r := range Rows(tree, State{}) {
		want := map[string]string{"x": "flosports", "a": "", "w": "iam", "y": ""}[r.Node.ID]
		if r.Repo != want {
			t.Errorf("%s: repo %q, want %q", r.Node.ID, r.Repo, want)
		}
		if gap := r.Node.ID == "y"; r.Gap != gap {
			t.Errorf("%s: gap %v, want %v", r.Node.ID, r.Gap, gap)
		}
	}
}

func TestRowsFoldAnAgentWithWorkers(t *testing.T) {
	expect(t, Rows(sample(), State{Folded: map[string]bool{"b": true, "a": true}}), `
 lead []
   a []
  +b [blocked]
 solo []
 repo1 []
   sh1 []
`)
}
