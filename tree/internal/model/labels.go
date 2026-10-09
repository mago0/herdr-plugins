package model

import (
	"path"
	"regexp"
	"strings"
)

// Agent kinds that say nothing as a row label.
var generic = map[string]bool{
	"": true, "claude": true, "codex": true, "gemini": true, "opencode": true, "omp": true, "pi": true,
}

const titleRunes = 34

// AgentLabel is the row label for a pane: its name, else a specific kind, else its terminal title.
func AgentLabel(a Agent) string {
	if a.Name != "" {
		return a.Name
	}
	if !generic[a.Kind] {
		return a.Kind
	}
	if a.Title != "" {
		r := []rune(a.Title)
		if len(r) > titleRunes {
			r = r[:titleRunes]
		}
		return string(r)
	}
	if a.Kind != "" {
		return a.Kind
	}
	return "agent"
}

var ticketRE = regexp.MustCompile(`(?i)\b([a-z]{2,6})-(\d{1,5})\b`)

// TicketKey is the first issue key in a run name, upper case, or "" when there is none.
func TicketKey(runName string) string {
	m := ticketRE.FindStringSubmatch(runName)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[1]) + "-" + m[2]
}

// WorktreeRepo is the repository a dispatch worktree belongs to, from <repo>/_worktrees/<branch>.
func WorktreeRepo(worktree string) string {
	i := strings.Index(worktree, "/_worktrees/")
	if i < 0 {
		return ""
	}
	return path.Base(worktree[:i])
}
