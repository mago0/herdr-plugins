package model

import (
	"reflect"
	"testing"
)

func TestAnswersReadsTheNumberedChoicesOfAClaudeDialog(t *testing.T) {
	lines := []string{"Tea or coffee?", "❯ 1. Tea", "Choose tea.", "2. Coffee", "Choose coffee.", "3. Type something.",
		"4. Chat about this", "Enter to select · ↑/↓ to navigate · Esc to cancel"}
	want := []Answer{{Key: "1", Text: "Tea"}, {Key: "2", Text: "Coffee"}}
	if got := Answers("claude", lines); !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %+v, want %+v", got, want)
	}
}

func TestAnswersReadsAPermissionDialogWithTheCursorOnAnyChoice(t *testing.T) {
	lines := []string{"Bash command", "rm -rf build", "Do you want to proceed?", "1. Yes",
		"❯ 2. Yes, and don't ask again for rm commands", "3. No, and tell Claude what to do differently (esc)"}
	want := []Answer{
		{Key: "1", Text: "Yes"},
		{Key: "2", Text: "Yes, and don't ask again for rm commands"},
		// Long text is cut so that the request stays a short terminal title.
		{Key: "3", Text: "No, and tell Claude what to do differen…"},
	}
	if got := Answers("claude", lines); !reflect.DeepEqual(got, want) {
		t.Fatalf("answers = %+v, want %+v", got, want)
	}
}

func TestAnswersGivesNoneWhenTheDialogIsNotSafeToAnswerByNumber(t *testing.T) {
	for name, c := range map[string]struct {
		kind  string
		lines []string
	}{
		"another agent kind":      {"codex", []string{"Allow?", "❯ 1. Yes", "2. No"}},
		"choices with no numbers": {"claude", []string{"Is this a project you trust?", "❯ No, exit", "Yes, I trust this folder"}},
		"one choice":              {"claude", []string{"Continue?", "❯ 1. Yes"}},
		"numbers that do not run": {"claude", []string{"Steps", "❯ 2. Build", "3. Test"}},
		"a list with a gap":       {"claude", []string{"Pick", "❯ 1. A", "3. C"}},
		"no lines":                {"claude", nil},
		// Numbered text with no cursor mark is prose, such as a plan above a prompt.
		"numbered text and no cursor": {"claude", []string{"Plan", "1. Yes, go", "2. No", "❯ No, exit", "Yes, I trust this folder"}},
		// The top of the list scrolled out of the lines Herdr keeps, and what is left starts with
		// a description that holds numbers of its own.
		"a list cut at its start": {"claude", []string{"1. Safe choice / 2. Also safe", "❯ 3. C", "4. D"}},
		"two cursor marks":        {"claude", []string{"❯ 1. A", "❯ 2. B"}},
		// A key is one digit, and the agent may wait for a second digit when it has ten choices.
		"ten choices": {"claude", []string{"❯ 1. A", "2. B", "3. C", "4. D", "5. E", "6. F", "7. G", "8. H", "9. I", "10. J"}},
		// A prompt the person sent is drawn with the cursor mark. A dialog with no numbers below
		// it is not a list of its choices.
		"a numbered prompt above a dialog": {"claude", []string{"❯ 1. do X", "2. do Y", "Working on it.", "Edit file a.go?", "Yes", "No, exit", "Esc to cancel"}},
		// Numbered prose and one numbered line far below it are not one list.
		"numbers far from each other": {"claude", []string{"❯ 1. first step", "2. second step", "a", "b", "c", "3. No, tell Claude"}},
		// One screen line that holds several numbers is one choice with odd text, not several.
		"a second list after the choices": {"claude", []string{"❯ 1. A", "2. B", "Notes", "1. first", "2. second"}},
	} {
		if got := Answers(c.kind, c.lines); len(got) != 0 {
			t.Errorf("%s: answers = %+v, want none", name, got)
		}
	}
}

func TestBuildPutsTheAnswersOfABlockedAgentOnItsRow(t *testing.T) {
	a := ag("w1:p1", "w1:t1", "")
	a.Kind, a.Status, a.Dialog, a.BlockerLines = "claude", Blocked, "d1a109", []string{"Allow?", "❯ 1. Yes", "2. No"}
	idle := ag("w2:p1", "w2:t1", "")
	idle.Kind, idle.Dialog, idle.BlockerLines = "claude", "d1a109", []string{"Allow?", "❯ 1. Yes", "2. No"}
	unnamed := ag("w3:p1", "w3:t1", "")
	unnamed.Kind, unnamed.Status, unnamed.BlockerLines = "claude", Blocked, []string{"Allow?", "❯ 1. Yes", "2. No"}
	s := Snapshot{
		Workspaces: []Workspace{ws("w1", "a", "", 1), ws("w2", "b", "", 2), ws("w3", "c", "", 3)},
		Tabs:       []Tab{tab("w1:t1", "1", 1), tab("w2:t1", "1", 1), tab("w3:t1", "1", 1)},
		Agents:     []Agent{a, idle, unnamed},
	}
	got := Build(s)
	if len(got.Roots[0].Answers) != 2 || got.Roots[0].Dialog != "d1a109" {
		t.Fatalf("a blocked row has its answers and its dialog, got %+v", got.Roots[0])
	}
	if len(got.Roots[1].Answers) != 0 {
		t.Fatalf("a row that is not blocked has no answers, got %+v", got.Roots[1].Answers)
	}
	if len(got.Roots[2].Answers) != 0 {
		t.Fatalf("with no dialog name Herdr cannot check an answer, so the row has none, got %+v", got.Roots[2].Answers)
	}
}
