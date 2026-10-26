package internal

import (
	"fmt"
	"path/filepath"
)

func (a *App) CmdEpicMerge(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}
	if e.Branch == a.Config.MainBranch {
		return fmt.Errorf("epic %s branch is %s (nothing to merge)", epicID, a.Config.MainBranch)
	}

	// Check for approval
	latestApproval, err := a.DB.GetLatestEpicApproval(epicID)
	if err != nil {
		return err
	}
	if latestApproval == nil {
		return fmt.Errorf("epic %s has no approval. Run: harness review request %s", epicID, epicID)
	}

	// Staleness check
	currentHead, err := a.Git.RevParse(e.Branch)
	if err != nil {
		return fmt.Errorf("cannot get HEAD of %s: %w", e.Branch, err)
	}
	if latestApproval.CommitSHA != "" && currentHead != latestApproval.CommitSHA {
		fmt.Printf("Run: harness review request %s\n", epicID)
		return fmt.Errorf("review is stale: approved commit %s but branch is now at %s",
			shortSHA(latestApproval.CommitSHA), shortSHA(currentHead))
	}

	// Warn about open tasks
	tasks, _ := a.DB.ListTasksByEpic(epicID)
	done, total := CountDone(tasks)
	if openCount := total - done; openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this epic\n", openCount)
	}

	fmt.Printf("Merging epic %s branch %s into %s...\n", epicID, e.Branch, a.Config.MainBranch)

	strategy, err := NewMergeStrategy(a.Config.Merge.Strategy)
	if err != nil {
		return err
	}

	tmpWorktree := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, "_epic-merge-"+epicID)
	a.Git.RemoveWorktree(tmpWorktree)
	if err := a.Git.CreateWorktree(e.Branch, tmpWorktree); err != nil {
		return fmt.Errorf("creating temp worktree: %w", err)
	}

	wtGit := NewGitClient(tmpWorktree)
	fmt.Printf("Preparing %s for %s merge into %s...\n", e.Branch, strategy.Name(), a.Config.MainBranch)
	if err := strategy.Prepare(wtGit, a.Config.MainBranch); err != nil {
		wtGit.RebaseAbort()
		a.Git.RemoveWorktree(tmpWorktree)
		return fmt.Errorf("prepare failed: %w", err)
	}

	// Rerun checks if configured
	if a.Config.Merge.RerunChecks {
		fmt.Println("Re-running checks...")
		runner := NewCheckRunner(a.Config.Checks, tmpWorktree)
		results, err := runner.RunAll()
		if err != nil {
			a.Git.RemoveWorktree(tmpWorktree)
			return fmt.Errorf("check execution error: %w", err)
		}
		if !PrintCheckResults(results) {
			a.Git.RemoveWorktree(tmpWorktree)
			return fmt.Errorf("checks failed after rebase; fix and retry")
		}
	}

	fmt.Printf("Merging %s into %s (strategy: %s)...\n", e.Branch, a.Config.MainBranch, strategy.Name())
	// The epic branch is checked out in tmpWorktree; free it so the branch can
	// be deleted, and so mergeIntoTarget doesn't mistake it for the target.
	a.Git.RemoveWorktree(tmpWorktree)
	message := BuildCommitMessage(a.Config.Commit.Message, "", epicID, e.ProjectID, e.Title)
	if err := a.mergeIntoTarget(strategy, e.Branch, a.Config.MainBranch, message, epicID); err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}

	a.Git.DeleteBranch(e.Branch)
	a.DB.UpdateEpicBranch(epicID, "")
	a.DB.UpdateEpicStatus(epicID, EpicStatusClosed)

	fmt.Println("Merge successful.")
	fmt.Printf("Epic %s is now closed.\n", epicID)
	fmt.Printf("Branch %s deleted.\n", e.Branch)
	return nil
}
