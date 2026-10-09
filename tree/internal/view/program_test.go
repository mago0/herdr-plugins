package view

import (
	"errors"
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
