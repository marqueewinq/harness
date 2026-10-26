package internal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (a *App) composeReviewPrompt(subjectID, title, diff, prevComments string) string {
	prompt := a.Config.Review.Prompt
	prompt = strings.ReplaceAll(prompt, "{{diff}}", diff)
	prompt = strings.ReplaceAll(prompt, "{{previous_comments}}", prevComments)
	prompt = strings.ReplaceAll(prompt, "{{task_description}}", title)
	prompt = strings.ReplaceAll(prompt, "{{task_id}}", subjectID)
	return prompt
}

// agentReviewNeedsCmd: an agent reviewer can only record a verdict with the
// request's token, and harness hands that token only to a process it launches.
func (a *App) agentReviewNeedsCmd() error {
	if a.Config.Review.Type == "agent" && a.Config.Review.Cmd == "" {
		return fmt.Errorf("review.type is agent but review.cmd is empty: set review.cmd so harness can launch the reviewer (e.g. 'claude -p ... < {{prompt_file}}')")
	}
	return nil
}

// runReviewer writes the prompt to a temp file, substitutes {{prompt_file}}
// and {{task_id}} in review.cmd, and executes it from the repo root with the
// request's token in its environment. Blocks until the command exits.
func (a *App) runReviewer(subjectID, prompt, token string) error {
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
	cmdStr = strings.ReplaceAll(cmdStr, "{{task_id}}", subjectID)
	fmt.Printf("Running reviewer: %s\n", cmdStr)

	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = a.RepoRoot
	cmd.Env = append(os.Environ(), ReviewTokenEnv+"="+token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// deliverReview launches the configured reviewer, or prints how a human records
// a verdict. The token is never printed.
func (a *App) deliverReview(subjectID, title, diff, prevComments, token string) error {
	prompt := a.composeReviewPrompt(subjectID, title, diff, prevComments)
	if a.Config.Review.Type == "agent" {
		fmt.Println("Launching automated reviewer...")
		if err := a.runReviewer(subjectID, prompt, token); err != nil {
			fmt.Printf("Reviewer command failed: %s\n", err)
		}
		return nil
	}

	contextFile, err := os.CreateTemp("", "harness-review-context-*.txt")
	if err == nil {
		contextFile.WriteString(prompt)
		contextFile.Close()
		fmt.Printf("Review context written to: %s\n", contextFile.Name())
	} else {
		fmt.Printf("Diff:\n%s\n\n", diff)
	}
	fmt.Println("For human review, in an interactive terminal run:")
	fmt.Printf("  harness diff %s\n", subjectID)
	fmt.Printf("  harness review approve %s\n", subjectID)
	fmt.Printf("  harness review reject --comment \"...\" %s\n", subjectID)
	return nil
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
	if err := a.agentReviewNeedsCmd(); err != nil {
		return err
	}

	head, err := a.Git.RevParse(task.Branch)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", task.Branch, err)
	}
	runs, err := a.DB.GetLatestCheckRuns(id)
	if err != nil {
		return err
	}
	if len(runs) > 0 && runs[0].CommitSHA != "" && runs[0].CommitSHA != head {
		return fmt.Errorf("%s moved since checks ran (%s -> %s); run: harness work check",
			task.Branch, shortSHA(runs[0].CommitSHA), shortSHA(head))
	}
	token, err := a.DB.OpenReviewRequest(id, "", head)
	if err != nil {
		return err
	}
	if err := a.DB.UpdateTaskState(id, StateReviewRequested); err != nil {
		return err
	}

	diff, _ := a.Git.Diff(task.BaseBranch, task.Branch)
	reviews, _ := a.DB.GetReviews(id)
	prevComments := FormatPreviousComments(reviews)

	fmt.Printf("Task %s is now REVIEW_REQUESTED (%s).\n\n", id, shortSHA(head))
	if err := a.deliverReview(id, task.Title, diff, prevComments, token); err != nil {
		return err
	}
	if a.Config.Review.Type != "agent" {
		return nil
	}

	task, err = a.DB.GetTask(id)
	if err != nil {
		return err
	}
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
		fmt.Println("Next: run harness work check, then harness review request again,")
		fmt.Printf("or record a verdict yourself in an interactive terminal: harness review approve %s\n", id)
	}
	return nil
}
