package source

import (
	"os"
	"path/filepath"
	"strings"
)

// where is the text that says where a checkout is: "<repo>/<path in the repo>" for a linked
// worktree, and the path with the home directory as ~ for any other directory.
func where(main, top string, linked bool, home string) string {
	if top == "" {
		return ""
	}
	if !linked || main == "" {
		if home != "" && (top == home || strings.HasPrefix(top, home+"/")) {
			return "~" + top[len(home):]
		}
		return top
	}
	if rel, err := filepath.Rel(main, top); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join(filepath.Base(main), rel)
	}
	return filepath.Join(filepath.Base(main), filepath.Base(top))
}

// checkout finds the Git checkout that holds dir. A linked worktree has a .git file that names
// its entry in the main checkout; main is that main checkout.
func checkout(dir string) (main, top string, linked bool) {
	if dir == "" {
		return "", "", false
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		info, err := os.Lstat(filepath.Join(d, ".git"))
		if err == nil {
			if info.IsDir() {
				return d, d, false
			}
			text, _ := os.ReadFile(filepath.Join(d, ".git"))
			gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(text)), "gitdir:"))
			if i := strings.Index(gitdir, "/.git/worktrees/"); i > 0 {
				return gitdir[:i], d, true
			}
			return d, d, false
		}
		if d == filepath.Dir(d) {
			return "", dir, false
		}
	}
}
