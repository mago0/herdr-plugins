package model

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
