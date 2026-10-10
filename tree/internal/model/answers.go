package model

import (
	"regexp"
	"strings"
)

// Answer is one choice of the dialog a blocked agent shows: the key that picks it and its text.
type Answer struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

// answerKinds are the agent kinds whose dialogs take the number of a choice as its key.
var answerKinds = map[string]bool{"claude": true}

var (
	choiceRE = regexp.MustCompile(`^([1-9])\. (.+)$`)
	// These choices open a text field, which a single key cannot fill.
	typedRE = regexp.MustCompile(`(?i)^(type something|chat about this)`)
)

const maxAnswerRunes = 40

// Answers reads the numbered choices from what a blocked agent shows. It gives none unless the
// choices are numbered from 1 with no gap, because a wrong key in a dialog cannot be taken back.
func Answers(kind, blocker string) []Answer {
	if !answerKinds[kind] {
		return nil
	}
	var all []Answer
	for _, part := range strings.Split(blocker, " / ") {
		m := choiceRE.FindStringSubmatch(strings.TrimPrefix(strings.TrimSpace(part), "❯ "))
		if m == nil {
			continue
		}
		if int(m[1][0]-'0') != len(all)+1 {
			return nil
		}
		all = append(all, Answer{Key: m[1], Text: m[2]})
	}
	if len(all) < 2 {
		return nil
	}
	var answers []Answer
	for _, a := range all {
		if typedRE.MatchString(a.Text) {
			continue
		}
		if r := []rune(a.Text); len(r) > maxAnswerRunes {
			a.Text = string(r[:maxAnswerRunes-1]) + "…"
		}
		answers = append(answers, a)
	}
	return answers
}
