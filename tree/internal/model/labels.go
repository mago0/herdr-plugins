package model

import "regexp"

// autoLabel is how herdr-automations names what it opens: "auto: <name>" for a workspace, with
// " YYYY-MM-DD HH:MM" after it for a tab in a workspace that is already there.
var autoLabel = regexp.MustCompile(`^auto: (.+?)(?: \d{4}-\d{2}-\d{2} \d{2}:\d{2})?$`)

// AutoName is the automation's name in a label herdr-automations gave, and true; else the label
// and false.
func AutoName(label string) (string, bool) {
	if m := autoLabel.FindStringSubmatch(label); m != nil {
		return m[1], true
	}
	return label, false
}

const titleRunes = 34

// AgentLabel is the row label for an agent: the title its session set, else its name, else its kind.
func AgentLabel(a Agent) string {
	if a.Title != "" {
		r := []rune(a.Title)
		if len(r) > titleRunes {
			r = r[:titleRunes]
		}
		return string(r)
	}
	if a.Name != "" {
		return a.Name
	}
	if a.Kind != "" {
		return a.Kind
	}
	return "agent"
}
