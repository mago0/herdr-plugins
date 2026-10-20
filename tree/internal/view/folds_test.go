package view

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func TestFoldsRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if got := LoadFolds(dir); len(got) != 0 {
		t.Fatalf("no file must read as no folds, got %v", got)
	}
	tree := model.Tree{Roots: []*model.Node{{ID: "w1", Children: []*model.Node{{ID: "w1:p1"}}}, {ID: "w2"}}}
	folds := map[string]bool{"w1": true, "w2": false, "w9": true, "w1:p1": true}
	if err := SaveFolds(dir, folds, tree); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"w1": true, "w2": false}
	if got := LoadFolds(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("folds = %v, want %v (only efforts that are still there keep a fold)", got, want)
	}
}
