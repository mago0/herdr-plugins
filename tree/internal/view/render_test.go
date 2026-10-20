package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func plain() Theme {
	th := DefaultTheme()
	th.Plain, th.ShellGlyph, th.WorktreeGlyph = true, "$", "⎇"
	return th
}

func sp(n int) string { return strings.Repeat(" ", n) }

func same(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func gke() model.Tree {
	job := &model.Node{ID: "w2:p1", Label: "SRE-194 job scheduler", Status: model.Idle, Shown: model.KindPane, Worker: true,
		Repo: "job-scheduler", Worktree: true, Git: model.Git{Repo: "job-scheduler", Branch: "sre-194"}, Tag: "SRE-194",
		Children: []*model.Node{{ID: "w2:p2", Label: "shepherd", Shown: model.KindShell}}}
	lead := &model.Node{ID: "w1:p1", Label: "HANDOFF.md resume", Status: model.Idle, Shown: model.KindPane,
		Children: []*model.Node{job}}
	e := &model.Node{ID: "w1", Label: "GKE Migration", Shown: model.KindWorkspace, Repo: "flosports",
		Git: model.Git{Repo: "flosports", Branch: "main"}, Link: "https://linear.app/flosports/project/gke-f425",
		Children: []*model.Node{{ID: "w1:p2", Label: "Status", Shown: model.KindShell}, lead}}
	return model.Tree{Roots: []*model.Node{e}}
}

func TestRenderAnEffort(t *testing.T) {
	same(t, Render(Rows(gke(), State{}), 44, -1, plain()), []string{
		" ▾ GKE Migration" + sp(22) + "proj ↗",
		sp(5) + "flosports@main",
		sp(5) + "$ Status",
		sp(5) + "o HANDOFF.md resume",
		sp(5) + "└ o SRE-194 job scheduler" + sp(7) + "SRE-194",
		sp(9) + "⎇ job-scheduler@sre-194",
		sp(9) + "$ shepherd",
	})
}

func TestRenderBranchLinesRunPastAWorkerToTheNextOne(t *testing.T) {
	w := func(id string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Shown: model.KindPane, Worker: true, Repo: "iam", Children: kids}
	}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Working, Shown: model.KindPane, Children: []*model.Node{
		w("a", &model.Node{ID: "w2:p2", Label: "reviewr", Shown: model.KindShell}),
		w("b"),
	}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, -1, plain()), []string{
		" ▾ Main",
		"     * lead",
		"     ├ o a",
		"     │   iam",
		"     │   $ reviewr",
		"     └ o b",
		sp(9) + "iam",
	})
}

func TestRenderFoldedEffortShowsTheDotsOfItsAgents(t *testing.T) {
	core := &model.Node{ID: "w3", Label: "Core", Shown: model.KindWorkspace, Tag: "SRE-923",
		Link: "https://linear.app/flosports/issue/SRE-923",
		Children: []*model.Node{
			{ID: "w3:p2", Label: "shepherd", Shown: model.KindShell},
			{ID: "w3:p1", Label: "lead", Status: model.Done, Shown: model.KindPane, Children: []*model.Node{
				{ID: "w4:p1", Label: "sweep", Status: model.Working, Shown: model.KindPane, Worker: true}}},
		}}
	notes := &model.Node{ID: "w5", Label: "notes", Shown: model.KindWorkspace,
		Children: []*model.Node{{ID: "w5:p1", Label: "~/notes", Shown: model.KindShell}}}
	st := State{Folded: map[string]bool{"w3": true, "w5": true}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{core, notes}}, st), 40, -1, plain()), []string{
		" ▸ Core  + *" + sp(19) + "SRE-923 ↗",
		"",
		" ▸ notes  $",
	})
}

func TestRenderWorkerTagAndRepoLine(t *testing.T) {
	review := &model.Node{ID: "w2:p1", Label: "Review infra base services", Status: model.Done, Shown: model.KindPane,
		Worker: true, Repo: "infra-base-services", Reported: true,
		Git:  model.Git{Repo: "infra-base-services", Branch: "main", Ahead: 2, Behind: 1},
		Link: "https://github.com/flocasts/infra-base-services/pull/1079"}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Idle, Shown: model.KindPane, Children: []*model.Node{review}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, -1, plain()), []string{
		" ▾ Main",
		"     o lead",
		"     └ + Review infra base services  #1079",
		sp(9) + "infra-base-services@main -1 +2  reported",
	})
}

func TestRenderNarrowCutsTheTitleAndTheBranchAndKeepsTheTag(t *testing.T) {
	review := &model.Node{ID: "w2:p1", Label: "Review service level objectives", Status: model.Idle, Shown: model.KindPane,
		Worker: true, Repo: "iam", Worktree: true,
		Git:  model.Git{Repo: "iam", Branch: "sre-1006-aws-account-alternate-contacts"},
		Link: "https://github.com/flocasts/iam/pull/57"}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Idle, Shown: model.KindPane, Children: []*model.Node{review}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	got := Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 31, -1, plain())
	same(t, got[2:], []string{
		"     └ o Review service l…  #57",
		sp(9) + "⎇ iam@sre-1006-aws-ac…",
	})
}

func TestRenderVeryNarrowPaneStaysInside(t *testing.T) {
	for _, width := range []int{1, 6, 12} {
		for _, line := range Render(Rows(gke(), State{}), width, 0, plain()) {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line %q is %d cells", width, line, w)
			}
		}
	}
}

func TestRenderCursorHeldPromptsAndStall(t *testing.T) {
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Working, Stalled: true, Pending: 2, Shown: model.KindPane}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	same(t, Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, 1, plain()), []string{
		" ▾ Main",
		">    ~ lead +2",
	})
}

func TestRenderAStalledDotOnlyForAWorkingAgent(t *testing.T) {
	done := &model.Node{ID: "w1:p1", Label: "done", Status: model.Done, Stalled: true, Shown: model.KindPane}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{done}}
	got := Render(Rows(model.Tree{Roots: []*model.Node{main}}, State{}), 0, -1, plain())
	same(t, got[1:], []string{"     + done"})
}

func TestEffortAndWorkerTags(t *testing.T) {
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{Link: "https://linear.app/flosports/project/gke-f425", Tag: "SRE-1"}, "proj ↗"},
		{model.Node{Tag: "SRE-923", Link: "https://linear.app/flosports/issue/SRE-923"}, "SRE-923 ↗"},
		{model.Node{Tag: "SRE-923"}, "SRE-923"},
		{model.Node{Link: "https://example.com/x"}, "↗"},
		{model.Node{}, ""},
	} {
		if got := effortTag(&c.n); got != c.want {
			t.Errorf("effortTag(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{Tag: "SRE-194", Link: "https://github.com/flocasts/iam/pull/5"}, "SRE-194"},
		{model.Node{Link: "https://github.com/flocasts/iam/pull/5"}, "#5"},
		{model.Node{Link: "https://linear.app/flosports/issue/SRE-1"}, ""},
	} {
		if got := workerTag(&c.n); got != c.want {
			t.Errorf("workerTag(%+v) = %q, want %q", c.n, got, c.want)
		}
	}
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

func TestRenderKeepsTheCountsAndReportedInANarrowPane(t *testing.T) {
	job := &model.Node{ID: "w2:p1", Label: "job", Status: model.Idle, Shown: model.KindPane, Worker: true, Reported: true,
		Repo: "job-scheduler", Worktree: true,
		Git: model.Git{Repo: "job-scheduler", Branch: "sre-194-watchdog-no-sidecar", Ahead: 1, Behind: 2}}
	lead := &model.Node{ID: "w1:p1", Label: "lead", Status: model.Idle, Shown: model.KindPane, Children: []*model.Node{job}}
	main := &model.Node{ID: "w1", Label: "Main", Shown: model.KindWorkspace, Children: []*model.Node{lead}}
	tree := model.Tree{Roots: []*model.Node{main}}
	for width, want := range map[int]string{
		50: sp(9) + "⎇ job-scheduler@sre-194-… -2 +1  reported",
		30: sp(9) + "⎇ jo… -2 +1  reported",
	} {
		if got := Render(Rows(tree, State{}), width, -1, plain())[3]; got != want {
			t.Errorf("width %d:\n got %q\nwant %q", width, got, want)
		}
	}
}
