package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckoutTellsALinkedWorktreeFromAMainCheckout(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "iam")
	wt := filepath.Join(repo, "_worktrees", "review-86")
	for _, d := range []string{filepath.Join(repo, ".git"), filepath.Join(wt, "src"), filepath.Join(root, "plain")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+repo+"/.git/worktrees/review-86\n"), 0o644)

	for _, c := range []struct {
		dir, main, top string
		linked         bool
	}{
		{filepath.Join(wt, "src"), repo, wt, true},
		{repo, repo, repo, false},
		{filepath.Join(root, "plain"), "", filepath.Join(root, "plain"), false},
		{"", "", "", false},
	} {
		main, top, linked := checkout(c.dir)
		if main != c.main || top != c.top || linked != c.linked {
			t.Errorf("checkout(%q) = %q, %q, %v; want %q, %q, %v", c.dir, main, top, linked, c.main, c.top, c.linked)
		}
	}
}

func TestWhere(t *testing.T) {
	for _, c := range []struct {
		main, top string
		linked    bool
		want      string
	}{
		{"/src/iam", "/src/iam/_worktrees/review-86", true, "iam/_worktrees/review-86"},
		{"/src/iam", "/elsewhere/review-86", true, "iam/review-86"},
		{"/home/me/src/iam", "/home/me/src/iam", false, "~/src/iam"},
		{"", "/opt/notes", false, "/opt/notes"},
		{"", "", false, ""},
	} {
		if got := where(c.main, c.top, c.linked, "/home/me"); got != c.want {
			t.Errorf("where(%q, %q, %v) = %q, want %q", c.main, c.top, c.linked, got, c.want)
		}
	}
}
