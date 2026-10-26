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

// requireIdleWorktree: checks or a review on a half-finished rebase would
// judge files full of conflict markers.
func requireIdleWorktree(task *Task) error {
	op, err := NewGitClient(task.WorktreePath).OperationInProgress()
	if err != nil {
		return fmt.Errorf("inspecting worktree %s: %w", task.WorktreePath, err)
	}
	if op != "" {
		return fmt.Errorf("worktree %s has an unfinished git operation (%s); finish it with harness work sync --continue or --abort", task.WorktreePath, op)
	}
	return nil
}

// mergeIntoTarget builds the merge result in a detached temporary worktree
// and then only fast-forwards target, so conflicts never land in a checkout
// someone is using, and no checkout is switched to another branch.
func (a *App) mergeIntoTarget(strategy MergeStrategy, source, target, message, scratchName string) error {
	result, err := a.buildMergeResult(strategy, source, target, message, scratchName)
	if err != nil {
		return err
	}
	return a.fastForward(target, result)
}

func (a *App) buildMergeResult(strategy MergeStrategy, source, target, message, scratchName string) (string, error) {
	path := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, "_merge-"+scratchName)
	a.Git.RemoveWorktree(path)
	if _, err := a.Git.run("worktree", "add", "--detach", path, "refs/heads/"+target); err != nil {
		return "", fmt.Errorf("creating temporary worktree for %s: %w", target, err)
	}
	defer a.Git.RemoveWorktree(path)
	g := NewGitClient(path)
	if err := strategy.Merge(g, source, target, message); err != nil {
		return "", err
	}
	return g.RevParse("HEAD")
}

// fastForward moves target to result. Where target is checked out, the
// checkout is updated with an ff-only merge (git refuses rather than clobber
// local changes); otherwise the ref is compare-and-swapped.
func (a *App) fastForward(target, result string) error {
	path, err := a.Git.WorktreeFor(target)
	if err != nil {
		return err
	}
	if path != "" {
		g := NewGitClient(path)
		op, err := g.OperationInProgress()
		if err != nil {
			return err
		}
		if op != "" {
			return fmt.Errorf("%s (%s checked out) has an unfinished git operation (%s); finish or abort it first", path, target, op)
		}
		return g.MergeFF(result)
	}
	old, err := a.Git.RevParse("refs/heads/" + target)
	if err != nil {
		return err
	}
	if _, err := a.Git.run("merge-base", "--is-ancestor", old, result); err != nil {
		return fmt.Errorf("%s moved and %s is not a fast-forward of it", target, shortSHA(result))
	}
	_, err = a.Git.run("update-ref", "refs/heads/"+target, result, old)
	return err
}
