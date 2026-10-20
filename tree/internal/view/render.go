package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// minLabel is the least count of cells a label, repo or branch keeps in a narrow pane.
const minLabel = 4

// maxTag is the most cells a label takes at the right edge.
const maxTag = 12

var (
	bold      = lipgloss.NewStyle().Bold(true)
	faint     = lipgloss.NewStyle().Faint(true)
	plainText = lipgloss.NewStyle()
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

// Render draws the rows: one line for each row, and a second line under an effort or a worker
// that has a repo. selected is the index of the row under the cursor, or -1.
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
		mark := " "
		switch {
		case i != selected:
		case th.Plain:
			mark = ">"
		default:
			mark = bold.Render("▌")
		}
		head, after, right, style := mark+th.paint(lead(r), faint), "", "", plainText
		switch {
		case r.Depth == 0:
			style, right = bold, effortTag(n)
			if r.Folded {
				after = "  " + rollup(r.Rollup, th)
			}
		case n.Shown == model.KindShell:
			head += th.shell() + " "
		default:
			head += th.dot(state(n)) + " "
			if n.Pending > 0 {
				after = " " + th.paint(th.pending(n.Pending), faint)
			}
			if n.Worker {
				right = workerTag(n)
			}
		}
		if r.Folded && r.Depth > 0 {
			after += "  " + rollup(r.Rollup, th)
		}
		used := lipgloss.Width(head) + lipgloss.Width(after)
		tail := 0
		if right != "" {
			tail = 2 + lipgloss.Width(right)
		}
		label := n.Label
		if width > 0 {
			label = cut(label, max(minLabel, width-used-tail))
		}
		line := head + th.paint(label, style) + after
		if right != "" {
			pad := 2
			if width > 0 {
				pad = max(2, width-used-lipgloss.Width(label)-lipgloss.Width(right))
			}
			line += strings.Repeat(" ", pad) + th.paint(right, tagStyle(n))
		}
		add(line)
		if r.Repo != "" {
			under, note := below(r), ""
			if n.Worker && n.Reported {
				note = "  reported"
			}
			room := 0
			if width > 0 {
				room = max(1, width-1-lipgloss.Width(under)-lipgloss.Width(note))
			}
			add(mark + th.paint(under, faint) + repoLine(n, room, th) + th.paint(note, faint))
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

// rollup is the dots of a folded effort's agents and workers, or the shell glyph for an effort
// that holds only shells.
func rollup(states []string, th Theme) string {
	out := make([]string, len(states))
	for i, s := range states {
		if s == RollupShell {
			out[i] = th.shell()
		} else {
			out[i] = th.dot(s)
		}
	}
	return strings.Join(out, " ")
}

// effortTag is the right edge of an effort: "proj ↗" for a Linear project, else its label, with
// ↗ when it has a link to open.
func effortTag(n *model.Node) string {
	switch {
	case model.IsProject(n.Link):
		return "proj ↗"
	case n.Tag != "" && n.Link != "":
		return cut(n.Tag, maxTag) + " ↗"
	case n.Tag != "":
		return cut(n.Tag, maxTag)
	case n.Link != "":
		return "↗"
	}
	return ""
}

// workerTag is the right edge of a worker: its label, else the number of the pull request it links.
func workerTag(n *model.Node) string {
	if n.Tag != "" {
		return cut(n.Tag, maxTag)
	}
	return model.PRNumber(n.Link)
}

// repoLine is the second line of a row: the worktree glyph for a linked worktree, the repo with its
// branch, and the distance of the branch from its base. room is the cells it can take, or 0 for no
// limit; the branch is cut, then left out, then the repo is cut, and the counts are kept whole.
func repoLine(n *model.Node, room int, th Theme) string {
	glyph := ""
	if n.Worktree {
		glyph = th.WorktreeGlyph + " "
	}
	name, branch, tail := n.Repo, n.Git.Branch, ""
	if branch != "" {
		if n.Git.Behind > 0 {
			tail += fmt.Sprintf(" -%d", n.Git.Behind)
		}
		if n.Git.Ahead > 0 {
			tail += fmt.Sprintf(" +%d", n.Git.Ahead)
		}
	}
	if room > 0 {
		room -= lipgloss.Width(glyph) + lipgloss.Width(tail)
		if branch != "" && lipgloss.Width(name)+1+lipgloss.Width(branch) > room {
			branch = cut(branch, max(minLabel, room-1-lipgloss.Width(name)))
			// With no room for the repo and a short branch, the branch goes before the counts do.
			if lipgloss.Width(name)+1+lipgloss.Width(branch) > room {
				branch = ""
			}
		}
		if branch == "" {
			name = cut(name, max(1, room))
		}
	}
	if branch != "" {
		name += "@" + branch
	}
	return th.paint(glyph, faint) + th.repo(name) + th.paint(tail, faint)
}

// guides is the lines of the workers above a row that run on past it.
func guides(trunk []bool) string {
	var b strings.Builder
	for _, runs := range trunk {
		if runs {
			b.WriteString("│ ")
		} else {
			b.WriteString("  ")
		}
	}
	return b.String()
}

// lead is what comes before the dot or glyph of a row: the fold mark of an effort, the indent of
// its panes with the fold mark of an agent that has workers, and the branch line of a worker,
// which starts under the dot of the agent above it.
func lead(r Row) string {
	switch r.Depth {
	case 0:
		mark := " "
		if r.HasChildren {
			mark = "▾"
			if r.Folded {
				mark = "▸"
			}
		}
		return mark + " "
	case 1:
		switch {
		case r.Folded:
			return "  ▸ "
		case r.HasChildren:
			return "  ▾ "
		}
		return "    "
	}
	b := "    " + guides(r.Trunk)
	switch {
	case r.Node.Shown != model.KindPane:
		return b + "  "
	case r.Last:
		return b + "└ "
	}
	return b + "├ "
}

// below is what comes before the second line of a row: under an effort, the indent of its name,
// so the repo reads as part of the heading and not as a pane; under an agent, the indent of its
// title; under a worker, its branch line running on to the next worker.
func below(r Row) string {
	switch r.Depth {
	case 0:
		return "  "
	case 1:
		return "      "
	}
	b := "    " + guides(r.Trunk)
	if r.Last {
		return b + "    "
	}
	return b + "│   "
}

// tagStyle is the look of a row's right edge: underlined when the row has a link to open.
func tagStyle(n *model.Node) lipgloss.Style {
	if n.Link != "" {
		return faint.Underline(true)
	}
	return faint
}
