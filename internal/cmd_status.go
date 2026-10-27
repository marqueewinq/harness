package internal

import (
	"fmt"
	"strings"
	"time"
)

func (a *App) CmdWorkStatus(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	fmt.Printf("Task: %s %q\n", task.ID, task.Title)
	if task.Description != "" {
		fmt.Printf("Description: %s\n", task.Description)
	}
	printNotes(task.Notes)
	fmt.Printf("Branch: %s\n", task.Branch)
	fmt.Printf("Worktree: %s\n", task.WorktreePath)
	fmt.Printf("State: %s\n", task.State)
	a.printMergeFailure(task)
	fmt.Printf("Review type: %s\n", FormatReviewType(task.ReviewType, task.ReviewTypeSetBy, task.ReviewTypeSetAt))

	fmt.Printf("Base: %s\n", task.BaseBranch)
	if task.ParentTaskID != "" {
		if parent, err := a.DB.GetTask(task.ParentTaskID); err == nil {
			fmt.Printf("Parent: %s %q\n", parent.ID, parent.Title)
		}
	}

	// Show epic/project context
	if task.EpicID != "" {
		if epic, err := a.DB.GetEpic(task.EpicID); err == nil {
			fmt.Printf("Epic: %s %q\n", epic.ID, epic.Title)
			if proj, err := a.DB.GetProject(epic.ProjectID); err == nil {
				fmt.Printf("Project: %s %q\n", proj.ID, proj.Title)
			}
		}
	}

	a.printDependencies(task.ID)

	// Show commit range
	if task.State == StatePlanned {
		// No commits for planned tasks
	} else if task.State == StateMerged {
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
		header := "\nLast check run:"
		if runs[0].CommitSHA != "" {
			short := runs[0].CommitSHA
			if len(short) > 8 {
				short = short[:8]
			}
			header = fmt.Sprintf("\nLast check run (commit %s):", short)
		}
		fmt.Println(header)
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
	if req, err := a.DB.GetLatestReviewRequest(task.ID, ""); err == nil && req != nil && req.ReviewerOutcome != "" {
		line, status := FormatReviewerRun(req, time.Now())
		fmt.Printf("Reviewer run: %s\n", line)
		switch status {
		case ReviewerOrphaned:
			fmt.Println("  The harness review request that launched it is gone (killed or crashed).")
			fallthrough
		case ReviewerNoVerdict, ReviewerFailed, ReviewerTimeout, ReviewerInterrupted:
			if task.State == StateReviewRequested {
				fmt.Printf("  No verdict. Run: harness work check %s, then harness review request %s\n", task.ID, task.ID)
			}
		}
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
		if task.ReviewType == ReviewNone {
			fmt.Println("  [-] Review not required (review type none)")
		} else {
			fmt.Printf("  %s Review approved\n", mark(reviewOK))
		}
	}

	fmt.Println()
	switch task.State {
	case StateInProgress:
		fmt.Println("Next: run harness work check")
	case StateChecksFailed, StateMergeFailed:
		fmt.Println("Next: fix the failures, then run: harness work check")
	case StateChecksPassed:
		switch task.ReviewType {
		case ReviewNone:
			fmt.Println("Next: run harness work merge (review type none: no review step)")
		case ReviewHuman:
			fmt.Println("Next: run harness review request; then a human approves or rejects at a terminal or in harness dashboard")
		default:
			fmt.Println("Next: run harness review request")
		}
	case StateRequestChanges:
		fmt.Println("Next: address review comments, then run: harness work check")
	case StateReviewRequested:
		if task.ReviewType == ReviewHuman {
			fmt.Println("Next: waiting for a human verdict (review type human).")
			fmt.Printf("      At an interactive terminal: harness review approve|reject %s, or a/r in harness dashboard\n", task.ID)
		} else if a.Config.Review.Cmd != "" {
			fmt.Println("Next: if no reviewer is running, run harness work check, then harness review request")
		} else if a.Config.Review.Type == "human" {
			fmt.Println("Next: waiting for human review.")
			fmt.Printf("      In another terminal: harness review approve %s\n", task.ID)
		} else {
			fmt.Println("Next: spawn a reviewer subagent, then: harness review approve/reject")
		}
	case StateApproved:
		fmt.Println("Next: run harness work merge")
	case StateQueued:
		pos, _ := a.DB.GetQueuePosition(task.ID)
		fmt.Printf("Queued at position #%d. harness work merge is blocking.\n", pos)
	case StatePlanned:
		fmt.Printf("Next: run harness task start %s\n", task.ID)
	case StateMerged:
		fmt.Println("Task is complete.")
	}

	transitions, _ := a.DB.GetTaskTransitions(task.ID)
	if len(transitions) > 0 {
		fmt.Println("\nState history:")
		for _, tr := range transitions {
			from := tr.FromState
			if from == "" {
				from = "(created)"
			}
			note := ""
			if tr.Note != "" {
				note = "  (" + tr.Note + ")"
			}
			fmt.Printf("  %s  %s -> %s%s\n", tr.CreatedAt.Format("15:04:05"), from, tr.ToState, note)
		}
	}

	subtasks, _ := a.DB.GetSubtasks(task.ID)
	if len(subtasks) > 0 {
		fmt.Println("\nSubtasks:")
		for _, st := range subtasks {
			fmt.Printf("  %-10s %-20s %s\n", st.ID, st.State, st.Title)
		}
	}
	return nil
}
