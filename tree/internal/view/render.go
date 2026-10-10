package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// minLabel is the least count of cells a label or repo keeps in a narrow pane.
const minLabel = 4

// maxTag is the most cells a tag takes at the right edge.
const maxTag = 12

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
		if r.Gap {
			lines = append(lines, "")
		}
		dot := n.Shown != model.KindGroup
		glyph, right := "", ""
		if dot && n.Tag != "" {
			right = cut(n.Tag, maxTag)
		}
		if dot && r.Depth > 0 {
			switch n.Shown {
			case model.KindTab:
				glyph = " " + th.TabGlyph
			case model.KindPane:
				glyph = " " + th.PaneGlyph
			}
		}
		// The repo line carries the mark of a linked worktree. A row with no repo keeps it here.
		if dot && n.Worktree && n.Repo == "" {
			glyph += " " + th.WorktreeGlyph
		}
		if dot && n.Pending > 0 {
			glyph += " " + th.pending(n.Pending)
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
			b.WriteString(th.dot(state(n)) + " ")
		}
		if r.Depth == 0 && dot {
			b.WriteString(th.paint(label, bold))
		} else {
			b.WriteString(label)
		}
		b.WriteString(th.paint(glyph, faint))
		if right != "" {
			// The text ends at the right edge of the pane, before the dots of a folded row.
			used := 1 + lipgloss.Width(ld) + dotCells(dot) + lipgloss.Width(label) + lipgloss.Width(glyph)
			b.WriteString(strings.Repeat(" ", max(2, width-used-tail+2)) + th.paint(right, faint))
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
			room := 0
			if width > 0 {
				room = max(1, width-1-lipgloss.Width(under))
			}
			add(mark + th.paint(under, faint) + repoLine(n, room, th))
		}
	}
	return lines
}

// state is the state a row's dot shows: a working agent whose screen stands still is stalled.
func state(n *model.Node) string {
	if n.Stalled && n.Status == model.Working {
		return Stalled
	}
	return n.Status
}

// repoLine is the text under a row: the repo with its branch and its distance from the upstream,
// or the repo with the worktree glyph for a linked worktree. room is the cells it can take, or 0
// for no limit; the repo is cut before the branch, and the text after them is kept whole.
func repoLine(n *model.Node, room int, th Theme) string {
	name, branch, tail := n.Repo, n.Git.Branch, ""
	switch {
	case n.Worktree:
		branch, tail = "", " "+th.WorktreeGlyph
	case branch != "":
		if n.Git.Behind > 0 {
			tail += fmt.Sprintf(" -%d", n.Git.Behind)
		}
		if n.Git.Ahead > 0 {
			tail += fmt.Sprintf(" +%d", n.Git.Ahead)
		}
	}
	if room > 0 {
		room -= lipgloss.Width(tail)
		if branch == "" {
			name = cut(name, max(minLabel, room))
		} else {
			name = cut(name, max(minLabel, room-1-lipgloss.Width(branch)))
			branch = cut(branch, max(minLabel, room-1-lipgloss.Width(name)))
		}
	}
	if branch != "" {
		name += "@" + branch
	}
	return th.repo(name) + th.paint(tail, faint)
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
