package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func (a *App) epicSyncWorktree(epicID string) string {
	return filepath.Join(a.RepoRoot, a.Config.WorktreeDir, "_epic-sync-"+epicID)
}

// CmdEpicSync rebases the epic branch onto the main branch. The rebase runs in
// a dedicated worktree that is kept while conflicts are being resolved, so
// --continue/--abort find it again.
func (a *App) CmdEpicSync(epicID string, cont, abort bool) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" || e.Branch == a.Config.MainBranch {
		return fmt.Errorf("epic %s has no branch of its own to sync", epicID)
	}
	path := a.epicSyncWorktree(epicID)
	_, statErr := os.Stat(path)
	haveWorktree := statErr == nil
	wt := NewGitClient(path)

	if cont || abort {
		if !haveWorktree {
			return fmt.Errorf("no epic sync in progress for %s", epicID)
		}
		inProgress, err := wt.RebaseInProgress()
		if err != nil {
			return err
		}
		if !inProgress {
			a.Git.RemoveWorktree(path)
			return fmt.Errorf("no rebase in progress for %s", epicID)
		}
		if abort {
			if err := wt.RebaseAbort(); err != nil {
				return err
			}
			a.Git.RemoveWorktree(path)
			fmt.Printf("Epic sync aborted; %s unchanged.\n", e.Branch)
			return nil
		}
		if err := wt.RebaseContinue(); err != nil {
			return a.epicSyncStopped(e, wt, err)
		}
		return a.epicSyncDone(e, path)
	}

	if haveWorktree {
		return fmt.Errorf("an epic sync for %s is already in progress in %s: resolve it and run harness epic sync --continue %s, or --abort", epicID, path, epicID)
	}
	tasks, err := a.DB.ListTasksByEpic(epicID)
	if err != nil {
		return err
	}
	// A merge into the epic branch rebases onto it and fast-forwards it;
	// rewriting the branch underneath would break that merge.
	for _, t := range tasks {
		if t.State == StateQueued || t.State == StateMerging {
			return fmt.Errorf("cannot sync epic %s while %s is %s into it", epicID, t.ID, t.State)
		}
	}
	if err := a.Git.CreateWorktree(e.Branch, path); err != nil {
		return fmt.Errorf("creating sync worktree: %w", err)
	}
	fmt.Printf("Rebasing %s onto %s...\n", e.Branch, a.Config.MainBranch)
	if err := wt.Rebase("refs/heads/" + a.Config.MainBranch); err != nil {
		return a.epicSyncStopped(e, wt, err)
	}
	return a.epicSyncDone(e, path)
}

func (a *App) epicSyncStopped(e *Epic, wt *GitClient, rebaseErr error) error {
	inProgress, err := wt.RebaseInProgress()
	if err != nil || !inProgress {
		a.Git.RemoveWorktree(wt.dir)
		return fmt.Errorf("rebase failed: %w", rebaseErr)
	}
	files, _ := wt.ConflictedFiles()
	fmt.Printf("Rebase of %s onto %s stopped on conflicts in %s.\n", e.Branch, a.Config.MainBranch, wt.dir)
	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}
	fmt.Printf("Next: resolve them there, git add, then: harness epic sync --continue %s (or --abort)\n", e.ID)
	return fmt.Errorf("epic sync conflicts in %s", e.ID)
}

func (a *App) epicSyncDone(e *Epic, path string) error {
	a.Git.RemoveWorktree(path)
	fmt.Printf("Rebased %s onto %s.\n", e.Branch, a.Config.MainBranch)
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	for _, t := range tasks {
		if t.Branch != "" && t.State != StateMerged && t.State != StateClosed && t.State != StatePlanned {
			fmt.Printf("  %s is based on the old %s: run harness work sync %s\n", t.ID, e.Branch, t.ID)
		}
	}
	return nil
}
