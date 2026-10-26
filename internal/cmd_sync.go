package internal

import (
	"errors"
	"fmt"
)

var syncableStates = map[TaskState]bool{
	StateInProgress:     true,
	StateChecksFailed:   true,
	StateChecksPassed:   true,
	StateRequestChanges: true,
	StateMergeFailed:    true,
}

func checkSyncable(task *Task) error {
	if syncableStates[task.State] {
		return nil
	}
	switch task.State {
	case StateApproved:
		return fmt.Errorf("cannot sync %s in state APPROVED: run harness work check first, then harness work sync", task.ID)
	case StateReviewRequested:
		return fmt.Errorf("cannot sync %s in state REVIEW_REQUESTED: wait for the review verdict", task.ID)
	case StateQueued, StateMerging:
		return fmt.Errorf("cannot sync %s in state %s: the task is in the merge queue", task.ID, task.State)
	default:
		return fmt.Errorf("cannot sync %s in state %s", task.ID, task.State)
	}
}

func (a *App) CmdWorkSync(taskID string, cont, abort bool) error {
	if cont && abort {
		return errors.New("--continue and --abort are mutually exclusive")
	}
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	wt := NewGitClient(task.WorktreePath)

	if abort {
		if task.State == StateQueued || task.State == StateMerging {
			return fmt.Errorf("cannot abort in state %s: the merge in progress owns this worktree's rebase", task.State)
		}
		inProgress, err := wt.RebaseInProgress()
		if err != nil {
			return err
		}
		if !inProgress {
			return fmt.Errorf("no rebase in progress in %s", task.WorktreePath)
		}
		if err := wt.RebaseAbort(); err != nil {
			return err
		}
		fmt.Printf("Rebase aborted; %s restored to its pre-sync state.\n", task.Branch)
		return nil
	}

	if err := checkSyncable(task); err != nil {
		return err
	}
	inProgress, err := wt.RebaseInProgress()
	if err != nil {
		return err
	}
	before, err := a.Git.RevParse(task.Branch)
	if err != nil {
		return err
	}

	if cont {
		if !inProgress {
			return fmt.Errorf("no rebase in progress in %s", task.WorktreePath)
		}
		if err := wt.RebaseContinue(); err != nil {
			return a.syncFailed(task, wt, err)
		}
		return a.syncDone(task, before)
	}

	if inProgress {
		return fmt.Errorf("a rebase is already in progress in %s: resolve it and run harness work sync --continue, or harness work sync --abort", task.WorktreePath)
	}
	status, err := wt.StatusPorcelain()
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("worktree %s has uncommitted changes; commit or discard them before syncing:\n%s", task.WorktreePath, status)
	}
	fmt.Printf("Rebasing %s onto %s...\n", task.Branch, task.BaseBranch)
	if err := wt.Rebase(task.BaseBranch); err != nil {
		return a.syncFailed(task, wt, err)
	}
	return a.syncDone(task, before)
}

func (a *App) syncFailed(task *Task, wt *GitClient, rebaseErr error) error {
	inProgress, err := wt.RebaseInProgress()
	if err != nil || !inProgress {
		return fmt.Errorf("rebase failed: %w", rebaseErr)
	}
	files, _ := wt.ConflictedFiles()
	fmt.Printf("Rebase of %s onto %s stopped on conflicts in %s.\n", task.Branch, task.BaseBranch, task.WorktreePath)
	if len(files) > 0 {
		fmt.Println("Conflicted files:")
		for _, f := range files {
			fmt.Printf("  %s\n", f)
		}
	}
	fmt.Println("Next: resolve the conflicts, git add the resolved files, then run: harness work sync --continue")
	fmt.Println("      or give up with: harness work sync --abort")
	return fmt.Errorf("rebase conflicts in %s", task.ID)
}

// syncDone: a moved branch head invalidates earlier check results and
// approvals, so IN_PROGRESS forces checks to run again.
func (a *App) syncDone(task *Task, before string) error {
	after, err := a.Git.RevParse(task.Branch)
	if err != nil {
		return err
	}
	if after == before {
		fmt.Printf("%s is already up to date with %s.\n", task.Branch, task.BaseBranch)
		return nil
	}
	if task.State != StateInProgress {
		if err := a.DB.UpdateTaskState(task.ID, StateInProgress); err != nil {
			return err
		}
	}
	fmt.Printf("Rebased %s onto %s.\n", task.Branch, task.BaseBranch)
	fmt.Println("Next: harness work check")
	return nil
}
