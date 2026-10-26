package internal

import (
	"os"
	"path/filepath"
	"strings"
)

func (g *GitClient) StatusPorcelain() (string, error) {
	return g.run("status", "--porcelain")
}

// RebaseInProgress reports whether a rebase is stopped in this worktree.
// It asks git for the paths because a linked worktree keeps rebase state
// under its own git dir, not under the main repo's .git.
func (g *GitClient) RebaseInProgress() (bool, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		p, err := g.run("rev-parse", "--git-path", name)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(g.dir, p)
		}
		if _, err := os.Stat(p); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// RebaseContinue keeps the existing commit messages. GIT_EDITOR outranks
// core.editor and VISUAL/EDITOR, so an agent's environment cannot make git
// wait on an interactive editor.
func (g *GitClient) RebaseContinue() error {
	_, err := g.runEnv([]string{"GIT_EDITOR=true"}, "rebase", "--continue")
	return err
}

func (g *GitClient) ConflictedFiles() ([]string, error) {
	out, err := g.run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}
