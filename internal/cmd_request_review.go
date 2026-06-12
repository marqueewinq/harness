package internal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// composeReviewPrompt builds the full review prompt with template substitution.
func (a *App) composeReviewPrompt(task *Task, diff, prevComments string) string {
	prompt := a.Config.Review.Prompt
	prompt = strings.ReplaceAll(prompt, "{{diff}}", diff)
	prompt = strings.ReplaceAll(prompt, "{{previous_comments}}", prevComments)
	prompt = strings.ReplaceAll(prompt, "{{task_description}}", task.Description)
	return prompt
}

// runReviewer writes the prompt to a temp file, substitutes {{prompt_file}}
// and {{task_id}} in review.cmd, and executes the command from the repo root.
// Blocks until the command exits.
func (a *App) runReviewer(taskID, prompt string) error {
	f, err := os.CreateTemp("", "harness-review-*.txt")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	promptFile := f.Name()
	defer os.Remove(promptFile)

	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		return fmt.Errorf("write prompt: %w", err)
	}
	f.Close()

	cmdStr := a.Config.Review.Cmd
	cmdStr = strings.ReplaceAll(cmdStr, "{{prompt_file}}", promptFile)
	cmdStr = strings.ReplaceAll(cmdStr, "{{task_id}}", taskID)
	fmt.Printf("Running reviewer: %s\n", cmdStr)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = a.RepoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

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

	// Automated reviewer: compose prompt, launch cmd, check result
	if a.Config.Review.Type == "agent" && a.Config.Review.Cmd != "" {
		prompt := a.composeReviewPrompt(task, diff, prevComments)

		fmt.Println("Launching automated reviewer...")
		if err := a.runReviewer(id, prompt); err != nil {
			fmt.Printf("Reviewer command failed: %s\n", err)
		}

		// Check if the reviewer recorded a verdict
		task, _ = a.DB.GetTask(id)
		switch task.State {
		case StateApproved:
			fmt.Printf("\nReviewer approved. Task %s is APPROVED.\n", id)
			fmt.Println("Next: run harness work merge")
		case StateRequestChanges:
			latestReviews, _ := a.DB.GetReviews(id)
			if len(latestReviews) > 0 {
				last := latestReviews[len(latestReviews)-1]
				fmt.Printf("\nReviewer requested changes: %s\n", last.Comment)
			}
			fmt.Printf("Task %s is REQUEST_CHANGES.\n", id)
			fmt.Println("Next: address comments, then run: harness check")
		default:
			fmt.Printf("\nWarning: reviewer exited without recording a verdict.\n")
			fmt.Printf("Task %s is still %s.\n", id, task.State)
			fmt.Printf("Manually run:\n")
			fmt.Printf("  harness review approve %s\n", id)
			fmt.Printf("  harness review reject --comment \"...\" %s\n", id)
		}
		return nil
	}

	// Fallback: write context to file, print compact instructions
	prompt := a.composeReviewPrompt(task, diff, prevComments)
	contextFile, err := os.CreateTemp("", "harness-review-context-*.txt")
	if err == nil {
		contextFile.WriteString(prompt)
		contextFile.Close()
		fmt.Printf("Review context written to: %s\n", contextFile.Name())
	} else {
		// If temp file fails, fall back to stdout
		fmt.Printf("Diff (%s..%s):\n%s\n\n", task.BaseBranch, task.Branch, diff)
	}

	if a.Config.Review.Type == "agent" {
		fmt.Println("IMPORTANT: ALWAYS spawn a reviewer sub-agent, DO NOT review yourself.")
	} else {
		fmt.Println("For human review, in another terminal run:")
		fmt.Printf("  harness diff %s\n", id)
		fmt.Printf("  harness review approve %s\n", id)
	}
	return nil
}
