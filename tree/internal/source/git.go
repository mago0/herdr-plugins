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

// gitState is the branch of a main checkout and its distance from the upstream branch. A linked
// worktree and a directory outside Git have none.
func gitState(main string, linked bool) model.Git {
	if main == "" || linked {
		return model.Git{}
	}
	g := model.Git{Repo: filepath.Base(main), Branch: branch(main)}
	if g.Branch == "" {
		return g
	}
	countMu.Lock()
	defer countMu.Unlock()
	c, ok := counts[main]
	if !ok || c.branch != g.Branch || now().Sub(c.at) >= countTTL {
		c = counted{branch: g.Branch, at: now()}
		c.ahead, c.behind = count(main)
		counts[main] = c
	}
	g.Ahead, g.Behind = c.ahead, c.behind
	return g
}

// branch is the branch a main checkout has out, or the short commit id of a detached HEAD.
func branch(main string) string {
	text, _ := os.ReadFile(filepath.Join(main, ".git", "HEAD"))
	head := strings.TrimSpace(string(text))
	if b, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return b
	}
	if len(head) > 7 {
		return head[:7]
	}
	return ""
}

// revCount asks Git how many commits HEAD is ahead of and behind its upstream branch, as of the
// last fetch. Both are 0 when the branch has no upstream.
func revCount(main string) (ahead, behind int) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", main, "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	fmt.Sscan(string(out), &behind, &ahead)
	return ahead, behind
}
