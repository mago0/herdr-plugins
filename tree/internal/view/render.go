package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// minLabel is the least count of cells a label or repo keeps in a narrow pane.
const minLabel = 4

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
)

// cut shortens s to n cells, ending in an ellipsis when it was cut.
func cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// cutLeft shortens s to n cells and keeps its end, with an ellipsis in front when it was cut.
func cutLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[1:]
	}
	return "…" + string(r)
}

// Render draws the rows: one line for a row, and a second line for its repo where Row.Repo is set.
// selected is the index of the row under the cursor, or -1.
func Render(rows []Row, width, selected int, th Theme) []string {
	var lines []string
	add := func(line string) {
		line = strings.TrimRight(line, " ")
		if width > 0 {
			line = strings.TrimRight(lipgloss.NewStyle().MaxWidth(width).Render(line), " ")
		}
		lines = append(lines, line)
	}
	for i, r := range rows {
		n := r.Node
		dot := n.Shown != model.KindGroup
		glyph, right := "", ""
		if dot && r.Depth == 0 {
			right = n.Ticket
		}
		if dot && r.Depth > 0 {
			switch n.Shown {
			case model.KindTab:
				glyph = " " + th.TabGlyph
			case model.KindPane:
				glyph = " " + th.PaneGlyph
			}
		}
		if dot && n.Worktree {
			glyph += " " + th.WorktreeGlyph
		}
		tail := 0
		if right != "" {
			tail += 2 + lipgloss.Width(right)
		}
		if len(r.Rollup) > 0 {
			tail += 1 + 2*len(r.Rollup)
		}
		ld := lead(r)
		label := n.Label
		if width > 0 {
			label = cut(label, max(minLabel, width-1-lipgloss.Width(ld)-dotCells(dot)-lipgloss.Width(glyph)-tail))
		}

		mark := " "
		switch {
		case i != selected:
		case th.Plain:
			mark = ">"
		default:
			mark = bold.Render("▌")
		}
		var b strings.Builder
		b.WriteString(mark + th.paint(ld, faint))
		if dot {
			b.WriteString(th.dot(n.Status) + " ")
		}
		if r.Depth == 0 && dot {
			b.WriteString(th.paint(label, bold))
		} else {
			b.WriteString(label)
		}
		b.WriteString(th.paint(glyph, faint))
		if right != "" {
			b.WriteString("  " + th.paint(right, faint))
		}
		if len(r.Rollup) > 0 {
			dots := make([]string, len(r.Rollup))
			for j, s := range r.Rollup {
				dots[j] = th.dot(s)
			}
			b.WriteString("  " + strings.Join(dots, " "))
		}
		add(b.String())
		if r.Repo != "" {
			under := below(r)
			repo := r.Repo
			if width > 0 {
				repo = cut(repo, max(minLabel, width-1-lipgloss.Width(under)))
			}
			add(mark + th.paint(under, faint) + th.repo(repo))
		}
	}
	return lines
}

// below is the part of a repo line before the repo: the guide lines that run past the row, and
// the line down to its children when they are in view. The repo starts under the label.
func below(r Row) string {
	var b strings.Builder
	b.WriteString("  ")
	if r.Depth > 0 {
		for _, runs := range append(append([]bool(nil), r.Trunk...), !r.Last) {
			if runs {
				b.WriteString("│  ")
			} else {
				b.WriteString("   ")
			}
		}
	}
	if r.HasChildren && !r.Folded {
		return b.String() + "│ "
	}
	return b.String() + "  "
}

// lead is the fold mark of a root, or the guide lines and branch of a row below one.
func lead(r Row) string {
	mark := ""
	if r.HasChildren {
		mark = "▾"
		if r.Folded {
			mark = "▸"
		}
	}
	if r.Depth == 0 {
		if mark == "" {
			mark = " "
		}
		return mark + " "
	}
	var b strings.Builder
	b.WriteString("  ")
	for _, runs := range r.Trunk {
		if runs {
			b.WriteString("│  ")
		} else {
			b.WriteString("   ")
		}
	}
	if r.Last {
		b.WriteString("└")
	} else {
		b.WriteString("├")
	}
	// Below a root only a folded row is marked: an open parent has its children in view.
	if r.Folded {
		return b.String() + "▸ "
	}
	return b.String() + "─ "
}

func dotCells(dot bool) int {
	if dot {
		return 2
	}
	return 0
}
