package model

import "testing"

func TestAgentLabel(t *testing.T) {
	cases := []struct {
		a    Agent
		want string
	}{
		{Agent{Name: "add-probe", Kind: "claude", Title: "Review infra"}, "Review infra"},
		{Agent{Name: "add-probe", Kind: "claude"}, "add-probe"},
		{Agent{Kind: "pr-watch-86"}, "pr-watch-86"},
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

func TestAutoName(t *testing.T) {
	for label, want := range map[string]struct {
		name string
		auto bool
	}{
		"auto: job-scheduler-flostag-watch 2026-10-20 00:07": {"job-scheduler-flostag-watch", true},
		"auto: Weekly sprint planning":                       {"Weekly sprint planning", true},
		"Dispatch":                                           {"Dispatch", false},
		"auto:":                                              {"auto:", false},
	} {
		if name, auto := AutoName(label); name != want.name || auto != want.auto {
			t.Errorf("AutoName(%q) = %q, %v; want %q, %v", label, name, auto, want.name, want.auto)
		}
	}
}
