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
