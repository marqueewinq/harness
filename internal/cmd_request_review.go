package internal

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Single pass, so placeholders that occur inside the diff are left alone.
func composeReviewPrompt(review ReviewConfig, subjectID, title, diff, prevComments string) string {
	return strings.NewReplacer(
		"{{diff}}", diff,
		"{{previous_comments}}", prevComments,
		"{{task_description}}", title,
		"{{task_id}}", subjectID,
	).Replace(review.Prompt)
}

// trustedReviewConfig reads the review section from the config committed on
// the main branch, not the working tree: review.cmd receives the approval
// token, so an implementer must not be able to redirect it with a local edit.
func (a *App) trustedReviewConfig() (ReviewConfig, error) {
	data, err := a.Git.run("show", a.Config.MainBranch+":.harness.yaml")
	if err != nil {
		return ReviewConfig{}, fmt.Errorf("reading committed .harness.yaml from %s: %w", a.Config.MainBranch, err)
	}
	cfg, err := ParseConfig([]byte(data))
	if err != nil {
		return ReviewConfig{}, fmt.Errorf("committed .harness.yaml on %s: %w", a.Config.MainBranch, err)
	}
	if cfg.Review != a.Config.Review {
		fmt.Printf("Note: using review config committed on %s; local edits to review: are ignored.\n", a.Config.MainBranch)
	}
	if cfg.Review.Type == "agent" && cfg.Review.Cmd == "" {
		return ReviewConfig{}, fmt.Errorf("review.type is agent but review.cmd is empty on %s: harness hands the approval token only to a reviewer it launches", a.Config.MainBranch)
	}
	return cfg.Review, nil
}

// runReviewer blocks until review.cmd exits. The token goes only into the
// reviewer's environment.
func (a *App) runReviewer(review ReviewConfig, subjectID, prompt, token string) error {
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

	cmdStr := review.Cmd
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

// The token is never printed.
func (a *App) deliverReview(review ReviewConfig, subjectID, title, diff, prevComments, token string) error {
	prompt := composeReviewPrompt(review, subjectID, title, diff, prevComments)
	if review.Type == "agent" {
		fmt.Println("Launching automated reviewer...")
		if err := a.runReviewer(review, subjectID, prompt, token); err != nil {
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
	fmt.Printf("  harness work diff %s\n", subjectID)
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
	review, err := a.trustedReviewConfig()
	if err != nil {
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
	if err := a.deliverReview(review, id, task.Title, diff, prevComments, token); err != nil {
		return err
	}
	if review.Type != "agent" {
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
		fmt.Println("Next: address comments, then run: harness work check")
	default:
		fmt.Printf("\nWarning: reviewer exited without recording a verdict.\n")
		fmt.Printf("Task %s is still %s.\n", id, task.State)
		fmt.Println("Next: run harness work check, then harness review request again,")
		fmt.Printf("or record a verdict yourself in an interactive terminal: harness review approve %s\n", id)
	}
	return nil
}
