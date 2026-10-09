package view

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Deps is what the pane needs from outside: the tree, the jump, and where folds are kept.
type Deps struct {
	Load       func() (model.Tree, error)
	Focus      func(*model.Node) error
	Save       func(folds map[string]bool, t model.Tree)
	OriginPane string
	OriginTab  string
}

type (
	loadedMsg struct {
		tree model.Tree
		err  error
	}
	focusedMsg struct{ err error }
	tickMsg    struct{}
)

// Lines of the view that are not tree rows: title, blank, footer.
const chrome = 3

type Program struct {
	deps   Deps
	theme  Theme
	state  State
	tree   model.Tree
	rows   []Row
	sel    string
	cursor int
	offset int
	width  int
	height int
	placed bool
	err    error
}

func New(d Deps, folds map[string]bool, th Theme) Program {
	if folds == nil {
		folds = map[string]bool{}
	}
	return Program{deps: d, theme: th, state: State{Folded: folds}, width: 80, height: 24}
}

func (p Program) Init() tea.Cmd { return tea.Batch(p.load(), tick()) }

func (p Program) load() tea.Cmd {
	return func() tea.Msg {
		t, err := p.deps.Load()
		return loadedMsg{tree: t, err: err}
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (p Program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
		p.scroll()
	case tickMsg:
		return p, tea.Batch(p.load(), tick())
	case loadedMsg:
		p.err = m.err
		if m.err == nil {
			p.tree = m.tree
			p.rebuild()
			if !p.placed {
				p.placed = true
				p.place()
			}
		}
	case focusedMsg:
		if m.err == nil {
			return p, tea.Quit
		}
		p.err = m.err
		return p, p.load()
	case tea.KeyMsg:
		switch m.String() {
		case "q", "esc", "ctrl+c":
			return p, tea.Quit
		case "up", "k":
			p.move(-1)
		case "down", "j":
			p.move(1)
		case " ":
			p.toggle()
		case "a":
			p.state.Attention = !p.state.Attention
			p.rebuild()
		case "enter":
			if len(p.rows) == 0 {
				break
			}
			n := p.rows[p.cursor].Node
			if n.Shown == model.KindGroup {
				p.toggle()
				break
			}
			focus := p.deps.Focus
			return p, func() tea.Msg { return focusedMsg{err: focus(n)} }
		}
	}
	return p, nil
}

// rebuild recomputes the rows and keeps the cursor on the node it was on.
func (p *Program) rebuild() {
	p.rows = Rows(p.tree, p.state)
	for i, r := range p.rows {
		if r.Node.ID == p.sel {
			p.cursor = i
			break
		}
	}
	p.settle()
}

// settle keeps the cursor on a real row and the view on the cursor.
func (p *Program) settle() {
	p.cursor = max(0, min(p.cursor, len(p.rows)-1))
	p.sel = ""
	if len(p.rows) > 0 {
		p.sel = p.rows[p.cursor].Node.ID
	}
	p.scroll()
}

func (p *Program) scroll() {
	body := max(1, p.height-chrome)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+body {
		p.offset = p.cursor - body + 1
	}
	p.offset = max(0, min(p.offset, max(0, len(p.rows)-body)))
}

func (p *Program) move(by int) {
	p.cursor += by
	p.settle()
}

// place puts the cursor on the agent the pane was opened from, else on that agent's tab.
func (p *Program) place() {
	for i, r := range p.rows {
		if r.Node.ID == p.deps.OriginPane {
			p.cursor = i
			p.settle()
			return
		}
	}
	for i, r := range p.rows {
		if p.deps.OriginTab != "" && r.Node.TabID == p.deps.OriginTab {
			p.cursor = i
			p.settle()
			return
		}
	}
}

func (p *Program) toggle() {
	if len(p.rows) == 0 || !p.rows[p.cursor].HasChildren {
		return
	}
	r := p.rows[p.cursor]
	p.state.Folded[r.Node.ID] = !r.Folded
	p.rebuild()
	if p.deps.Save != nil {
		p.deps.Save(p.state.Folded, p.tree)
	}
}

func (p Program) View() string {
	var b strings.Builder
	title := " Agents by supervisor"
	if p.state.Attention {
		title += "  (attention)"
	}
	b.WriteString(p.theme.paint(title, bold) + "\n\n")
	if len(p.rows) == 0 {
		b.WriteString("   no agents\n")
	}
	lines := Render(p.rows, p.width, p.cursor, p.theme)
	body := max(1, p.height-chrome)
	for _, line := range lines[min(p.offset, len(lines)):min(len(lines), p.offset+body)] {
		b.WriteString(line + "\n")
	}
	foot := " ↑↓ move  ⏎ jump  ␣ fold  a attention  q close"
	if p.err != nil {
		foot = " error: " + p.err.Error()
	}
	b.WriteString(p.theme.paint(cut(foot, p.width), faint))
	return b.String()
}
