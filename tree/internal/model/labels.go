package model

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
