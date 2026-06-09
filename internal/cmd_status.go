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

	fmt.Printf("Base: %s\n", task.BaseBranch)
	if task.ParentTaskID != "" {
		if parent, err := a.DB.GetTask(task.ParentTaskID); err == nil {
			fmt.Printf("Parent: %s %q\n", parent.ID, parent.Description)
		}
	}

	// Show commit range
	if task.State == StateMerged {
		// For merged tasks, the branch is deleted. Show the merge point on base.
		if headShort, err := a.Git.RevParseShort(task.BaseBranch); err == nil {
			fmt.Printf("Merged into: %s (%s)\n", headShort, task.BaseBranch)
		}
	} else {
		// For active tasks, show base..head range
		baseShort, baseErr := a.Git.RevParseShort(task.BaseBranch)
		headShort, headErr := a.Git.RevParseShort(task.Branch)
		if baseErr == nil && headErr == nil {
			fmt.Printf("Commits: %s..%s (%s..%s)\n", baseShort, headShort, task.BaseBranch, task.Branch)
		}
	}

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

	// Skip merge requirements for terminal states
	if task.State != StateMerged {
		fmt.Println("\nMerge requirements:")
		passedStates := map[TaskState]bool{
			StateChecksPassed: true, StateReviewRequested: true,
			StateApproved: true, StateQueued: true, StateMerging: true,
		}
		checksOK := passedStates[task.State]
		reviewOK := task.State == StateApproved || task.State == StateQueued || task.State == StateMerging
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

	subtasks, _ := a.DB.GetSubtasks(task.ID)
	if len(subtasks) > 0 {
		fmt.Println("\nSubtasks:")
		for _, st := range subtasks {
			fmt.Printf("  %-10s %-20s %s\n", st.ID, st.State, st.Description)
		}
	}
	return nil
}
