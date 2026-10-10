package model

import (
	"regexp"
	"strings"
)

// Answer is one choice of the dialog a blocked agent shows: the key that picks it and its text.
type Answer struct {
	Key  string
	Text string
}

// answerKinds are the agent kinds whose dialogs take the number of a choice as its key.
var answerKinds = map[string]bool{"claude": true}

var (
	choiceRE = regexp.MustCompile(`^([1-9])\. (.+)$`)
	// These choices open a text field, which a single key cannot fill.
	typedRE = regexp.MustCompile(`(?i)^(type something|chat about this)`)
)

// cursor marks the choice a dialog has selected.
const cursor = "❯ "

// MaxAnswerRunes is the most text an answer keeps.
const MaxAnswerRunes = 40

// Answers reads the numbered choices from the screen lines of a blocked agent. A wrong key in a
// dialog cannot be taken back, so it gives none unless the lines are sure to be one list of
// choices: every numbered line runs from 1 with no gap and no repeat, and the dialog's cursor is
// on exactly one of them.
func Answers(kind string, lines []string) []Answer {
	if !answerKinds[kind] {
		return nil
	}
	var all []Answer
	marked := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		onCursor := strings.HasPrefix(line, cursor)
		m := choiceRE.FindStringSubmatch(strings.TrimPrefix(line, cursor))
		if m == nil {
			continue
		}
		if int(m[1][0]-'0') != len(all)+1 {
			return nil
		}
		if onCursor {
			marked++
		}
		all = append(all, Answer{Key: m[1], Text: m[2]})
	}
	if len(all) < 2 || marked != 1 {
		return nil
	}
	var answers []Answer
	for _, a := range all {
		if typedRE.MatchString(a.Text) {
			continue
		}
		if r := []rune(a.Text); len(r) > MaxAnswerRunes {
			a.Text = string(r[:MaxAnswerRunes-1]) + "…"
		}
		answers = append(answers, a)
	}
	return answers
}
