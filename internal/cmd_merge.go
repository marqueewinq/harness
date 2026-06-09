package internal

import (
	"fmt"
	"time"
)

func (a *App) CmdMerge(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	if task.State != StateApproved {
		return fmt.Errorf("cannot merge in state %s (need APPROVED)", task.State)
	}

	pos, err := a.DB.EnqueueTask(id)
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	if err := a.DB.UpdateTaskState(id, StateQueued); err != nil {
		return err
	}
	fmt.Printf("Queued for merge (position #%d).\n", pos)

	for {
		first, err := a.DB.PeekQueue()
		if err != nil {
			return fmt.Errorf("peek queue: %w", err)
		}
		if first == id {
			break
		}
		fmt.Printf("Waiting for %s...\n", first)
		time.Sleep(2 * time.Second)
	}

	fmt.Printf("\nNow processing merge for %s...\n", id)
	if err := a.DB.UpdateTaskState(id, StateMerging); err != nil {
		return err
	}

	strategy, err := NewMergeStrategy(a.Config.Merge.Strategy)
	if err != nil {
		a.DB.DequeueTask(id)
		a.DB.UpdateTaskState(id, StateMergeFailed)
		return err
	}

	targetBranch := task.BaseBranch

	// Prepare (e.g., rebase)
	wtGit := NewGitClient(task.WorktreePath)
	fmt.Printf("Preparing %s for %s merge into %s...\n", task.Branch, strategy.Name(), targetBranch)
	if err := strategy.Prepare(wtGit, targetBranch); err != nil {
		wtGit.RebaseAbort()
		a.DB.DequeueTask(id)
		a.DB.UpdateTaskState(id, StateMergeFailed)
		fmt.Printf("Prepare failed: %s\n", err)
		fmt.Printf("\nTask %s is now MERGE_FAILED.\n", id)
		fmt.Println("Fix the conflicts, then run: harness check")
		return nil
	}

	if a.Config.Merge.RerunChecks {
		fmt.Println("Re-running checks...")
		runner := NewCheckRunner(a.Config.Checks, task.WorktreePath)
		results, err := runner.RunAll()
		if err != nil {
			a.DB.DequeueTask(id)
			a.DB.UpdateTaskState(id, StateMergeFailed)
			return fmt.Errorf("check execution error: %w", err)
		}
		allPassed := true
		for _, r := range results {
			if r.Skipped {
				fmt.Printf("  %-12s SKIP\n", r.Name)
			} else if r.ExitCode == 0 {
				fmt.Printf("  %-12s PASS\n", r.Name)
			} else {
				fmt.Printf("  %-12s FAIL\n", r.Name)
				allPassed = false
			}
		}
		if !allPassed {
			a.DB.DequeueTask(id)
			a.DB.UpdateTaskState(id, StateMergeFailed)
			fmt.Printf("\nChecks failed.\n")
			fmt.Printf("Task %s is now MERGE_FAILED.\n", id)
			fmt.Println("Fix the failures, then run: harness check")
			return nil
		}
	}

	// Merge
	// Determine which git client to use for the merge.
	// If the target branch is checked out in a worktree (i.e., it's another task's branch),
	// we must merge from that worktree instead of the main repo.
	mergeGit := a.Git
	if targetBranch != a.Config.MainBranch {
		if parentTask, err := a.DB.GetTaskByBranch(targetBranch); err == nil {
			mergeGit = NewGitClient(parentTask.WorktreePath)
		}
	}

	fmt.Printf("Merging %s into %s (strategy: %s)...\n", task.Branch, targetBranch, strategy.Name())
	if targetBranch == a.Config.MainBranch || mergeGit != a.Git {
		// For main branch: checkout in main repo. For worktree targets: already on correct branch.
	}
	if targetBranch == a.Config.MainBranch {
		if err := a.Git.Checkout(targetBranch); err != nil {
			a.DB.DequeueTask(id)
			a.DB.UpdateTaskState(id, StateMergeFailed)
			return fmt.Errorf("checkout %s: %w", targetBranch, err)
		}
	}
	if err := strategy.Merge(mergeGit, task.Branch, targetBranch, task.Description); err != nil {
		a.DB.DequeueTask(id)
		a.DB.UpdateTaskState(id, StateMergeFailed)
		fmt.Printf("Merge failed: %s\n", err)
		fmt.Printf("\nTask %s is now MERGE_FAILED.\n", id)
		fmt.Println("Fix the conflicts, then run: harness check")
		return nil
	}

	a.Git.RemoveWorktree(task.WorktreePath)
	a.Git.DeleteBranch(task.Branch)
	a.DB.DequeueTask(id)
	a.DB.UpdateTaskState(id, StateMerged)

	fmt.Println("Merge successful.")
	fmt.Printf("\nCleaned up:\n")
	fmt.Printf("  Worktree: %s (removed)\n", task.WorktreePath)
	fmt.Printf("  Branch: %s (deleted)\n", task.Branch)
	fmt.Printf("\nTask %s is now MERGED.\n", id)
	return nil
}
