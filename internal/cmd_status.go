package internal

import (
	"fmt"
	"strings"
)

func (a *App) CmdStatus(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	fmt.Printf("Task: %s %q\n", task.ID, task.Description)
	fmt.Printf("Branch: %s\n", task.Branch)
	fmt.Printf("Worktree: %s\n", task.WorktreePath)
	fmt.Printf("State: %s\n", task.State)

	runs, err := a.DB.GetLatestCheckRuns(task.ID)
	if err == nil && len(runs) > 0 {
		fmt.Println("\nLast check run:")
		for _, r := range runs {
			if r.ExitCode == -1 {
				fmt.Printf("  %-12s SKIP\n", r.CheckName)
				continue
			}
			status := "PASS"
			if r.ExitCode != 0 {
				status = "FAIL"
			}
			dur := r.FinishedAt.Sub(r.StartedAt)
			fmt.Printf("  %-12s %-6s (%.1fs)\n", r.CheckName, status, dur.Seconds())
			if r.ExitCode != 0 && r.Stderr != "" {
				for _, line := range strings.Split(strings.TrimSpace(r.Stderr), "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
		}
	}

	reviews, err := a.DB.GetReviews(task.ID)
	if err == nil && len(reviews) > 0 {
		latest := reviews[len(reviews)-1]
		fmt.Printf("\nLatest review: %s\n", strings.ToUpper(latest.Decision))
		if latest.Comment != "" {
			fmt.Printf("  %s\n", latest.Comment)
		}
	} else {
		fmt.Println("\nReview: not requested")
	}

	fmt.Println("\nMerge requirements:")
	checksOK := task.State == StateChecksPassed || task.State == StateReviewRequested || task.State == StateApproved || task.State == StateQueued
	reviewOK := task.State == StateApproved || task.State == StateQueued
	mark := func(ok bool) string {
		if ok {
			return "[x]"
		}
		return "[ ]"
	}
	fmt.Printf("  %s Work in worktree\n", mark(task.WorktreePath != ""))
	fmt.Printf("  %s All checks pass\n", mark(checksOK))
	if a.Config.Merge.Require.Review == "approved" {
		fmt.Printf("  %s Review approved\n", mark(reviewOK))
	}

	fmt.Println()
	switch task.State {
	case StateInProgress:
		fmt.Println("Next: run harness check")
	case StateChecksFailed, StateMergeFailed:
		fmt.Println("Next: fix the failures, then run: harness check")
	case StateChecksPassed:
		fmt.Println("Next: run harness request-review")
	case StateRequestChanges:
		fmt.Println("Next: address review comments, then run: harness check")
	case StateReviewRequested:
		if a.Config.Review.Type == "human" {
			fmt.Println("Next: waiting for human review.")
			fmt.Printf("      In another terminal: harness record-review approve %s\n", task.ID)
		} else {
			fmt.Println("Next: spawn a reviewer subagent, then: harness record-review approve/request-changes")
		}
	case StateApproved:
		fmt.Println("Next: run harness merge")
	case StateQueued:
		pos, _ := a.DB.GetQueuePosition(task.ID)
		fmt.Printf("Queued at position #%d. harness merge is blocking.\n", pos)
	case StateMerged:
		fmt.Println("Task is complete.")
	}
	return nil
}
