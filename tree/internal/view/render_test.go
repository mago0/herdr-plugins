package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func plain() Theme {
	th := DefaultTheme()
	th.Plain, th.TabGlyph, th.PaneGlyph = true, "⇥", "›"
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
		" ▾ * ops" + sp(11) + "ABC-1",
		">  ├─ + review-86" + sp(2) + "iam",
		"   └─ o build ⇥" + sp(4) + "api",
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
		"   └─ o left ›" + sp(2) + "ops",
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
		"   └▸ o b" + sp(8) + "o",
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
	// At a width that fits the right column, a cut label ends in an ellipsis and the column still lines up.
	got := Render(rows, 30, -1, plain())
	if !strings.Contains(got[0], "…") || !strings.HasSuffix(got[0], "ABC-12345") {
		t.Errorf("root line = %q", got[0])
	}
	if !strings.Contains(got[1], "…") || !strings.HasSuffix(got[1], "repository") {
		t.Errorf("child line = %q", got[1])
	}
	a, b := strings.Index(got[0], "ABC-12345"), strings.Index(got[1], "repository")
	if lipgloss.Width(got[0][:a]) != lipgloss.Width(got[1][:b]) {
		t.Errorf("right column is not aligned:\n%q\n%q", got[0], got[1])
	}
}

func TestRenderNoRows(t *testing.T) {
	if got := Render(nil, 40, 0, plain()); len(got) != 0 {
		t.Fatalf("no rows, got %q", got)
	}
}
