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

func TestProgramFoldSavesAndEnterFocusesAnEffort(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	p = send(t, p, key("space"))
	if len(p.rows) != 4 || !p.rows[0].Folded {
		t.Fatalf("space folds the selected effort, rows:\n%s", show(p.rows))
	}
	if !f.saved["lead"] {
		t.Fatalf("the fold is saved, got %v", f.saved)
	}
	p = send(t, p, key("down"))
	before := len(p.rows)
	p = send(t, p, key("space"))
	if selected(p) != "solo" || len(p.rows) != before {
		t.Fatalf("space on an effort with no panes does nothing, rows:\n%s", show(p.rows))
	}
	_, cmd := p.Update(key("enter"))
	cmd()
	if len(f.focused) != 1 || f.focused[0] != "solo" {
		t.Fatalf("enter on an effort focuses it, focused %v", f.focused)
	}
}

func TestProgramQuit(t *testing.T) {
	f := &fake{tree: sample()}
	p := start(t, f, f.deps())
	if next, _ := p.Update(key("a")); len(next.(Program).rows) != 7 {
		t.Fatal("a is no longer a key")
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
	p = send(t, p, loadedMsg{tree: model.Tree{Roots: []*model.Node{effort("lead")}}})
	if p.cursor != 0 || selected(p) != "lead" {
		t.Fatalf("cursor %d on %q, want the last row", p.cursor, selected(p))
	}
	// Nothing left at all: no panic on any key.
	p = send(t, p, loadedMsg{tree: model.Tree{}}, key("down"), key("space"), key("enter"))
	if !strings.Contains(p.View(), "no workspaces") {
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
		roots = append(roots, effort(id))
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
	p = send(t, p, click(12, 2))
	next, cmd := p.Update(release(12, 2))
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
	f := &fake{tree: model.Tree{Roots: []*model.Node{effort("solo")}}}
	p := sidebar(t, f)
	p = send(t, p, loadedMsg{tree: f.tree, current: "late"})
	grown := model.Tree{Roots: []*model.Node{effort("solo"), effort("late")}}
	p = send(t, p, loadedMsg{tree: grown, current: "late"})
	if selected(p) != "late" {
		t.Fatalf("selected %q, want late", selected(p))
	}
}

func repos() model.Tree {
	n := func(id, repo string) *model.Node {
		e := effort(id)
		e.Repo = repo
		return e
	}
	return model.Tree{Roots: []*model.Node{n("a", "iam"), n("b", "ops"), n("c", "api")}}
}

func TestSidebarClickOnARepoLineSelectsItsRow(t *testing.T) {
	f := &fake{tree: repos()}
	p := sidebar(t, f)
	// Lines: a, iam, gap, b, ops, gap, c, api. A gap belongs to the row below it.
	for y, want := range []string{"a", "a", "b", "b", "b", "c", "c", "c"} {
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
	if !strings.Contains(v, "  c") || !strings.Contains(v, "api") {
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
	if got := fmt.Sprint(cmd()); got != "herdr-menu;workspace;w-b;1;;" {
		t.Fatalf("request = %q", got)
	}
	if len(f.focused) != 0 {
		t.Fatal("a right-click must not jump")
	}
	_, cmd = next.(Program).Update(rightClick(12, 4))
	if got := fmt.Sprint(cmd()); got != "herdr-menu;workspace;w-b;2;;" {
		t.Fatalf("second request = %q", got)
	}
}

func TestMenuTargetFollowsWhatTheRowShows(t *testing.T) {
	for _, c := range []struct {
		n    model.Node
		want string
	}{
		{model.Node{ID: "w1", Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w1"}, "workspace;w1"},
		{model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w1"}, "pane;w1:p1"},
		{model.Node{ID: "w1:p2", Focus: model.KindPane, Shown: model.KindShell, WorkspaceID: "w1"}, "pane;w1:p2"},
	} {
		if got := menuTarget(&c.n); got != c.want {
			t.Errorf("%+v: target %q, want %q", c.n, got, c.want)
		}
	}
}

func places() model.Tree {
	e := effort("main", agent("a", model.Idle), agent("b", model.Idle))
	e.Where = "~/src/flo"
	e.Children[0].Where, e.Children[1].Where = "iam/_worktrees/a", "ops/_worktrees/b"
	return model.Tree{Roots: []*model.Node{e}}
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

func TestMenuRequestNamesThePaneOnlyForAnAgentRow(t *testing.T) {
	a := &model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w1", Tag: "SRE-923"}
	if got := menuTitle(a, 3); got != "herdr-menu;pane;w1:p1;3;w1:p1;SRE-923" {
		t.Errorf("agent row: %q", got)
	}
	s := &model.Node{ID: "w1:p2", Focus: model.KindPane, Shown: model.KindShell, WorkspaceID: "w1"}
	if got := menuTitle(s, 4); got != "herdr-menu;pane;w1:p2;4;;" {
		t.Errorf("shell row: %q", got)
	}
	e := &model.Node{ID: "w1", Focus: model.KindWorkspace, Shown: model.KindWorkspace, WorkspaceID: "w1"}
	if got := menuTitle(e, 5); got != "herdr-menu;workspace;w1;5;;" {
		t.Errorf("effort row: %q", got)
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
	if selected(p) != "b1" || fmt.Sprint(s.focused) != "[b a b1]" {
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

func TestSidebarStepSkipsEffortAndShellRowsAndWraps(t *testing.T) {
	s := &stepper{fake: &fake{tree: sample()}, at: "b1"}
	p := run(t, s.start(t), stepDown)
	if selected(p) != "a" {
		t.Fatalf("selected %q, want a past solo, repo1, sh1, the end and lead", selected(p))
	}
	p = run(t, p, stepUp)
	if selected(p) != "b1" || fmt.Sprint(s.focused) != "[a b1]" {
		t.Fatalf("selected %q, focused %v", selected(p), s.focused)
	}
}

func TestSidebarStepSkipsTheRowsOfAFoldedEffort(t *testing.T) {
	tree := model.Tree{Roots: []*model.Node{
		effort("x", agent("x1", model.Idle)), effort("y", agent("y1", model.Idle)), effort("z", agent("z1", model.Idle)),
	}}
	s := &stepper{fake: &fake{tree: tree}, at: "x1"}
	p := send(t, s.start(t), key("down"), key("down"), key("space"))
	if !p.rows[2].Folded {
		t.Fatalf("y is folded, rows:\n%s", show(p.rows))
	}
	p = run(t, p, stepDown)
	if selected(p) != "z1" || fmt.Sprint(s.focused) != "[z1]" {
		t.Fatalf("selected %q, focused %v, want z1", selected(p), s.focused)
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
	if selected(p) != "a" {
		t.Fatalf("selected %q, want a: the next step is not held back", selected(p))
	}
}

func TestSidebarStepStaysOnARowThatPutsTheFocusOnAnotherRow(t *testing.T) {
	// A jump to b1 leaves the focus on b, which has a row of its own.
	s := &stepper{fake: &fake{tree: sample()}, at: "a", lands: map[string]string{"b1": "b"}}
	p := run(t, s.start(t), stepUp)
	p = send(t, p, loadedMsg{tree: s.tree, current: "b"})
	if selected(p) != "b1" {
		t.Fatalf("selected %q, want b1: a reload keeps the row of the step", selected(p))
	}
	p = run(t, p, stepUp)
	if selected(p) != "b" {
		t.Fatalf("selected %q, want b: the step goes on from b1", selected(p))
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
	n := &model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w1", Tag: "SRE-9",
		Dialog: "d1a109", Answers: []model.Answer{{Key: "1", Text: "Yes"}, {Key: "2", Text: `No "thanks"`}}}
	want := `herdr-menu2;{"t":"pane","i":"w1:p1","p":"w1:p1","l":"SRE-9","d":"d1a109","a":[["1","Yes"],["2","No \"thanks\""]]}`
	if got := menuTitle(n, 1); got != want {
		t.Fatalf("title = %s\nwant    %s", got, want)
	}
	// An effort row names no pane, so it can carry no answers.
	n.ID, n.Focus, n.Shown = "w1", model.KindWorkspace, model.KindWorkspace
	if got := menuTitle(n, 2); got != "herdr-menu;workspace;w1;2;;SRE-9" {
		t.Fatalf("title = %s", got)
	}
}

func TestMenuTitleFitsThePaneTitleLimit(t *testing.T) {
	long := strings.Repeat("a long answer text ", 3)
	n := &model.Node{ID: "w12:p34", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w12", Tag: "SRE-12345",
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
	n.ID = strings.Repeat("w", 300)
	if got := menuTitle(n, 1); strings.HasPrefix(got, "herdr-menu2;") {
		t.Fatalf("an oversize request must fall back, got %d characters", len(got))
	}
}

func TestMenuTitleCarriesNoMoreAnswersThanTheClientShows(t *testing.T) {
	n := &model.Node{ID: "w1:p1", Focus: model.KindPane, Shown: model.KindPane, WorkspaceID: "w1", Dialog: "0123456789abcdef"}
	for i := 1; i <= 9; i++ {
		n.Answers = append(n.Answers, model.Answer{Key: fmt.Sprint(i), Text: "a short answer"})
	}
	got := menuTitle(n, 1)
	if !strings.Contains(got, `["6","a short answer"]`) || strings.Contains(got, `["7",`) {
		t.Fatalf("want six answers with their whole text, got %s", got)
	}
}

func release(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

func drag(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
}

// spaces is three efforts, one space each. The second holds two agents.
func spaces() model.Tree {
	n := func(id, space string, kids ...*model.Node) *model.Node {
		e := effort(id, kids...)
		e.WorkspaceID = space
		return e
	}
	x, y := agent("x", model.Idle), agent("y", model.Idle)
	x.WorkspaceID, y.WorkspaceID = "w2", "w2"
	return model.Tree{Roots: []*model.Node{n("one", "w1"), n("two", "w2", x, y), n("three", "w3")}}
}

// mover records each move as "<space><<before>".
type mover struct {
	fake
	moves []string
	err   error
}

func (m *mover) start(t *testing.T) Program {
	t.Helper()
	m.tree = spaces()
	d := m.deps()
	d.Sidebar = true
	d.Move = func(space, before string) error {
		m.moves = append(m.moves, space+"<"+before)
		return m.err
	}
	return start(t, &m.fake, d)
}

// on puts the cursor on a row.
func on(t *testing.T, p Program, id string) Program {
	t.Helper()
	for i, r := range p.rows {
		if r.Node.ID == id {
			p.cursor = i
			p.settle()
			return p
		}
	}
	t.Fatalf("no row %q", id)
	return p
}

func line(t *testing.T, p Program, id string) int {
	t.Helper()
	for i, r := range p.rows {
		if r.Node.ID == id {
			top, _ := p.span(i)
			return top + r.Height() - 1
		}
	}
	t.Fatalf("no row %q", id)
	return 0
}

func TestMoveKeysMoveAnEffort(t *testing.T) {
	for _, c := range []struct{ row, key, want string }{
		{"one", "shift+down", "w2<w1"},
		{"three", "shift+up", "w3<w2"},
		{"two", "shift+up", "w2<w1"},
		{"two", "shift+down", "w3<w2"},
	} {
		m := &mover{}
		p := on(t, m.start(t), c.row)
		k := tea.KeyMsg{Type: tea.KeyShiftDown}
		if c.key == "shift+up" {
			k = tea.KeyMsg{Type: tea.KeyShiftUp}
		}
		run(t, p, k)
		if len(m.moves) != 1 || m.moves[0] != c.want {
			t.Errorf("%s on %s moved %v, want [%s]", c.key, c.row, m.moves, c.want)
		}
	}
}

func TestMoveKeysDoNothingAtTheEndsAndOnAPane(t *testing.T) {
	for _, c := range []struct{ row, key string }{
		{"one", "up"}, {"three", "down"}, {"x", "up"}, {"x", "down"}, {"y", "up"},
	} {
		m := &mover{}
		p := on(t, m.start(t), c.row)
		k := tea.KeyMsg{Type: tea.KeyShiftDown}
		if c.key == "up" {
			k = tea.KeyMsg{Type: tea.KeyShiftUp}
		}
		if _, cmd := p.Update(k); cmd != nil || len(m.moves) != 0 {
			t.Errorf("shift+%s on %s moved %v", c.key, c.row, m.moves)
		}
	}
}

func TestMoveReloadsTheTreeAndShowsAnError(t *testing.T) {
	m := &mover{err: errors.New("workspace_move_block_failed")}
	p := on(t, m.start(t), "one")
	p = run(t, p, tea.KeyMsg{Type: tea.KeyShiftDown})
	if !strings.Contains(p.View(), "workspace_move_block_failed") {
		t.Fatalf("the error of a move is shown:\n%s", p.View())
	}
}

func TestDragPutsAnEffortWhereItIsDropped(t *testing.T) {
	for _, c := range []struct{ from, to, want string }{
		// Down: the effort goes below the effort it is dropped on.
		{"one", "two", "w1<w3"},
		{"one", "three", "w1<"},
		// A drop on a pane row is a drop on the effort that holds it.
		{"one", "y", "w1<w3"},
		// Up: the effort takes the place of the effort it is dropped on.
		{"three", "one", "w3<w1"},
		{"three", "x", "w3<w2"},
	} {
		m := &mover{}
		p := m.start(t)
		from, to := line(t, p, c.from), line(t, p, c.to)
		p = run(t, p, click(12, from), drag(12, to), release(12, to))
		if len(m.moves) != 1 || m.moves[0] != c.want {
			t.Errorf("drag %s to %s moved %v, want [%s]", c.from, c.to, m.moves, c.want)
		}
		if len(m.focused) != 0 {
			t.Errorf("drag %s to %s jumped to %v", c.from, c.to, m.focused)
		}
	}
}

func TestDragOfAPaneRowDoesNothing(t *testing.T) {
	for _, c := range []struct{ from, to string }{{"x", "one"}, {"x", "three"}, {"y", "x"}} {
		m := &mover{}
		p := m.start(t)
		from, to := line(t, p, c.from), line(t, p, c.to)
		p = run(t, p, click(12, from), drag(12, to), release(12, to))
		if len(m.moves) != 0 || len(m.focused) != 0 {
			t.Errorf("drag %s to %s moved %v and jumped to %v", c.from, c.to, m.moves, m.focused)
		}
	}
}

func TestDragSaysWhichRowMoves(t *testing.T) {
	m := &mover{}
	p := m.start(t)
	p = send(t, p, click(12, line(t, p, "one")), drag(12, line(t, p, "three")))
	if got := lastLine(p); !strings.Contains(got, "move one") {
		t.Fatalf("the last line of a drag is %q", got)
	}
	p = send(t, p, release(12, line(t, p, "three")))
	if got := lastLine(p); strings.Contains(got, "move one") {
		t.Fatalf("the last line after the drop is %q", got)
	}
}

// opener records the links the pane opens.
type opener struct {
	fake
	opened []string
	err    error
}

func (o *opener) start(t *testing.T) Program {
	t.Helper()
	n := func(id, link string) *model.Node {
		return &model.Node{ID: id, Label: id, Status: model.Idle, Shown: model.KindWorkspace, WorkspaceID: "w-" + id, Tag: "T-" + id, Link: link}
	}
	o.tree = model.Tree{Roots: []*model.Node{n("linked", "https://example.com/1"), n("bare", "")}}
	d := o.deps()
	d.Sidebar = true
	d.Open = func(link string) error {
		o.opened = append(o.opened, link)
		return o.err
	}
	return start(t, &o.fake, d)
}

func ctrlClick(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Ctrl: true}
}

func TestCtrlClickOpensTheLinkOfARow(t *testing.T) {
	o := &opener{}
	p := o.start(t)
	y := line(t, p, "linked")
	run(t, p, ctrlClick(12, y), release(12, y))
	if len(o.opened) != 1 || o.opened[0] != "https://example.com/1" {
		t.Fatalf("opened %v", o.opened)
	}
	if len(o.focused) != 0 {
		t.Fatalf("a click that opens a link must not jump, focused %v", o.focused)
	}
}

func TestCtrlClickOnARowWithNoLinkIsAClick(t *testing.T) {
	o := &opener{}
	p := o.start(t)
	y := line(t, p, "bare")
	run(t, p, ctrlClick(12, y), release(12, y))
	if len(o.opened) != 0 || len(o.focused) != 1 || o.focused[0] != "bare" {
		t.Fatalf("opened %v, focused %v", o.opened, o.focused)
	}
}

func TestOKeyOpensTheLinkOfTheCursorRow(t *testing.T) {
	o := &opener{err: errors.New("no browser")}
	p := o.start(t)
	p = run(t, p, key("o"))
	if len(o.opened) != 1 || o.opened[0] != "https://example.com/1" {
		t.Fatalf("opened %v", o.opened)
	}
	if !strings.Contains(p.View(), "no browser") {
		t.Fatalf("the error of an open is shown:\n%s", p.View())
	}
	o.opened = nil
	if _, cmd := on(t, p, "bare").Update(key("o")); cmd != nil || len(o.opened) != 0 {
		t.Fatalf("a row with no link opens nothing, opened %v", o.opened)
	}
}

func TestSidebarFoldsAnAgentWithWorkers(t *testing.T) {
	f := &fake{tree: sample()}
	p := sidebar(t, f)
	// Rows: lead, a, b, b1. The fold mark of b is left of its dot.
	next, cmd := p.Update(click(3, 2))
	p = next.(Program)
	if cmd != nil || len(f.focused) != 0 {
		t.Fatal("a click on the fold mark of an agent must not jump")
	}
	if !p.rows[2].Folded || !f.saved["b"] {
		t.Fatalf("the click folds b and saves it, rows:\n%s", show(p.rows))
	}
	p = send(t, p, key("space"))
	if selected(p) != "b" || p.rows[2].Folded {
		t.Fatalf("space on b opens it again, rows:\n%s", show(p.rows))
	}
	p = send(t, on(t, p, "a"), key("space"))
	if p.rows[1].Folded {
		t.Fatal("an agent with no workers does not fold")
	}
}
