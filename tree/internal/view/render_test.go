package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func plain() Theme {
	th := DefaultTheme()
	th.Plain, th.TabGlyph, th.PaneGlyph, th.WorktreeGlyph = true, "⇥", "›", "⎇"
	return th
}

func sp(n int) string { return strings.Repeat(" ", n) }

func same(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRenderOpenTree(t *testing.T) {
	review := &model.Node{ID: "r", Label: "review-86", Status: model.Done, Repo: "iam", Shown: model.KindWorkspace}
	build := &model.Node{ID: "b", Label: "build", Status: model.Idle, Repo: "api", Shown: model.KindTab}
	ops := &model.Node{ID: "o", Label: "ops", Status: model.Working, Ticket: "ABC-1", Shown: model.KindWorkspace,
		Children: []*model.Node{review, build}}
	tree := model.Tree{Roots: []*model.Node{ops}, NoAgent: []*model.Node{{ID: "x", Label: "x"}, {ID: "y", Label: "y"}}}
	got := Render(Rows(tree, State{}), 40, 1, plain())
	same(t, got, []string{
		" ▾ * ops" + sp(2) + "ABC-1",
		">  ├─ + review-86",
		">  │" + sp(4) + "iam",
		"   └─ o build ⇥",
		sp(8) + "api",
		" ▸ No agent (2)",
	})
}

func TestRenderFoldedParentShowsDescendantDots(t *testing.T) {
	ops := &model.Node{ID: "o", Label: "ops", Status: model.Working, Ticket: "ABC-1", Shown: model.KindWorkspace,
		Children: []*model.Node{
			{ID: "a", Label: "a", Status: model.Done},
			{ID: "b", Label: "b", Status: model.Blocked},
		}}
	rows := Rows(model.Tree{Roots: []*model.Node{ops}}, State{Folded: map[string]bool{"o": true}})
	same(t, Render(rows, 40, -1, plain()), []string{" ▸ * ops" + sp(2) + "ABC-1" + sp(2) + "+ !"})
}

func TestRenderPaneGlyphAndUnknownDot(t *testing.T) {
	tab := &model.Node{ID: "t", Label: "planning", Status: model.Unknown, Shown: model.KindTab, Children: []*model.Node{
		{ID: "p", Label: "left", Status: model.Idle, Repo: "ops", Shown: model.KindPane}}}
	got := Render(Rows(model.Tree{Roots: []*model.Node{tab}}, State{}), 40, -1, plain())
	same(t, got, []string{
		" ▾ · planning",
		"   └─ o left ›",
		sp(8) + "ops",
	})
}

func TestRenderGuideLines(t *testing.T) {
	n := func(id string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Shown: model.KindWorkspace, Children: kids}
	}
	tree := model.Tree{Roots: []*model.Node{n("lead", n("a", n("a1"), n("a2")), n("b", n("b1")))}}
	rows := Rows(tree, State{Folded: map[string]bool{"b": true}})
	same(t, Render(rows, 40, -1, plain()), []string{
		" ▾ o lead",
		"   ├─ o a",
		"   │  ├─ o a1",
		"   │  └─ o a2",
		"   └▸ o b" + sp(2) + "o",
	})
}

func TestRenderNarrowAndWide(t *testing.T) {
	long := &model.Node{ID: "l", Label: "a-very-long-workspace-label-that-does-not-fit", Status: model.Idle, Ticket: "ABC-12345",
		Children: []*model.Node{{ID: "c", Label: "日本語のラベルは幅が広い", Status: model.Done, Repo: "repository"}}}
	rows := Rows(model.Tree{Roots: []*model.Node{long}}, State{})
	for _, width := range []int{0, 1, 5, 10, 24, 60} {
		for _, th := range []Theme{plain(), DefaultTheme()} {
			for _, line := range Render(rows, width, 0, th) {
				if w := lipgloss.Width(line); width > 0 && w > width {
					t.Errorf("width %d plain=%v: line is %d cells: %q", width, th.Plain, w, line)
				}
			}
		}
	}
	// A cut label ends in an ellipsis, and the ticket stays on the line.
	got := Render(rows, 30, -1, plain())
	if !strings.Contains(got[0], "…") || !strings.HasSuffix(got[0], "ABC-12345") {
		t.Errorf("root line = %q", got[0])
	}
	if !strings.Contains(got[1], "…") || strings.TrimSpace(got[2]) != "repository" {
		t.Errorf("child lines = %q, %q", got[1], got[2])
	}
}

func TestRenderRepoBelowTheNameOnlyWhereItChanges(t *testing.T) {
	n := func(id, repo string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Repo: repo, Shown: model.KindWorkspace, Children: kids}
	}
	tree := model.Tree{Roots: []*model.Node{
		n("main", "flo", n("same", "flo"), n("mid", "iam", n("leaf", "iam"), n("other", "ops"))),
	}}
	same(t, Render(Rows(tree, State{}), 40, -1, plain()), []string{
		" ▾ o main",
		"   │ flo",
		"   ├─ o same",
		"   └─ o mid",
		"      │ iam",
		"      ├─ o leaf",
		"      └─ o other",
		sp(11) + "ops",
	})
}

func TestRenderMarksARowThatWorksInALinkedWorktree(t *testing.T) {
	worker := &model.Node{ID: "w", Label: "worker", Status: model.Idle, Shown: model.KindTab, Worktree: true}
	lead := &model.Node{ID: "l", Label: "lead", Status: model.Idle, Shown: model.KindWorkspace, Worktree: true,
		Children: []*model.Node{worker, {ID: "m", Label: "main", Status: model.Idle, Shown: model.KindWorkspace}}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{lead}}, State{}), 40, -1, plain()), []string{
		" ▾ o lead ⎇",
		"   ├─ o worker ⇥ ⎇",
		"   └─ o main",
	})
}

func TestCutLeftKeepsTheEnd(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{40, "iam/_worktrees/review-86"}, {10, "…review-86"}, {1, "…"}, {0, ""}} {
		if got := cutLeft("iam/_worktrees/review-86", c.n); got != c.want {
			t.Errorf("cutLeft(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestRenderNoRows(t *testing.T) {
	if got := Render(nil, 40, 0, plain()); len(got) != 0 {
		t.Fatalf("no rows, got %q", got)
	}
}
