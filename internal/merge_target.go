package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeFor returns the path of the worktree that has branch checked out,
// or "" if none does.
func (g *GitClient) WorktreeFor(branch string) (string, error) {
	out, err := g.run("worktree", "list", "--porcelain")
	if err != nil {
		return "", err
	}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path, nil
		}
	}
	return "", nil
}

// OperationInProgress names an unfinished merge/rebase/cherry-pick in the
// worktree, or returns "".
func (g *GitClient) OperationInProgress() (string, error) {
	for _, marker := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD"} {
		p, err := g.run("rev-parse", "--path-format=absolute", "--git-path", marker)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(p); err == nil {
			return marker, nil
		}
	}
	return "", nil
}

// mergeIntoTarget integrates source into target where target is checked out
// (the main checkout, or a parent task's worktree); if no worktree has it, a
// temporary one is used. It never switches any checkout to another branch.
func (a *App) mergeIntoTarget(strategy MergeStrategy, source, target, message, scratchName string) error {
	path, err := a.Git.WorktreeFor(target)
	if err != nil {
		return err
	}
	if path == "" {
		path = filepath.Join(a.RepoRoot, a.Config.WorktreeDir, "_merge-"+scratchName)
		a.Git.RemoveWorktree(path)
		if err := a.Git.CreateWorktree(target, path); err != nil {
			return fmt.Errorf("creating temporary worktree for %s: %w", target, err)
		}
		defer a.Git.RemoveWorktree(path)
	}
	g := NewGitClient(path)
	op, err := g.OperationInProgress()
	if err != nil {
		return err
	}
	if op != "" {
		return fmt.Errorf("%s (%s checked out) has an unfinished git operation (%s); finish or abort it first", path, target, op)
	}
	return strategy.Merge(g, source, target, message)
}
