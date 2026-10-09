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
	tree := model.Tree{Roots: []*model.Node{{ID: "w1:p1", Children: []*model.Node{{ID: "w2:p1"}}}}}
	folds := map[string]bool{"w1:p1": true, "w2:p1": false, "w9:p9": true, GroupHidden: false}
	if err := SaveFolds(dir, folds, tree); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"w1:p1": true, "w2:p1": false, GroupHidden: false}
	if got := LoadFolds(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("folds = %v, want %v (the id of a node that is gone is removed)", got, want)
	}
}
