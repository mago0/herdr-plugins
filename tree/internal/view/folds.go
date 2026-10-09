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

// SaveFolds writes the folds of the rows that are still in the tree.
func SaveFolds(dir string, folds map[string]bool, t model.Tree) error {
	keep := map[string]bool{GroupNoAgent: true, GroupHidden: true}
	var mark func(nodes []*model.Node)
	mark = func(nodes []*model.Node) {
		for _, n := range nodes {
			keep[n.ID] = true
			mark(n.Children)
		}
	}
	mark(t.Roots)
	mark(t.NoAgent)
	mark(t.Hidden)
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
