package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// failMerge leaves the queue and marks the task MERGE_FAILED; the returned
// error carries cause so the command exits non-zero.
func (a *App) failMerge(id string, cause error) error {
	a.DB.DequeueOwned(id, os.Getpid())
	reason := mergeFailureReason(cause)
	a.mergeLogf(id, "MERGE FAILED: %v", cause)
	if err := a.DB.UpdateTaskStateNote(id, StateMergeFailed, reason); err != nil {
		return fmt.Errorf("%w (also failed to mark %s MERGE_FAILED: %v)", cause, id, err)
	}
	fmt.Printf("\nTask %s is now MERGE_FAILED: %s\n", id, reason)
	fmt.Printf("Merge log: %s\n", MergeLogPath(a.RepoRoot, id))
	return cause
}

// errQueueEntryLost: someone else removed (and may have re-enqueued) this
// task, so its state is no longer ours to change.
var errQueueEntryLost = errors.New("removed from the merge queue by another process")

// waitForQueueHead returns once id is first in the queue, reclaiming dead or
// expired leases. Cancelling ctx (Ctrl-C) gives up the slot.
func (a *App) waitForQueueHead(ctx context.Context, id string) error {
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
		if err := a.reclaimStaleEntries(); err != nil {
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

// requireChecksAt enforces merge.require.checks against the latest check run
// on exactly the commit the merge is pinned to (the approved one, or for
// review type none the checked head), so the merge cannot carry checks that
// ran on different code. pinned names that commit in the error.
func (a *App) requireChecksAt(id, sha, pinned, retry string) error {
	required := a.Config.Merge.Require.Checks
	if len(required) == 0 {
		return nil
	}
	passed, err := a.DB.PassedChecksAt(id, sha)
	if err != nil {
		return err
	}
	var missing []string
	for _, name := range required {
		if !passed[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required checks not passed on %s commit %s: %s (run: harness work check %s%s)",
			pinned, shortSHA(sha), strings.Join(missing, ", "), id, retry)
	}
	return nil
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
	a.showNewNotes(id)
	if task.State == StateQueued || task.State == StateMerging {
		return a.recoverAbandonedMerge(id)
	}
	unreviewed := task.State == StateChecksPassed && task.ReviewType == ReviewNone
	if task.State != StateApproved && !unreviewed {
		if task.ReviewType == ReviewNone {
			return fmt.Errorf("cannot merge in state %s (need CHECKS_PASSED or APPROVED)", task.State)
		}
		return fmt.Errorf("cannot merge in state %s (need APPROVED; review type %s)", task.State, task.ReviewType)
	}

	unreviewedNote := ""
	// pinned is the commit the gate vouched for (checked or approved); it is
	// re-verified once the queue is ours, since the branch can move while
	// waiting.
	var pinned string
	if unreviewed {
		if pinned, err = a.requireCheckedHead(task); err != nil {
			return err
		}
		unreviewedNote = unreviewedMergeNote(task.ReviewSummary())
		fmt.Printf("Task %s has review type none: %s.\n", id, unreviewedNote)
	} else {
		latestApproval, err := a.DB.GetLatestApproval(id)
		if err != nil {
			return err
		}
		if latestApproval == nil || latestApproval.CommitSHA == "" {
			return fmt.Errorf("task %s has no approval pinned to a commit; run: harness work check %s, then harness review request %s", id, id, id)
		}
		// A pre-approval only stands while the type is none: moving the type
		// away (t picker) without the p toggle must not leave it mergeable.
		if IsPreApproval(latestApproval.Reviewer) && task.ReviewType != ReviewNone {
			return fmt.Errorf("task %s's latest approval is a pre-approval (%s), and its review type is now %s; run: harness work check %s, then harness review request %s",
				id, latestApproval.Reviewer, task.ReviewType, id, id)
		}
		currentHead, err := a.Git.RevParse(task.Branch)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", task.Branch, err)
		}
		if currentHead != latestApproval.CommitSHA {
			if err := a.DB.UpdateTaskState(id, StateInProgress); err != nil {
				return err
			}
			fmt.Printf("Task %s is now IN_PROGRESS.\n", id)
			fmt.Println("Next: run harness work check")
			return fmt.Errorf("review is stale: approved commit %s but branch is now at %s",
				shortSHA(latestApproval.CommitSHA), shortSHA(currentHead))
		}
		if err := a.requireChecksAt(id, latestApproval.CommitSHA, "approved", ", then request review again"); err != nil {
			return err
		}
		pinned = latestApproval.CommitSHA
	}

	// From here on Ctrl-C/SIGTERM must not kill harness mid-merge: the merge
	// is cancelled at the next step (a terminal Ctrl-C also stops git
	// children), and every exit path releases the lease.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	a.startMergeLog(id)
	// The lease row exists before the task reads QUEUED, so a concurrent
	// recoverAbandonedMerge always sees a live owner.
	if _, err := a.DB.EnqueueTask(id, os.Getpid()); err != nil {
		return fmt.Errorf("enqueue %s: %w", id, err)
	}
	queue := func() error { return a.DB.UpdateTaskState(id, StateQueued) }
	if unreviewed {
		queue = func() error { return a.DB.QueueWithoutReview(id, unreviewedNote) }
	}
	if err := queue(); err != nil {
		a.DB.DequeueOwned(id, os.Getpid())
		return err
	}
	pos, err := a.DB.GetQueuePosition(id)
	if err != nil {
		return a.failMerge(id, fmt.Errorf("reading merge queue position: %w", err))
	}
	stopHeartbeat := a.startHeartbeat(id)
	defer stopHeartbeat()
	defer func() {
		if owned, err := a.DB.OwnsQueueEntry(id, os.Getpid()); err == nil && owned {
			a.failMerge(id, errors.New("merge did not finish (harness exited mid-merge)"))
		}
	}()
	fmt.Printf("Queued for merge (position #%d).\n", pos)

	a.mergeLogf(id, "queued at position #%d", pos)

	if err := a.waitForQueueHead(ctx, id); err != nil {
		if errors.Is(err, errQueueEntryLost) {
			a.mergeLogf(id, "%v", err)
			return fmt.Errorf("%s: %w", id, err)
		}
		return a.failMerge(id, err)
	}

	fmt.Printf("\nNow processing merge for %s...\n", id)
	a.mergeLogf(id, "reached the head of the merge queue")
	if err := a.DB.UpdateTaskState(id, StateMerging); err != nil {
		return a.failMerge(id, fmt.Errorf("marking %s MERGING: %w", id, err))
	}

	if head, err := a.Git.RevParse(task.Branch); err != nil {
		return a.failMerge(id, fmt.Errorf("resolving %s: %w", task.Branch, err))
	} else if head != pinned {
		return a.failMerge(id, fmt.Errorf("branch moved while waiting in the merge queue (%s -> %s); re-run checks and review", shortSHA(pinned), shortSHA(head)))
	}

	strategy, err := NewMergeStrategy(a.Config.Merge.Strategy)
	if err != nil {
		return a.failMerge(id, err)
	}

	targetBranch := task.BaseBranch

	wtGit := NewGitClient(task.WorktreePath)
	fmt.Printf("Preparing %s for %s merge into %s...\n", task.Branch, strategy.Name(), targetBranch)
	a.mergeLogf(id, "preparing %s for %s merge into %s", task.Branch, strategy.Name(), targetBranch)
	if err := strategy.Prepare(wtGit, targetBranch); err != nil {
		wtGit.RebaseAbort()
		fmt.Println("Next: harness work sync (rebase onto the latest base and resolve conflicts), then harness work check")
		return a.failMerge(id, fmt.Errorf("prepare failed (%s onto %s): %w", strategy.Name(), targetBranch, err))
	}

	if ctx.Err() != nil {
		return a.failMerge(id, errors.New("interrupted after prepare"))
	}

	if a.Config.Merge.RerunChecks {
		fmt.Println("Re-running checks...")
		a.mergeLogf(id, "re-running checks")
		runner := NewCheckRunner(a.Config.Checks, task.WorktreePath)
		results, err := runner.RunAll()
		if err != nil {
			return a.failMerge(id, fmt.Errorf("check execution error: %w", err))
		}
		a.mergeLogChecks(id, results)
		if !PrintCheckResults(results) {
			if ctx.Err() != nil {
				// The interrupt killed the checks; their failure is not the reason.
				return a.failMerge(id, errors.New("interrupted while re-running checks"))
			}
			fmt.Println("Fix the failures, then run: harness work check")
			return a.failMerge(id, fmt.Errorf("checks failed after rebase: %s", failedCheckNames(results)))
		}
	}

	if ctx.Err() != nil {
		return a.failMerge(id, errors.New("interrupted after checks"))
	}

	fmt.Printf("Merging %s into %s (strategy: %s)...\n", task.Branch, targetBranch, strategy.Name())
	a.mergeLogf(id, "merging %s into %s (strategy: %s)", task.Branch, targetBranch, strategy.Name())
	epicID, projID := a.resolveTaskHierarchy(task)
	mergeMessage := BuildCommitMessage(a.Config.Commit.Message, task.ID, epicID, projID, task.Title)
	if unreviewedNote != "" {
		mergeMessage += "\n\n" + unreviewedNote
	}

	if err := a.mergeIntoTarget(strategy, task.Branch, targetBranch, mergeMessage, id); err != nil {
		fmt.Println("Next: harness work sync (rebase onto the latest base and resolve conflicts), then harness work check")
		return a.failMerge(id, fmt.Errorf("merge failed (into %s): %w", targetBranch, err))
	}

	// MERGED before releasing the lease: a leaseless MERGING task would look
	// abandoned to a concurrent recovery.
	if err := a.DB.UpdateTaskState(id, StateMerged); err != nil {
		return err
	}
	a.DB.DequeueOwned(id, os.Getpid())
	a.mergeLogf(id, "merged")
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
