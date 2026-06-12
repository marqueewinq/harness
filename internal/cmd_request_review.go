package internal

import (
	"fmt"
	"strings"
)

func (a *App) CmdReviewRequest(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	if task.State != StateChecksPassed {
		return fmt.Errorf("cannot request review in state %s (need CHECKS_PASSED)", task.State)
	}
	if err := a.DB.UpdateTaskState(id, StateReviewRequested); err != nil {
		return err
	}

	diff, _ := a.Git.Diff(task.BaseBranch, task.Branch)
	reviews, _ := a.DB.GetReviews(id)
	prevComments := "(none)"
	if len(reviews) > 0 {
		var parts []string
		for _, r := range reviews {
			if r.Comment != "" {
				parts = append(parts, fmt.Sprintf("[%s] %s", r.Decision, r.Comment))
			}
		}
		if len(parts) > 0 {
			prevComments = strings.Join(parts, "\n  ")
		}
	}

	fmt.Printf("Task %s is now REVIEW_REQUESTED.\n\n", id)
	fmt.Printf("Diff (%s..%s):\n%s\n\n", task.BaseBranch, task.Branch, diff)
	fmt.Printf("Task description: %s\n\n", task.Description)
	fmt.Printf("Previous review comments:\n  %s\n\n", prevComments)

	if a.Config.Review.Prompt != "" {
		prompt := a.Config.Review.Prompt
		prompt = strings.ReplaceAll(prompt, "{{previous_comments}}", prevComments)
		prompt = strings.ReplaceAll(prompt, "{{task_description}}", task.Description)
		fmt.Printf("Review prompt:\n%s\n", prompt)
	}

	fmt.Println("---")
	if a.Config.Review.Type == "agent" {
		fmt.Println("For agent review: spawn a reviewer subagent with the above context.")
		fmt.Printf("  The reviewer must call: harness record-review approve %s\n", id)
		fmt.Printf("  or: harness record-review request-changes --comment \"...\" %s\n", id)
	} else {
		fmt.Println("For human review, in another terminal run:")
		fmt.Printf("  harness diff %s\n", id)
		fmt.Printf("  harness record-review approve %s\n", id)
	}
	return nil
}
