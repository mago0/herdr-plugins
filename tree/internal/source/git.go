package source

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

// countTTL is how long the counts of a checkout stand before Git is asked again.
const countTTL = 10 * time.Second

// What the commits of a checkout are counted against: its upstream branch for a workspace, and
// the default branch of origin for an agent, which is what a dispatched branch starts from.
const (
	upstream = "@{upstream}"
	base     = "origin/HEAD"
)

var (
	now   = time.Now
	count = revCount

	countMu sync.Mutex
	counts  = map[string]counted{}
)

type counted struct {
	branch        string
	ahead, behind int
	at            time.Time
}

// gitState is the branch a checkout has out and how far HEAD is ahead of and behind against.
// main is the main checkout of the repository, and top is the root of a linked worktree.
func gitState(main, top string, linked bool, against string) model.Git {
	if main == "" {
		return model.Git{}
	}
	dir := main
	if linked && top != "" {
		dir = top
	}
	g := model.Git{Repo: filepath.Base(main), Branch: branch(gitDir(dir))}
	if g.Branch == "" {
		return g
	}
	key := dir + "\x00" + against
	countMu.Lock()
	defer countMu.Unlock()
	c, ok := counts[key]
	if !ok || c.branch != g.Branch || now().Sub(c.at) >= countTTL {
		c = counted{branch: g.Branch, at: now()}
		c.ahead, c.behind = count(dir, against)
		counts[key] = c
	}
	g.Ahead, g.Behind = c.ahead, c.behind
	return g
}

// gitDir is the folder that holds the HEAD of a checkout: .git of a main checkout, or the folder
// that the .git file of a linked worktree names.
func gitDir(top string) string {
	p := filepath.Join(top, ".git")
	info, err := os.Lstat(p)
	if err != nil || info.IsDir() {
		return p
	}
	text, _ := os.ReadFile(p)
	d := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(text)), "gitdir:"))
	if !filepath.IsAbs(d) {
		d = filepath.Join(top, d)
	}
	return d
}

// branch is the branch named by the HEAD in a Git folder, or the short commit id of a detached HEAD.
func branch(gitdir string) string {
	text, _ := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	head := strings.TrimSpace(string(text))
	if b, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return b
	}
	if len(head) > 7 {
		return head[:7]
	}
	return ""
}

// revCount asks Git how many commits HEAD is ahead of and behind against, as of the last fetch.
// Both are 0 when against does not resolve.
func revCount(dir, against string) (ahead, behind int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-list", "--left-right", "--count", against+"...HEAD")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	fmt.Sscan(string(out), &behind, &ahead)
	return ahead, behind
}
