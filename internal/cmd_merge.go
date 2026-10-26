package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// failMerge leaves the queue and marks the task MERGE_FAILED; the returned
// error carries cause so the command exits non-zero.
func (a *App) failMerge(id string, cause error) error {
	a.DB.DequeueOwned(id, os.Getpid())
	if err := a.DB.UpdateTaskState(id, StateMergeFailed); err != nil {
		return fmt.Errorf("%w (also failed to mark %s MERGE_FAILED: %v)", cause, id, err)
	}
	fmt.Printf("\nTask %s is now MERGE_FAILED.\n", id)
	return cause
}

// errQueueEntryLost: someone else removed (and may have re-enqueued) this
// task, so its state is no longer ours to change.
var errQueueEntryLost = errors.New("removed from the merge queue by another process")

// waitForQueueHead returns once id is first in the queue, reclaiming dead or
// expired leases ahead of it. Ctrl-C while waiting gives up the slot.
func (a *App) waitForQueueHead(id string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deadline := time.Now().Add(time.Duration(a.Config.Merge.QueueTimeout) * time.Second)
	lastShown := ""
	for {
		owned, err := a.DB.OwnsQueueEntry(id, os.Getpid())
		if err != nil {
			return fmt.Errorf("reading merge queue: %w", err)
		}
		if !owned {
			return errQueueEntryLost
		}
		if err := a.reclaimStaleHead(); err != nil {
			return fmt.Errorf("reclaiming stale queue entry: %w", err)
		}
		first, err := a.DB.PeekQueue()
		if err != nil {
			return fmt.Errorf("peek queue: %w", err)
		}
		if first == id {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("merge queue timeout after %ds (waiting for %s)", a.Config.Merge.QueueTimeout, first)
		}
		if first != lastShown {
			fmt.Printf("Waiting for %s...\n", first)
			lastShown = first
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("interrupted while waiting in merge queue")
		case <-time.After(queuePollEvery):
		}
	}
}

func (a *App) CmdWorkMerge(taskID string) error {
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

	latestApproval, err := a.DB.GetLatestApproval(id)
	if err != nil {
		return err
	}
	if latestApproval != nil && latestApproval.CommitSHA != "" {
		currentHead, err := a.Git.RevParse(task.Branch)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", task.Branch, err)
		}
		if currentHead != latestApproval.CommitSHA {
			if err := a.DB.UpdateTaskState(id, StateInProgress); err != nil {
				return err
			}
			fmt.Printf("Task %s is now IN_PROGRESS.\n", id)
			fmt.Println("Next: run harness check")
			return fmt.Errorf("review is stale: approved commit %s but branch is now at %s",
				shortSHA(latestApproval.CommitSHA), shortSHA(currentHead))
		}
	}

	if err := a.DB.UpdateTaskState(id, StateQueued); err != nil {
		return err
	}
	pos, err := a.DB.EnqueueTask(id, os.Getpid())
	if err != nil {
		return a.failMerge(id, fmt.Errorf("enqueue: %w", err))
	}
	stopHeartbeat := a.startHeartbeat(id)
	defer stopHeartbeat()
	fmt.Printf("Queued for merge (position #%d).\n", pos)

	if err := a.waitForQueueHead(id); err != nil {
		if errors.Is(err, errQueueEntryLost) {
			return fmt.Errorf("%s: %w", id, err)
		}
		return a.failMerge(id, err)
	}

	fmt.Printf("\nNow processing merge for %s...\n", id)
	if err := a.DB.UpdateTaskState(id, StateMerging); err != nil {
		return a.failMerge(id, err)
	}

	strategy, err := NewMergeStrategy(a.Config.Merge.Strategy)
	if err != nil {
		return a.failMerge(id, err)
	}

	targetBranch := task.BaseBranch

	wtGit := NewGitClient(task.WorktreePath)
	fmt.Printf("Preparing %s for %s merge into %s...\n", task.Branch, strategy.Name(), targetBranch)
	if err := strategy.Prepare(wtGit, targetBranch); err != nil {
		wtGit.RebaseAbort()
		fmt.Println("Fix the conflicts, then run: harness work check")
		return a.failMerge(id, fmt.Errorf("prepare failed: %w", err))
	}

	if a.Config.Merge.RerunChecks {
		fmt.Println("Re-running checks...")
		runner := NewCheckRunner(a.Config.Checks, task.WorktreePath)
		results, err := runner.RunAll()
		if err != nil {
			return a.failMerge(id, fmt.Errorf("check execution error: %w", err))
		}
		if !PrintCheckResults(results) {
			fmt.Println("Fix the failures, then run: harness work check")
			return a.failMerge(id, fmt.Errorf("checks failed after rebase"))
		}
	}

	// A task-branch target is checked out in its own worktree; anything else
	// (main, epic branches) is merged in the main checkout.
	mergeGit := a.Git
	if targetBranch != a.Config.MainBranch {
		if parentTask, err := a.DB.GetTaskByBranch(targetBranch); err == nil {
			mergeGit = NewGitClient(parentTask.WorktreePath)
		} else if err := a.Git.Checkout(targetBranch); err != nil {
			return a.failMerge(id, fmt.Errorf("checkout %s: %w", targetBranch, err))
		}
	}

	fmt.Printf("Merging %s into %s (strategy: %s)...\n", task.Branch, targetBranch, strategy.Name())
	if targetBranch == a.Config.MainBranch {
		if err := a.Git.Checkout(targetBranch); err != nil {
			return a.failMerge(id, fmt.Errorf("checkout %s: %w", targetBranch, err))
		}
	}
	epicID, projID := a.resolveTaskHierarchy(task)
	mergeMessage := BuildCommitMessage(a.Config.Commit.Message, task.ID, epicID, projID, task.Title)

	if err := strategy.Merge(mergeGit, task.Branch, targetBranch, mergeMessage); err != nil {
		fmt.Println("Fix the conflicts, then run: harness work check")
		return a.failMerge(id, fmt.Errorf("merge failed: %w", err))
	}

	a.DB.DequeueOwned(id, os.Getpid())
	if err := a.DB.UpdateTaskState(id, StateMerged); err != nil {
		return err
	}
	fmt.Println("Merge successful.")

	fmt.Printf("\nCleaned up:\n")
	if err := a.Git.RemoveWorktree(task.WorktreePath); err != nil {
		fmt.Printf("  Worktree: %s (NOT removed: %v)\n", task.WorktreePath, err)
	} else {
		fmt.Printf("  Worktree: %s (removed)\n", task.WorktreePath)
	}
	if err := a.Git.DeleteBranch(task.Branch); err != nil {
		fmt.Printf("  Branch: %s (NOT deleted: %v)\n", task.Branch, err)
	} else {
		fmt.Printf("  Branch: %s (deleted)\n", task.Branch)
	}
	fmt.Printf("\nTask %s is now MERGED.\n", id)
	return nil
}
