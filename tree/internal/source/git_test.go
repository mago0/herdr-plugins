package source

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func TestBranchReadsHEAD(t *testing.T) {
	for head, want := range map[string]string{
		"ref: refs/heads/feat/x\n":                   "feat/x",
		"0123456789abcdef0123456789abcdef01234567\n": "0123456",
		"": "",
	} {
		repo := t.TempDir()
		os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
		os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte(head), 0o644)
		if got := branch(repo); got != want {
			t.Errorf("branch with HEAD %q = %q, want %q", head, got, want)
		}
	}
}

func TestGitStateCountsAgainstTheUpstream(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(root, "init", "-q", "--bare", "origin.git")
	run(root, "clone", "-q", "origin.git", "iam")
	repo := filepath.Join(root, "iam")
	run(repo, "checkout", "-q", "-b", "main")
	run(repo, "commit", "-q", "--allow-empty", "-m", "one")
	run(repo, "commit", "-q", "--allow-empty", "-m", "two")
	run(repo, "push", "-q", "-u", "origin", "main")
	run(repo, "reset", "-q", "--hard", "HEAD~1")
	run(repo, "commit", "-q", "--allow-empty", "-m", "three")
	run(repo, "commit", "-q", "--allow-empty", "-m", "four")

	want := model.Git{Repo: "iam", Branch: "main", Ahead: 2, Behind: 1}
	if got := gitState(repo, false); got != want {
		t.Fatalf("gitState = %+v, want %+v", got, want)
	}
	if got := gitState(repo, true); got != (model.Git{}) {
		t.Fatalf("gitState of a linked worktree = %+v, want none", got)
	}
}

func TestGitStateCountsAgainOnlyAfterTheBranchOrTheClockMoves(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "iam")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	head := func(b string) {
		os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/"+b+"\n"), 0o644)
	}
	calls, at := 0, time.Unix(0, 0)
	oldCount, oldNow := count, now
	count, now = func(string) (int, int) { calls++; return calls, 0 }, func() time.Time { return at }
	t.Cleanup(func() { count, now = oldCount, oldNow })

	head("main")
	gitState(repo, false)
	if g := gitState(repo, false); calls != 1 || g.Ahead != 1 {
		t.Fatalf("calls = %d, ahead = %d; want 1, 1", calls, g.Ahead)
	}
	head("other")
	if gitState(repo, false); calls != 2 {
		t.Fatalf("calls after a branch change = %d, want 2", calls)
	}
	at = at.Add(countTTL)
	if gitState(repo, false); calls != 3 {
		t.Fatalf("calls after the cache time = %d, want 3", calls)
	}
}
