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
		if got := branch(filepath.Join(repo, ".git")); got != want {
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
	if got := gitState(repo, repo, false, upstream); got != want {
		t.Fatalf("gitState = %+v, want %+v", got, want)
	}
	// A linked worktree has its own branch, counted against the default branch of origin.
	run(repo, "remote", "set-head", "origin", "main")
	wt := filepath.Join(repo, "_worktrees", "feat")
	run(repo, "worktree", "add", "-q", "-b", "feat", wt)
	run(wt, "commit", "-q", "--allow-empty", "-m", "five")
	want = model.Git{Repo: "iam", Branch: "feat", Ahead: 3, Behind: 1}
	if got := gitState(repo, wt, true, base); got != want {
		t.Fatalf("gitState of a linked worktree = %+v, want %+v", got, want)
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
	count, now = func(string, string) (int, int) { calls++; return calls, 0 }, func() time.Time { return at }
	t.Cleanup(func() { count, now = oldCount, oldNow })

	head("main")
	gitState(repo, repo, false, upstream)
	if g := gitState(repo, repo, false, upstream); calls != 1 || g.Ahead != 1 {
		t.Fatalf("calls = %d, ahead = %d; want 1, 1", calls, g.Ahead)
	}
	head("other")
	if gitState(repo, repo, false, upstream); calls != 2 {
		t.Fatalf("calls after a branch change = %d, want 2", calls)
	}
	at = at.Add(countTTL)
	if gitState(repo, repo, false, upstream); calls != 3 {
		t.Fatalf("calls after the cache time = %d, want 3", calls)
	}
}

func TestGitDirFollowsTheGitFileOfALinkedWorktree(t *testing.T) {
	top := t.TempDir()
	if got := gitDir(top); got != filepath.Join(top, ".git") {
		t.Errorf("a main checkout keeps its Git folder in .git, got %q", got)
	}
	os.WriteFile(filepath.Join(top, ".git"), []byte("gitdir: /src/iam/.git/worktrees/feat\n"), 0o644)
	if got := gitDir(top); got != "/src/iam/.git/worktrees/feat" {
		t.Errorf("gitDir = %q", got)
	}
	os.WriteFile(filepath.Join(top, ".git"), []byte("gitdir: ../iam/.git/worktrees/feat\n"), 0o644)
	if got, want := gitDir(top), filepath.Join(filepath.Dir(top), "iam", ".git", "worktrees", "feat"); got != want {
		t.Errorf("a relative gitdir = %q, want %q", got, want)
	}
}
