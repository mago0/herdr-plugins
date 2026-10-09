package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

const minRightColumn = 8

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

// Render draws one line per row. selected is the index of the row under the cursor, or -1.
func Render(rows []Row, width, selected int, th Theme) []string {
	type parts struct {
		lead, label, glyph, right string
		dot                       bool
	}
	ps := make([]parts, len(rows))
	maxLeft, maxRight := 0, 0
	for i, r := range rows {
		n := r.Node
		p := parts{lead: lead(r), label: n.Label, dot: n.Shown != model.KindGroup}
		if p.dot {
			if r.Depth == 0 {
				p.right = n.Ticket
			} else {
				p.right = n.Repo
				switch n.Shown {
				case model.KindTab:
					p.glyph = " " + th.TabGlyph
				case model.KindPane:
					p.glyph = " " + th.PaneGlyph
				}
			}
		}
		ps[i] = p
		maxLeft = max(maxLeft, lipgloss.Width(p.lead)+dotCells(p.dot)+lipgloss.Width(p.label)+lipgloss.Width(p.glyph))
		maxRight = max(maxRight, lipgloss.Width(p.right))
	}

	// The right column starts two cells after the widest left part, or earlier in a narrow pane.
	col := maxLeft + 2
	if width > 0 {
		col = min(col, width-1-maxRight-1)
	}
	col = max(col, minRightColumn)

	lines := make([]string, len(rows))
	for i, r := range rows {
		p := ps[i]
		label := cut(p.label, col-2-lipgloss.Width(p.lead)-dotCells(p.dot)-lipgloss.Width(p.glyph))
		used := lipgloss.Width(p.lead) + dotCells(p.dot) + lipgloss.Width(label) + lipgloss.Width(p.glyph)

		var b strings.Builder
		switch {
		case i != selected:
			b.WriteString(" ")
		case th.Plain:
			b.WriteString(">")
		default:
			b.WriteString(bold.Render("▌"))
		}
		b.WriteString(th.paint(p.lead, faint))
		if p.dot {
			b.WriteString(th.dot(r.Node.Status) + " ")
		}
		if r.Depth == 0 && p.dot {
			b.WriteString(th.paint(label, bold))
		} else {
			b.WriteString(label)
		}
		b.WriteString(th.paint(p.glyph, faint))
		if p.right != "" || len(r.Rollup) > 0 {
			b.WriteString(strings.Repeat(" ", max(1, col-used)))
			b.WriteString(th.paint(p.right, faint))
		}
		if len(r.Rollup) > 0 {
			dots := make([]string, len(r.Rollup))
			for j, s := range r.Rollup {
				dots[j] = th.dot(s)
			}
			b.WriteString("  " + strings.Join(dots, " "))
		}
		line := strings.TrimRight(b.String(), " ")
		if width > 0 {
			line = lipgloss.NewStyle().MaxWidth(width).Render(line)
		}
		lines[i] = strings.TrimRight(line, " ")
	}
	return lines
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
