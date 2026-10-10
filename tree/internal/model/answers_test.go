package model

import (
	"reflect"
	"testing"
)

func TestAnswersReadsTheNumberedChoicesOfAClaudeDialog(t *testing.T) {
	blocker := "Tea or coffee? / ❯ 1. Tea / Choose tea. / 2. Coffee / Choose coffee. / 3. Type something. / 4. Chat about this / Enter to select · ↑/↓ to navigate · Esc to cancel"
	want := []Answer{{Key: "1", Text: "Tea"}, {Key: "2", Text: "Coffee"}}
	if got := Answers("claude", blocker); !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %+v, want %+v", got, want)
	}
}

func TestAnswersReadsAPermissionDialog(t *testing.T) {
	blocker := "Bash command / rm -rf build / Do you want to proceed? / ❯ 1. Yes / 2. Yes, and don't ask again for rm commands / 3. No, and tell Claude what to do differently (esc)"
	want := []Answer{
		{Key: "1", Text: "Yes"},
		{Key: "2", Text: "Yes, and don't ask again for rm commands"},
		// Long text is cut so that the request stays a short terminal title.
		{Key: "3", Text: "No, and tell Claude what to do differen…"},
	}
	if got := Answers("claude", blocker); !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %+v, want %+v", got, want)
	}
}

func TestAnswersGivesNoneWhenTheDialogIsNotSafeToAnswerByNumber(t *testing.T) {
	for name, c := range map[string][2]string{
		"another agent kind":       {"codex", "Allow? / 1. Yes / 2. No"},
		"choices with no numbers":  {"claude", "Is this a project you trust? / ❯ No, exit / Yes, I trust this folder / Enter to confirm"},
		"one choice":               {"claude", "Continue? / 1. Yes"},
		"numbers that do not run":  {"claude", "Steps / 2. Build / 3. Test"},
		"a list with a gap":        {"claude", "Pick / 1. A / 3. C"},
		"no blocker":               {"claude", ""},
		"numbered text, cut start": {"claude", "...2. Coffee / 3. Type something."},
	} {
		if got := Answers(c[0], c[1]); len(got) != 0 {
			t.Errorf("%s: answers = %+v, want none", name, got)
		}
	}
}

func TestBuildPutsTheAnswersOfABlockedAgentOnItsRow(t *testing.T) {
	a := ag("w1:p1", "w1:t1", "")
	a.Kind, a.Status, a.Blocker = "claude", Blocked, "Allow? / ❯ 1. Yes / 2. No"
	idle := ag("w2:p1", "w2:t1", "")
	idle.Kind, idle.Blocker = "claude", "Allow? / ❯ 1. Yes / 2. No"
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "a", "", 1), ws("w2", "b", "", 2)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1)},
		Agents:     []Agent{a, idle},
	}
	got := Build(s)
	if len(got.Roots[0].Answers) != 2 {
		t.Fatalf("a blocked row has its answers, got %+v", got.Roots[0].Answers)
	}
	if len(got.Roots[1].Answers) != 0 {
		t.Fatalf("a row that is not blocked has no answers, got %+v", got.Roots[1].Answers)
	}
}
