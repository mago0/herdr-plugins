package model

import "testing"

func TestAgentLabel(t *testing.T) {
	cases := []struct {
		a    Agent
		want string
	}{
		{Agent{Name: "add-probe", Kind: "claude", Title: "x"}, "add-probe"},
		{Agent{Kind: "pr-watch-86", Title: "/some/script"}, "pr-watch-86"},
		{Agent{Kind: "claude", Title: "Plan the api redesign"}, "Plan the api redesign"},
		{Agent{Kind: "codex", Title: "0123456789012345678901234567890123456789"}, "0123456789012345678901234567890123"},
		{Agent{Kind: "claude"}, "claude"},
		{Agent{}, "agent"},
	}
	for _, c := range cases {
		if got := AgentLabel(c.a); got != c.want {
			t.Errorf("AgentLabel(%+v) = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestTicketKey(t *testing.T) {
	cases := map[string]string{
		"abc-923-soak":        "ABC-923",
		"ops-12":              "OPS-12",
		"review-20261009":     "",
		"abc-review-20261009": "",
		"cleanup":             "",
		"x-1":                 "",
	}
	for in, want := range cases {
		if got := TicketKey(in); got != want {
			t.Errorf("TicketKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWorktreeRepo(t *testing.T) {
	cases := map[string]string{
		"/src/api/_worktrees/abc-1-fix": "api",
		"/src/api":                      "",
		"":                              "",
	}
	for in, want := range cases {
		if got := WorktreeRepo(in); got != want {
			t.Errorf("WorktreeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
