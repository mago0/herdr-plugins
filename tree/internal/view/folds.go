package view

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

const foldsFile = "folds.json"

// LoadFolds reads the fold of each row the user has set. A missing or broken file is no folds.
func LoadFolds(dir string) map[string]bool {
	folds := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(dir, foldsFile)); err == nil {
		json.Unmarshal(b, &folds)
	}
	if folds == nil {
		folds = map[string]bool{}
	}
	return folds
}

// SaveFolds writes the folds of the efforts and their agents that are still there.
func SaveFolds(dir string, folds map[string]bool, t model.Tree) error {
	keep := map[string]bool{}
	for _, n := range t.Roots {
		keep[n.ID] = true
		for _, c := range n.Children {
			keep[c.ID] = true
		}
	}
	out := map[string]bool{}
	for id, v := range folds {
		if keep[id] {
			out[id] = v
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, foldsFile), b, 0o644)
}
