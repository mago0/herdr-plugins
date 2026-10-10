package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Deps is what the pane needs from outside: the tree, the jump, and where folds are kept.
type Deps struct {
	Load       func() (model.Tree, error)
	Focus      func(*model.Node) error
	Save       func(folds map[string]bool, t model.Tree)
	OriginPane string
	OriginTab  string
	// Sidebar is true when the pane is a section of the Herdr sidebar: it stays open, shows
	// only rows, and keeps the cursor on the pane Herdr has in focus.
	Sidebar bool
	// Current returns the pane and tab Herdr has in focus, as of the last Load.
	Current func() (pane, tab string)
	// Focused asks Herdr for the pane and tab it has in focus now. Both are empty when it fails.
	Focused func() (pane, tab string)
}

type (
	loadedMsg struct {
		tree model.Tree
		err  error
		// current and currentTab are the pane and tab Herdr has in focus.
		current, currentTab string
	}
	focusedMsg struct {
		err error
		// row is the row a step jumped to, and pane is the pane Herdr has in focus after it.
		row, pane string
	}
	// stepMsg starts a step from the pane and tab Herdr has in focus.
	stepMsg struct {
		by        int
		pane, tab string
	}
	tickMsg struct{}
)

// Herdr sends these keys to a sidebar pane to move its focus one row down or up the tree.
const (
	stepNext     = "f20"
	stepPrevious = "f19"
)

// Lines of a popup view that are not tree rows: title and blank above, footer below.
const (
	popupHeader = 2
	popupChrome = popupHeader + 1
	wheelRows   = 3
	// hoverTicks is how many ticks a pointer that does not move keeps the last line.
	hoverTicks = 5
)

type Program struct {
	deps   Deps
	theme  Theme
	state  State
	tree   model.Tree
	rows   []Row
	sel    string
	cursor int
	// offset is the first line in view. A row with a repo takes two lines.
	offset int
	// hover is the row under the pointer, and still counts the ticks since the pointer moved.
	hover string
	still int
	// menus counts the menu requests, so each one is a new terminal title.
	menus  int
	width  int
	height int
	placed bool
	// current is the focused pane the cursor last moved to.
	current string
	// free is true while the wheel has moved the view away from the cursor.
	free bool
	// stepping is true from a step key until its jump ends. queued is the steps that wait for
	// it: more than zero is down, less is up.
	stepping bool
	queued   int
	// stepped is the row of the last step, while the cursor is still there.
	stepped string
	err     error
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
		m := loadedMsg{tree: t, err: err}
		if err == nil && p.deps.Current != nil {
			m.current, m.currentTab = p.deps.Current()
		}
		return m
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
		if p.still++; p.still >= hoverTicks {
			p.hover = ""
		}
		return p, tea.Batch(p.load(), tick())
	case loadedMsg:
		p.err = m.err
		if m.err == nil {
			p.tree = m.tree
			p.rebuild()
			if !p.placed {
				p.placed = true
				p.place(p.deps.OriginPane, p.deps.OriginTab)
			}
			// A focused pane with no row yet is tried again on the next load.
			if p.deps.Sidebar && m.current != "" && m.current != p.current && p.place(m.current, m.currentTab) {
				p.current = m.current
				p.free = false
				p.hover = ""
			}
		}
	case focusedMsg:
		if m.err == nil && !p.deps.Sidebar {
			return p, tea.Quit
		}
		p.err = m.err
		if m.row != "" {
			return p.stepDone(m)
		}
		if m.err == nil {
			return p, nil
		}
		return p, p.load()
	case stepMsg:
		// The cursor is the start when the last step left it there and the focus did not move.
		held := m.pane == p.current && p.stepped != "" && p.stepped == p.sel
		if m.pane != "" && !held && p.place(m.pane, m.tab) {
			p.current = m.pane
		}
		return p.advance(m.by)
	case tea.MouseMsg:
		return p.mouse(m)
	case tea.KeyMsg:
		switch m.String() {
		case "q", "esc", "ctrl+c":
			if p.deps.Sidebar {
				break
			}
			return p, tea.Quit
		case "up", "k":
			p.move(-1)
		case "down", "j":
			p.move(1)
		case stepNext:
			return p.step(1)
		case stepPrevious:
			return p.step(-1)
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
			return p, p.jump(n)
		}
	}
	return p, nil
}

func (p Program) jump(n *model.Node) tea.Cmd {
	focus := p.deps.Focus
	return func() tea.Msg { return focusedMsg{err: focus(n)} }
}

// step moves the focus of Herdr one row down or up from the row of the pane it has in focus.
// A step that comes while one is in work waits for it, so each one starts from the row before.
func (p Program) step(by int) (tea.Model, tea.Cmd) {
	if !p.deps.Sidebar {
		return p, nil
	}
	if p.stepping {
		p.queued += by
		return p, nil
	}
	p.stepping = true
	focused := p.deps.Focused
	if focused == nil {
		return p.advance(by)
	}
	return p, func() tea.Msg {
		m := stepMsg{by: by}
		m.pane, m.tab = focused()
		return m
	}
}

// advance puts the cursor on the next row in the direction of by that can take the focus, past
// the ends of the tree, and jumps to it. Group rows cannot take the focus.
func (p Program) advance(by int) (tea.Model, tea.Cmd) {
	for i := 1; i <= len(p.rows); i++ {
		at := ((p.cursor+by*i)%len(p.rows) + len(p.rows)) % len(p.rows)
		n := p.rows[at].Node
		if n.Shown == model.KindGroup {
			continue
		}
		p.cursor, p.free, p.hover = at, false, ""
		p.settle()
		focus, focused := p.deps.Focus, p.deps.Focused
		return p, func() tea.Msg {
			m := focusedMsg{err: focus(n), row: n.ID}
			if m.err == nil && focused != nil {
				m.pane, _ = focused()
			}
			return m
		}
	}
	p.stepping, p.queued = false, 0
	return p, nil
}

// stepDone ends the jump of a step and starts the next step that waits.
func (p Program) stepDone(m focusedMsg) (tea.Model, tea.Cmd) {
	if m.err != nil {
		p.stepping, p.queued, p.stepped = false, 0, ""
		return p, p.load()
	}
	p.stepped = m.row
	// The cursor stays on the row of the step when Herdr puts the focus on another row.
	if m.pane != "" {
		p.current = m.pane
	}
	if p.queued == 0 {
		p.stepping = false
		return p, nil
	}
	by := 1
	if p.queued < 0 {
		by = -1
	}
	p.queued -= by
	return p.advance(by)
}

// chrome is the count of lines above the rows and of all lines that are not rows.
func (p Program) chrome() (header, total int) {
	if !p.deps.Sidebar {
		return popupHeader, popupChrome
	}
	// The last line says where the row works.
	if p.err != nil {
		return 0, 2
	}
	return 0, 1
}

// where is the last line of a sidebar pane, for the row under the pointer, or the row under the
// cursor when the pointer is on no row: what a blocked row waits for, else where the row works.
func (p Program) where() string {
	var n *model.Node
	for _, r := range p.rows {
		if p.hover != "" && r.Node.ID == p.hover {
			n = r.Node
		}
	}
	if n == nil && len(p.rows) > 0 {
		n = p.rows[p.cursor].Node
	}
	if n == nil {
		return ""
	}
	// What a blocked agent waits for is more urgent than where it works.
	if n.Status == model.Blocked && n.Blocker != "" {
		return " " + p.theme.paint(cut(n.Blocker, p.width-1), faint)
	}
	if n.Where == "" {
		return ""
	}
	return " " + p.theme.paint(cutLeft(n.Where, p.width-1), faint)
}

// mouse handles a click on a row and the wheel. A click on the part before the state dot
// folds a row that has children; a click elsewhere on a row jumps to it.
func (p Program) mouse(m tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.Action == tea.MouseActionMotion {
		p.hover, p.still = "", 0
		if i, ok := p.rowAt(m.Y); ok {
			p.hover = p.rows[i].Node.ID
		}
	}
	if m.Action != tea.MouseActionPress {
		return p, nil
	}
	switch m.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		by := wheelRows
		if m.Button == tea.MouseButtonWheelUp {
			by = -wheelRows
		}
		p.free = true
		p.offset += by
		p.scroll()
	case tea.MouseButtonRight:
		i, ok := p.rowAt(m.Y)
		if !ok || !p.deps.Sidebar {
			break
		}
		n := p.rows[i].Node
		if menuTarget(n) == "" {
			break
		}
		p.menus++
		return p, tea.SetWindowTitle(menuTitle(n, p.menus))
	case tea.MouseButtonLeft:
		i, ok := p.rowAt(m.Y)
		if !ok {
			break
		}
		p.cursor = i
		p.free = false
		p.settle()
		r := p.rows[i]
		if r.Node.Shown == model.KindGroup || (r.HasChildren && m.X < 1+lipgloss.Width(lead(r))) {
			p.toggle()
			break
		}
		return p, p.jump(r.Node)
	}
	return p, nil
}

// menuRequest starts the terminal title that asks Herdr to open its menu for a row. A pane has
// no other way to reach the Herdr client that draws it.
const menuRequest = "herdr-menu"

// answerMenuRequest starts the title form that is JSON and can carry the answers of a dialog.
const answerMenuRequest = "herdr-menu2"

// menuTitle is the request for the menu of a row. Its last two parts are the pane of the row's
// agent and the label that pane has, which Herdr needs to set a label. The pane is empty for a
// row that holds several agents.
func menuTitle(n *model.Node, count int) string {
	pane := ""
	if n.Focus == model.KindPane {
		pane = n.ID
	}
	if pane != "" && len(n.Answers) > 0 {
		if title, ok := answerMenuTitle(n, pane); ok {
			return title
		}
	}
	return fmt.Sprintf("%s;%s;%d;%s;%s", menuRequest, menuTarget(n), count, pane, n.Tag)
}

// answerMenuTitle is the request for the menu of a blocked row, with the answers of its dialog.
func answerMenuTitle(n *model.Node, pane string) (string, bool) {
	kind, id, _ := strings.Cut(menuTarget(n), ";")
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(struct {
		Kind    string         `json:"kind"`
		ID      string         `json:"id"`
		Pane    string         `json:"agent_pane"`
		Label   string         `json:"label"`
		Answers []model.Answer `json:"answers"`
	}{kind, id, pane, n.Tag, n.Answers})
	if err != nil {
		return "", false
	}
	return answerMenuRequest + ";" + strings.TrimSpace(b.String()), true
}

// menuTarget names what a row stands for on screen, as "<kind>;<id>". A group row has no menu.
func menuTarget(n *model.Node) string {
	switch n.Shown {
	case model.KindWorkspace:
		return "workspace;" + n.WorkspaceID
	case model.KindTab:
		return "tab;" + n.TabID
	case model.KindPane:
		return "pane;" + n.ID
	}
	return ""
}

// rowAt returns the row drawn on line y of the pane.
func (p Program) rowAt(y int) (int, bool) {
	header, _ := p.chrome()
	line := p.offset + y - header
	if y < header || line < 0 {
		return 0, false
	}
	for i, r := range p.rows {
		if line < r.Height() {
			return i, true
		}
		line -= r.Height()
	}
	return 0, false
}

// span returns the first line of row i and the count of lines of all rows.
func (p Program) span(i int) (top, total int) {
	for j, r := range p.rows {
		if j == i {
			top = total
		}
		total += r.Height()
	}
	return top, total
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
	_, chrome := p.chrome()
	body := max(1, p.height-chrome)
	top, total := p.span(p.cursor)
	if !p.free && len(p.rows) > 0 {
		if bottom := top + p.rows[p.cursor].Height(); bottom > p.offset+body {
			p.offset = bottom - body
		}
		p.offset = min(p.offset, top)
	}
	p.offset = max(0, min(p.offset, max(0, total-body)))
}

func (p *Program) move(by int) {
	p.free = false
	p.cursor += by
	p.settle()
}

// place puts the cursor on the row of an agent pane, else on a row of that pane's tab.
func (p *Program) place(pane, tab string) bool {
	for i, r := range p.rows {
		if r.Node.ID == pane {
			p.cursor = i
			p.settle()
			return true
		}
	}
	for i, r := range p.rows {
		if tab != "" && r.Node.TabID == tab {
			p.cursor = i
			p.settle()
			return true
		}
	}
	return false
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
	if p.deps.Sidebar {
		return p.sidebarView()
	}
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
	body := max(1, p.height-popupChrome)
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

// sidebarView is the rows alone. Herdr draws the section title, and the keys are not shown.
func (p Program) sidebarView() string {
	if len(p.rows) == 0 && p.err == nil {
		return p.theme.paint(" no agents", faint)
	}
	_, chrome := p.chrome()
	body := max(1, p.height-chrome)
	lines := Render(p.rows, p.width, p.cursor, p.theme)
	lines = lines[min(p.offset, len(lines)):min(len(lines), p.offset+body)]
	if p.state.Attention && len(lines) == 0 {
		lines = append(lines, p.theme.paint(" no agent needs attention (a: show all)", faint))
	}
	for len(lines) < body {
		lines = append(lines, "")
	}
	if p.err != nil {
		lines = append(lines, p.theme.paint(cut(" error: "+p.err.Error(), p.width), faint))
	}
	return strings.Join(append(lines, p.where()), "\n")
}
