package view

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

type fake struct {
	tree     model.Tree
	focused  []string
	focusErr error
	saved    map[string]bool
}

func (f *fake) deps() Deps {
	return Deps{
		Load:  func() (model.Tree, error) { return f.tree, nil },
		Focus: func(n *model.Node) error { f.focused = append(f.focused, n.ID); return f.focusErr },
		Save:  func(folds map[string]bool, _ model.Tree) { f.saved = folds },
	}
}

func start(t *testing.T, f *fake, d Deps) Program {
	t.Helper()
	p := New(d, nil, plain())
	return send(t, p, loadedMsg{tree: f.tree})
}

func send(t *testing.T, p Program, msgs ...tea.Msg) Program {
	t.Helper()
	for _, m := range msgs {
		next, _ := p.Update(m)
		p = next.(Program)
	}
	return p
}

func key(s string) tea.Msg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func selected(p Program) string { return p.rows[p.cursor].Node.ID }

func TestProgramMoveAndFocus(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	if selected(p) != "lead" {
		t.Fatalf("starts on %q", selected(p))
	}
	p = send(t, p, key("up"))
	if selected(p) != "lead" {
		t.Fatalf("up at the top stays, got %q", selected(p))
	}
	p = send(t, p, key("down"), key("j"))
	if selected(p) != "b" {
		t.Fatalf("selected %q, want b", selected(p))
	}
	_, cmd := p.Update(key("enter"))
	msg := cmd()
	if len(f.focused) != 1 || f.focused[0] != "b" {
		t.Fatalf("focused %v, want [b]", f.focused)
	}
	if _, quit := p.Update(msg); quit == nil || quit() != tea.Quit() {
		t.Fatal("a good focus closes the pane")
	}
}

func TestProgramFocusErrorStaysOpen(t *testing.T) {
	f := &fake{tree: sample(), focusErr: errors.New("pane_not_found: gone")}
	p := start(t, f, f.deps())
	_, cmd := p.Update(key("enter"))
	next, reload := p.Update(cmd())
	p = next.(Program)
	if reload == nil {
		t.Fatal("a failed focus reloads the tree")
	}
	if _, ok := reload().(loadedMsg); !ok {
		t.Fatal("a failed focus must not close the pane")
	}
	if !strings.Contains(p.View(), "pane_not_found") {
		t.Errorf("the error is shown:\n%s", p.View())
	}
}

func TestProgramFoldSavesAndGroupsUnfoldOnEnter(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("space"))
	if len(p.rows) != 4 || !p.rows[0].Folded {
		t.Fatalf("space folds the selected parent, rows:\n%s", show(p.rows))
	}
	if !f.saved["lead"] {
		t.Fatalf("the fold is saved, got %v", f.saved)
	}
	p = send(t, p, key("down"), key("down"))
	if selected(p) != GroupNoAgent {
		t.Fatalf("selected %q", selected(p))
	}
	p = send(t, p, key("enter"))
	if len(f.focused) != 0 {
		t.Fatal("enter on a group must not focus anything")
	}
	if p.rows[p.cursor].Folded {
		t.Fatalf("enter on a group unfolds it, rows:\n%s", show(p.rows))
	}
	p = send(t, p, key("space"))
	before := len(p.rows)
	p = send(t, p, key("up"), key("space"))
	if selected(p) != "solo" || len(p.rows) != before {
		t.Fatalf("space on a row with no children does nothing, rows:\n%s", show(p.rows))
	}
}

func TestProgramAttentionAndQuit(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("a"))
	if len(p.rows) != 4 {
		t.Fatalf("attention rows:\n%s", show(p.rows))
	}
	p = send(t, p, key("a"))
	if len(p.rows) != 7 {
		t.Fatalf("attention off rows:\n%s", show(p.rows))
	}
	for _, k := range []string{"q", "esc"} {
		if _, cmd := p.Update(key(k)); cmd == nil || cmd() != tea.Quit() {
			t.Errorf("%s must quit", k)
		}
	}
}

func TestProgramKeepsSelectionAcrossReload(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("down"), key("down"), key("down"), key("down"))
	if selected(p) != "solo" {
		t.Fatalf("selected %q", selected(p))
	}
	// A worker goes away: the cursor follows the same node to its new line.
	shorter := sample()
	shorter.Roots[0].Children = shorter.Roots[0].Children[:1]
	p = send(t, p, loadedMsg{tree: shorter})
	if selected(p) != "solo" {
		t.Fatalf("after reload selected %q, want solo", selected(p))
	}
	// The selected node and everything after it go away: the cursor lands on the last row.
	p = send(t, p, loadedMsg{tree: model.Tree{Roots: []*model.Node{node("lead", model.Idle)}}})
	if p.cursor != 0 || selected(p) != "lead" {
		t.Fatalf("cursor %d on %q, want the last row", p.cursor, selected(p))
	}
	// Nothing left at all: no panic on any key.
	p = send(t, p, loadedMsg{tree: model.Tree{}}, key("down"), key("space"), key("enter"))
	if !strings.Contains(p.View(), "no agents") {
		t.Errorf("empty view:\n%s", p.View())
	}
}

func TestProgramStartsOnOrigin(t *testing.T) {
	f := &fake{tree: sample()}
	f.tree.Roots[0].Children[1].TabID = "w5:t2"
	d := f.deps()
	d.OriginPane = "b1"
	if p := start(t, f, d); selected(p) != "b1" {
		t.Fatalf("starts on %q, want the origin pane", selected(p))
	}
	d.OriginPane, d.OriginTab = "w5:p9", "w5:t2"
	if p := start(t, f, d); selected(p) != "b" {
		t.Fatalf("starts on %q, want the first row of the origin tab", selected(p))
	}
}

func TestProgramLoadErrorKeepsTree(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, loadedMsg{err: errors.New("dial unix: no such file")})
	if len(p.rows) != 7 || !strings.Contains(p.View(), "no such file") {
		t.Fatalf("a load error keeps the last tree and shows the error:\n%s", p.View())
	}
}

func TestProgramScrolls(t *testing.T) {
	var roots []*model.Node
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		roots = append(roots, node(id, model.Idle))
	}
	f := &fake{tree: model.Tree{Roots: roots}}
	p := start(t, f, f.deps())
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 8})
	for i := 0; i < 9; i++ {
		p = send(t, p, key("down"))
	}
	view := p.View()
	if !strings.Contains(view, "> ") || !strings.Contains(view, " j") || strings.Contains(view, " a\n") {
		t.Fatalf("the view follows the cursor to the last row:\n%s", view)
	}
	if n := strings.Count(view, "\n"); n > 8 {
		t.Fatalf("the view is %d lines in a pane of 8", n)
	}
}

func sidebar(t *testing.T, f *fake) Program {
	t.Helper()
	d := f.deps()
	d.Sidebar = true
	return start(t, f, d)
}

func click(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func TestSidebarStaysOpen(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	for _, k := range []string{"q", "esc"} {
		if _, cmd := p.Update(key(k)); cmd != nil {
			t.Fatalf("%s must not close a sidebar pane", k)
		}
	}
	_, cmd := p.Update(key("enter"))
	if _, quit := p.Update(cmd()); quit != nil {
		t.Fatal("a jump must not close a sidebar pane")
	}
	if len(f.focused) != 1 || f.focused[0] != "lead" {
		t.Fatalf("focused %v, want [lead]", f.focused)
	}
}

func TestSidebarViewIsOnlyRows(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	v := p.View()
	if strings.Contains(v, "Agents by supervisor") || strings.Contains(v, "close") {
		t.Fatalf("a sidebar pane has no title or key help:\n%s", v)
	}
	if first := strings.SplitN(v, "\n", 2)[0]; !strings.Contains(first, "lead") {
		t.Fatalf("the first line is the first row, got %q", first)
	}
}

func TestSidebarClickJumpsToTheRow(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	next, cmd := p.Update(click(12, 2))
	p = next.(Program)
	if selected(p) != "b" {
		t.Fatalf("selected %q, want b", selected(p))
	}
	if cmd == nil {
		t.Fatal("a click on a row jumps to it")
	}
	cmd()
	if len(f.focused) != 1 || f.focused[0] != "b" {
		t.Fatalf("focused %v, want [b]", f.focused)
	}
}

func TestSidebarClickOnTheFoldMarkFolds(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	next, cmd := p.Update(click(1, 0))
	p = next.(Program)
	if cmd != nil || len(f.focused) != 0 {
		t.Fatal("a click on the fold mark must not jump")
	}
	if !p.rows[0].Folded || !f.saved["lead"] {
		t.Fatalf("the click folds the row, rows:\n%s", show(p.rows))
	}
}

func TestSidebarClickBelowTheRowsDoesNothing(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	if _, cmd := p.Update(click(3, 20)); cmd != nil {
		t.Fatal("a click below the last row does nothing")
	}
}

func TestSidebarFollowsTheFocusedPane(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	p = send(t, p, loadedMsg{tree: f.tree, current: "b1"})
	if selected(p) != "b1" {
		t.Fatalf("selected %q, want b1", selected(p))
	}
	p = send(t, p, key("up"), loadedMsg{tree: f.tree, current: "b1"})
	if selected(p) != "b" {
		t.Fatalf("a reload with the same focus keeps the cursor, got %q", selected(p))
	}
	p = send(t, p, loadedMsg{tree: f.tree, current: "a"})
	if selected(p) != "a" {
		t.Fatalf("selected %q, want a", selected(p))
	}
}

func TestSidebarWheelScrollsWithoutMovingTheCursor(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 3},
		tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if p.offset == 0 || selected(p) != "lead" {
		t.Fatalf("offset %d, selected %q", p.offset, selected(p))
	}
	if strings.Contains(p.View(), "lead") {
		t.Fatalf("the first row is scrolled out:\n%s", p.View())
	}
	p = send(t, p, tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if p.offset != 0 {
		t.Fatalf("offset %d after wheel up", p.offset)
	}
}

func TestSidebarPlacesTheCursorWhenTheFocusedPaneGetsARow(t *testing.T) {
	f := &fake{tree: model.Tree{Roots: []*model.Node{node("solo", model.Idle)}}}
	p := sidebar(t, f)
	p = send(t, p, loadedMsg{tree: f.tree, current: "late"})
	grown := model.Tree{Roots: []*model.Node{node("solo", model.Idle), node("late", model.Working)}}
	p = send(t, p, loadedMsg{tree: grown, current: "late"})
	if selected(p) != "late" {
		t.Fatalf("selected %q, want late", selected(p))
	}
}

func repos() model.Tree {
	n := func(id, repo string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Repo: repo, Shown: model.KindWorkspace, WorkspaceID: "w-" + id, Children: kids}
	}
	return model.Tree{Roots: []*model.Node{n("main", "flo", n("a", "iam"), n("b", "ops"), n("c", "api"))}}
}

func TestSidebarClickOnARepoLineSelectsItsRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	// Lines: main, flo, a, iam, b, ops, c, api.
	for y, want := range []string{"main", "main", "a", "a", "b", "b", "c", "c"} {
		next, _ := p.Update(click(12, y))
		if got := selected(next.(Program)); got != want {
			t.Errorf("line %d selects %q, want %q", y, got, want)
		}
	}
}

func TestSidebarScrollKeepsBothLinesOfTheCursorRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 4})
	p = send(t, p, key("down"), key("down"))
	v := p.View()
	if !strings.Contains(v, "o b") || !strings.Contains(v, "ops") {
		t.Fatalf("the name and repo of the cursor row are both in view:\n%s", v)
	}
	if n := strings.Count(v, "\n") + 1; n > 4 {
		t.Fatalf("the view is %d lines in a pane of 4", n)
	}
}

func rightClick(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight}
}

func TestSidebarRightClickAsksHerdrForTheMenuOfTheRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	next, cmd := p.Update(rightClick(12, 4))
	if cmd == nil {
		t.Fatal("a right-click on a row asks for its menu")
	}
	if got := fmt.Sprint(cmd()); got != "herdr-menu;workspace;w-b;1;b;" {
		t.Fatalf("request = %q", got)
	}
	if len(f.focused) != 0 {
		t.Fatal("a right-click must not jump")
	}
	// A second request on the same row is a new title, so Herdr sees it.
	_, cmd = next.(Program).Update(rightClick(12, 4))
	if got := fmt.Sprint(cmd()); got != "herdr-menu;workspace;w-b;2;b;" {
		t.Fatalf("second request = %q", got)
	}
}

func TestMenuTargetFollowsWhatTheRowShows(t *testing.T) {
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{ID: "w1:p1", Shown: model.KindWorkspace, WorkspaceID: "w1", TabID: "w1:t1"}, "workspace;w1"},
		{model.Node{ID: "w1", Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w1"}, "workspace;w1"},
		{model.Node{ID: "w1:p1", Shown: model.KindTab, WorkspaceID: "w1", TabID: "w1:t1"}, "tab;w1:t1"},
		{model.Node{ID: "w1:p1", Shown: model.KindPane, WorkspaceID: "w1", TabID: "w1:t1"}, "pane;w1:p1"},
		{model.Node{ID: GroupHidden, Shown: model.KindGroup}, ""},
	} {
		if got := menuTarget(&c.n); got != c.want {
			t.Errorf("%+v: target %q, want %q", c.n, got, c.want)
		}
	}
}

func places() model.Tree {
	n := func(id, where string, kids ...*model.Node) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Shown: model.KindWorkspace, Where: where, Children: kids}
	}
	return model.Tree{Roots: []*model.Node{n("main", "~/src/flo", n("a", "iam/_worktrees/a"), n("b", "ops/_worktrees/b"))}}
}

func lastLine(p Program) string {
	lines := strings.Split(p.View(), "\n")
	return lines[len(lines)-1]
}

func hover(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone}
}

func TestSidebarLastLineSaysWhereTheRowWorks(t *testing.T) {
	f := &fake{tree: places()}
	p := sidebar(t, f)
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 8})
	if n := strings.Count(p.View(), "\n") + 1; n != 8 {
		t.Fatalf("the view is %d lines in a pane of 8, so the last line is not at the bottom", n)
	}
	if got := lastLine(p); got != " ~/src/flo" {
		t.Fatalf("with no pointer the line is for the cursor row, got %q", got)
	}
	p = send(t, p, hover(12, 2))
	if got := lastLine(p); got != " ops/_worktrees/b" {
		t.Fatalf("the line follows the pointer, got %q", got)
	}
	if selected(p) != "main" {
		t.Fatal("the pointer does not move the cursor")
	}
	// The pane gets no event when the pointer leaves it, so a pointer that stops moving lets go.
	for i := 0; i < hoverTicks; i++ {
		p = send(t, p, tickMsg{})
	}
	if got := lastLine(p); got != " ~/src/flo" {
		t.Fatalf("a still pointer gives the line back to the cursor row, got %q", got)
	}
}

func TestMenuRequestNamesTheAgentPaneOnlyForARowThatIsOneAgent(t *testing.T) {
	agent := &model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindWorkspace, WorkspaceID: "w1", Tag: "SRE-923"}
	if got := menuTitle(agent, 3); got != "herdr-menu;workspace;w1;3;w1:p1;SRE-923" {
		t.Errorf("agent row: %q", got)
	}
	holder := &model.Node{ID: "w1", Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w1"}
	if got := menuTitle(holder, 4); got != "herdr-menu;workspace;w1;4;;" {
		t.Errorf("row that holds several agents: %q", got)
	}
}

// stepper is a sidebar pane over a Herdr that has pane at in focus and moves the focus on a jump.
// A jump to a row in lands puts the focus on the pane named there.
type stepper struct {
	*fake
	at    string
	lands map[string]string
}

func (s *stepper) start(t *testing.T) Program {
	t.Helper()
	d := s.deps()
	d.Sidebar = true
	d.Focus = func(n *model.Node) error {
		s.focused = append(s.focused, n.ID)
		if s.focusErr != nil {
			return s.focusErr
		}
		s.at = n.ID
		if pane, ok := s.lands[n.ID]; ok {
			s.at = pane
		}
		return nil
	}
	d.Focused = func() (string, string) { return s.at, "" }
	return start(t, s.fake, d)
}

var (
	stepDown = tea.KeyMsg{Type: tea.KeyF20}
	stepUp   = tea.KeyMsg{Type: tea.KeyF19}
)

// run sends each message, and then every message its commands return.
func run(t *testing.T, p Program, msgs ...tea.Msg) Program {
	t.Helper()
	for _, m := range msgs {
		next, cmd := p.Update(m)
		p = next.(Program)
		for cmd != nil {
			next, cmd = p.Update(cmd())
			p = next.(Program)
		}
	}
	return p
}

func TestSidebarStepJumpsOneRowFromTheFocusedPane(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "a"}
	p := s.start(t)
	if selected(p) != "lead" {
		t.Fatalf("starts on %q", selected(p))
	}
	p = run(t, p, stepDown)
	if selected(p) != "b" || fmt.Sprint(s.focused) != "[b]" {
		t.Fatalf("selected %q, focused %v, want b from the focused pane a", selected(p), s.focused)
	}
	p = run(t, p, stepUp, stepUp)
	if selected(p) != "lead" || fmt.Sprint(s.focused) != "[b a lead]" {
		t.Fatalf("selected %q, focused %v", selected(p), s.focused)
	}
}

func TestSidebarStepIgnoresACursorThatLeftTheFocusedPane(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "lead"}
	p := run(t, s.start(t), stepDown)
	p = send(t, p, key("down"), key("down"))
	if selected(p) != "b1" {
		t.Fatalf("selected %q, want b1", selected(p))
	}
	p = run(t, p, stepDown)
	if selected(p) != "b" {
		t.Fatalf("selected %q, want b: the step starts from the focused pane a", selected(p))
	}
}

func TestSidebarStepSkipsGroupRowsAndWraps(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "solo"}
	p := run(t, s.start(t), stepDown)
	if selected(p) != "lead" {
		t.Fatalf("selected %q, want lead past the two group rows and the end", selected(p))
	}
	p = run(t, p, stepUp)
	if selected(p) != "solo" {
		t.Fatalf("selected %q, want solo past the start and the two group rows", selected(p))
	}
	// An open group shows its rows, and a step stops on them.
	p = send(t, p, key("down"), key("enter"))
	p = run(t, p, stepDown)
	if selected(p) != "repo1" || fmt.Sprint(s.focused) != "[lead solo repo1]" {
		t.Fatalf("selected %q, focused %v", selected(p), s.focused)
	}
}

func TestSidebarStepSkipsTheRowsOfAFoldedParent(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "lead"}
	p := send(t, s.start(t), key("space"))
	p = run(t, p, stepDown)
	if selected(p) != "solo" || fmt.Sprint(s.focused) != "[solo]" {
		t.Fatalf("selected %q, focused %v, want solo", selected(p), s.focused)
	}
}

func TestSidebarStepsThatArriveTogetherEachMoveOneRow(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "lead"}
	p := s.start(t)
	next, first := p.Update(stepDown)
	next, second := next.(Program).Update(stepDown)
	next, third := next.(Program).Update(stepDown)
	if second != nil || third != nil {
		t.Fatal("a step waits for the step before it")
	}
	p = next.(Program)
	for cmd := first; cmd != nil; {
		next, cmd = p.Update(cmd())
		p = next.(Program)
	}
	if selected(p) != "b1" || fmt.Sprint(s.focused) != "[a b b1]" {
		t.Fatalf("selected %q, focused %v", selected(p), s.focused)
	}
	p = run(t, p, stepDown)
	if selected(p) != "solo" {
		t.Fatalf("selected %q, want solo: the next step is not held back", selected(p))
	}
}

func TestSidebarStepStaysOnARowThatPutsTheFocusOnAnotherRow(t *testing.T) {
	// A jump to lead leaves the focus on its pane a, which has a row of its own.
	s := &stepper{fake: &fake{tree: sample()}, at: "a", lands: map[string]string{"lead": "a"}}
	p := run(t, s.start(t), stepUp)
	p = send(t, p, loadedMsg{tree: s.tree, current: "a"})
	if selected(p) != "lead" {
		t.Fatalf("selected %q, want lead: a reload keeps the row of the step", selected(p))
	}
	p = run(t, p, stepUp)
	if selected(p) != "solo" {
		t.Fatalf("selected %q, want solo: the step goes on from lead", selected(p))
	}
}

func TestSidebarStepErrorIsShownAndTheNextStepWorks(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample(), focusErr: errors.New("pane_not_found: gone")}, at: "lead"}
	p := s.start(t)
	next, locate := p.Update(stepDown)
	next, _ = next.(Program).Update(stepDown)
	next, jump := next.(Program).Update(locate())
	next, reload := next.(Program).Update(jump())
	p = next.(Program)
	if !strings.Contains(p.View(), "pane_not_found") || reload == nil {
		t.Fatalf("a failed step shows the error and reloads the tree:\n%s", p.View())
	}
	p = run(t, p, reload())
	if len(s.focused) != 1 {
		t.Fatalf("focused %v: a failed step drops the steps that wait", s.focused)
	}
	s.focusErr = nil
	p = run(t, p, stepDown)
	if selected(p) != "a" || fmt.Sprint(s.focused) != "[a a]" {
		t.Fatalf("selected %q, focused %v, want a again from the focused pane lead", selected(p), s.focused)
	}
}

func TestStepKeysDoNothingOutsideTheSidebar(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	next, cmd := p.Update(stepDown)
	if cmd != nil || selected(next.(Program)) != "lead" {
		t.Fatal("a popup pane has no step keys")
	}
}

// keys records the keys Bubble Tea reads and quits after two.
type keys []string

func (k keys) Init() tea.Cmd { return nil }
func (k keys) View() string  { return "" }
func (k keys) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m, ok := msg.(tea.KeyMsg); ok {
		if k = append(k, m.String()); len(k) == 2 {
			return k, tea.Quit
		}
	}
	return k, nil
}

func TestStepKeysAreTheBytesHerdrSends(t *testing.T) {
	in := strings.NewReader("\x1b[19;2~\x1b[18;2~")
	got, err := tea.NewProgram(keys{}, tea.WithInput(in), tea.WithOutput(io.Discard)).Run()
	if err != nil {
		t.Fatal(err)
	}
	if want := (keys{stepNext, stepPrevious}); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("read %v, want %v", got, want)
	}
}

func TestSidebarLastLineSaysWhatABlockedRowWaitsFor(t *testing.T) {
	blocked := &model.Node{ID: "b", Label: "b", Status: model.Blocked, Shown: model.KindWorkspace,
		Where: "ops/_worktrees/b", Blocker: "Do you want to run this command? / 1. Yes / 2. No"}
	idle := &model.Node{ID: "i", Label: "i", Status: model.Idle, Shown: model.KindWorkspace,
		Where: "~/src/flo", Blocker: "left over"}
	f := &fake{tree: model.Tree{Roots: []*model.Node{blocked, idle}}}
	p := sidebar(t, f)
	p = send(t, p, tea.WindowSizeMsg{Width: 40, Height: 8})
	if got := lastLine(p); got != " Do you want to run this command? / 1. …" {
		t.Fatalf("a blocked row shows what it waits for, got %q", got)
	}
	p = send(t, p, key("j"))
	if got := lastLine(p); got != " ~/src/flo" {
		t.Fatalf("a row that is not blocked shows where it works, got %q", got)
	}
}

func TestMenuTitleCarriesTheAnswersOfABlockedRow(t *testing.T) {
	n := &model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindWorkspace, WorkspaceID: "w1", Tag: "SRE-9",
		Dialog: "d1a109", Answers: []model.Answer{{Key: "1", Text: "Yes"}, {Key: "2", Text: `No "thanks"`}}}
	want := `herdr-menu2;{"t":"workspace","i":"w1","p":"w1:p1","l":"SRE-9","d":"d1a109","a":[["1","Yes"],["2","No \"thanks\""]]}`
	if got := menuTitle(n, 1); got != want {
		t.Fatalf("title = %s\nwant    %s", got, want)
	}
	// A row that holds several agents names no pane, so it can carry no answers.
	n.Focus = model.KindWorkspace
	if got := menuTitle(n, 2); got != "herdr-menu;workspace;w1;2;;SRE-9" {
		t.Fatalf("title = %s", got)
	}
}

func TestMenuTitleFitsThePaneTitleLimit(t *testing.T) {
	long := strings.Repeat("a long answer text ", 3)
	n := &model.Node{ID: "w12:p34", Focus: model.KindPane, Shown: model.KindWorkspace, WorkspaceID: "w12", Tag: "SRE-12345",
		Dialog: "0123456789abcdef"}
	for i := 1; i <= 6; i++ {
		n.Answers = append(n.Answers, model.Answer{Key: fmt.Sprint(i), Text: long})
	}
	got := menuTitle(n, 1)
	if !strings.HasPrefix(got, "herdr-menu2;") {
		t.Fatalf("six answers still fit when their text is cut, got %s", got)
	}
	if n := len([]rune(got)); n > maxMenuTitle {
		t.Fatalf("title is %d characters, more than Herdr keeps (%d)", n, maxMenuTitle)
	}
	for i := 1; i <= 6; i++ {
		if !strings.Contains(got, fmt.Sprintf(`["%d","a `, i)) {
			t.Fatalf("answer %d is missing or lost its start: %s", i, got)
		}
	}
	// A request that cannot fit carries no answers. A cut request would not parse.
	n.WorkspaceID = strings.Repeat("w", 300)
	if got := menuTitle(n, 1); strings.HasPrefix(got, "herdr-menu2;") {
		t.Fatalf("an oversize request must fall back, got %d characters", len(got))
	}
}
