package view

import (
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// Stalled is the dot state of a working agent whose screen does not change.
const Stalled = "stalled"

// Theme is how states and row kinds are drawn. Plain draws with no color, for tests and scripts.
type Theme struct {
	Plain  bool
	Colors map[string]string
	// ShellGlyph stands before the name of a pane that holds no agent.
	ShellGlyph string
	// WorktreeGlyph marks a row whose agent works in a linked Git worktree.
	WorktreeGlyph string
	// RepoColor is the color of the repo line under a row.
	RepoColor string
	// PendingGlyph stands before the count of prompts Herdr holds for a row.
	PendingGlyph string
}

// DefaultTheme uses Nerd Font glyphs for the worktree and held prompts.
func DefaultTheme() Theme {
	return Theme{
		Colors: map[string]string{
			model.Blocked: "#f38ba8",
			model.Working: "#f9e2af",
			model.Done:    "#a6e3a1",
			model.Idle:    "#89b4fa",
			model.Unknown: "#6c7086",
			Stalled:       "#fab387",
		},
		ShellGlyph:    "$",
		WorktreeGlyph: "\ue0a0",
		PendingGlyph:  "\uf0e0",
	}
}

// LoadTheme applies the [glyphs] and [colors] tables of <configDir>/config.toml to the default theme.
func LoadTheme(configDir string) Theme {
	th := DefaultTheme()
	var file struct {
		Glyphs struct {
			Shell    string `toml:"shell"`
			Worktree string `toml:"worktree"`
			Pending  string `toml:"pending"`
		} `toml:"glyphs"`
		Colors map[string]string `toml:"colors"`
	}
	if _, err := toml.DecodeFile(filepath.Join(configDir, "config.toml"), &file); err != nil {
		return th
	}
	if file.Glyphs.Shell != "" {
		th.ShellGlyph = file.Glyphs.Shell
	}
	if c := file.Colors["repo"]; c != "" {
		th.RepoColor = c
	}
	if file.Glyphs.Worktree != "" {
		th.WorktreeGlyph = file.Glyphs.Worktree
	}
	if file.Glyphs.Pending != "" {
		th.PendingGlyph = file.Glyphs.Pending
	}
	for state, color := range file.Colors {
		if _, known := th.Colors[state]; known && color != "" {
			th.Colors[state] = color
		}
	}
	return th
}

var plainDots = map[string]string{
	model.Blocked: "!", model.Working: "*", model.Done: "+", model.Idle: "o", Stalled: "~",
}

// pending is the mark for prompts Herdr holds for a row, with their count.
func (th Theme) pending(n int) string {
	if th.Plain {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprintf("%s %d", th.PendingGlyph, n)
}

func (th Theme) dot(status string) string {
	if _, known := th.Colors[status]; !known {
		status = model.Unknown
	}
	if th.Plain {
		if g, ok := plainDots[status]; ok {
			return g
		}
		return "·"
	}
	g := "●"
	if status == model.Unknown {
		g = "·"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(th.Colors[status])).Render(g)
}

func (th Theme) paint(s string, st lipgloss.Style) string {
	if th.Plain || s == "" {
		return s
	}
	return st.Render(s)
}

// repo draws a repo name apart from the row names: italic, in its own color.
func (th Theme) repo(s string) string {
	return th.paint(s, lipgloss.NewStyle().Italic(true).Faint(true).Foreground(lipgloss.Color(th.RepoColor)))
}
