package internal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
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
// The ref is fully qualified because a local tag named like the main branch
// would otherwise win ref resolution.
func (a *App) trustedReviewConfig() (ReviewConfig, error) {
	review, err := a.CommittedReviewConfig()
	if err != nil {
		return ReviewConfig{}, err
	}
	if review != a.Config.Review {
		fmt.Printf("Note: using review config committed on %s; local edits to review: are ignored.\n", a.Config.MainBranch)
	}
	if review.Type == "agent" && review.Cmd == "" {
		return ReviewConfig{}, fmt.Errorf("review.type is agent but review.cmd is empty on %s: harness hands the approval token only to a reviewer it launches", a.Config.MainBranch)
	}
	return review, nil
}

// CommittedReviewConfig is the review section committed on the main branch,
// which decides how `harness review request` delivers a review.
func (a *App) CommittedReviewConfig() (ReviewConfig, error) {
	data, err := a.Git.ShowCommitted("refs/heads/"+a.Config.MainBranch, ".harness.yaml")
	if err != nil {
		return ReviewConfig{}, fmt.Errorf("reading committed .harness.yaml from %s: %w", a.Config.MainBranch, err)
	}
	cfg, err := ParseConfig([]byte(data))
	if err != nil {
		return ReviewConfig{}, fmt.Errorf("committed .harness.yaml on %s: %w", a.Config.MainBranch, err)
	}
	return cfg.Review, nil
}

// runReviewer blocks until review.cmd exits, times out, or harness is
// interrupted, records the run on request reqID, and returns the stored
// outcome. The token goes only into the reviewer's environment.
func (a *App) runReviewer(review ReviewConfig, reqID int, subjectID, prompt, token string) (string, error) {
	f, err := os.CreateTemp("", "harness-review-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	promptFile := f.Name()
	defer os.Remove(promptFile)

	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		return "", fmt.Errorf("write prompt: %w", err)
	}
	f.Close()

	cmdStr := review.Cmd
	cmdStr = strings.ReplaceAll(cmdStr, "{{prompt_file}}", promptFile)
	cmdStr = strings.ReplaceAll(cmdStr, "{{task_id}}", subjectID)
	fmt.Printf("Running reviewer: %s\n", cmdStr)

	// The reviewer runs in its own process group, so terminal Ctrl-C or hangup
	// no longer reaches it; forward them by cancelling.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	timeout := review.TimeoutDuration()
	ctx, cancel := context.WithTimeout(sigCtx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = a.RepoRoot
	cmd.Env = append(os.Environ(), ReviewTokenEnv+"="+token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Stdin stays nil (/dev/null): in its own process group the reviewer would
	// be stopped by SIGTTIN reading a terminal. The prompt is {{prompt_file}}.
	cmd.Stdin = nil
	// Agents spawn helpers that may hold the token; kill the whole group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "harness: starting reviewer: %v\n", err)
		return a.DB.FinishReviewerRun(reqID, ReviewerFailed, -1)
	}
	ownerPID := os.Getpid()
	if err := a.DB.StartReviewerRun(reqID, ownerPID, cmd.Process.Pid); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: recording reviewer run failed: %v\n", err)
	}
	stopHeartbeat := a.startReviewerHeartbeat(reqID, ownerPID)
	err = cmd.Wait()
	stopHeartbeat()

	outcome, exitCode := ReviewerNoVerdict, 0
	switch {
	case sigCtx.Err() != nil:
		outcome, exitCode = ReviewerInterrupted, InterruptedExitCode
		fmt.Fprintln(os.Stderr, "\nharness: reviewer interrupted")
	case ctx.Err() == context.DeadlineExceeded:
		outcome, exitCode = ReviewerTimeout, TimeoutExitCode
		fmt.Fprintf(os.Stderr, "\nharness: reviewer timed out after %s (review.timeout)\n", timeout)
	case errors.Is(err, exec.ErrWaitDelay):
		// The reviewer itself exited 0; a leftover child held its output.
	case err != nil:
		outcome, exitCode = ReviewerFailed, -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "\nharness: reviewer failed: %v\n", err)
	}
	if outcome != ReviewerInterrupted && outcome != ReviewerTimeout {
		// Background children may outlive a reviewer that exited; they must
		// not keep running with the token. Safe after Wait: the group id can't
		// be reused while any member lives, and with none left this is ESRCH.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	stored, ferr := a.DB.FinishReviewerRun(reqID, outcome, exitCode)
	if ferr != nil {
		return "", fmt.Errorf("recording reviewer outcome: %w", ferr)
	}
	if stored == ReviewerVerdict && exitCode != 0 {
		fmt.Printf("Warning: reviewer recorded a verdict but then exited with %s (exit %d); keeping the verdict.\n", outcome, exitCode)
	}
	return stored, nil
}

func (a *App) startReviewerHeartbeat(reqID, ownerPID int) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(reviewerHeartbeatEvery)
		defer t.Stop()
		warned := false
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := a.DB.HeartbeatReviewerRun(reqID, ownerPID); err != nil && !warned {
					fmt.Fprintf(os.Stderr, "Warning: renewing reviewer heartbeat failed: %v\n", err)
					warned = true
				}
			}
		}
	}()
	return func() { close(done) }
}

// deliverReview launches the reviewer (agent) or prints instructions (human).
// An agent run that ends without a verdict is an error naming retryHint. The
// token is never printed.
func (a *App) deliverReview(review ReviewConfig, reqID int, subjectID, title, diff, prevComments, token, retryHint string) error {
	prompt := composeReviewPrompt(review, subjectID, title, diff, prevComments)
	if review.Type == "agent" {
		fmt.Println("Launching automated reviewer...")
		outcome, err := a.runReviewer(review, reqID, subjectID, prompt, token)
		if err != nil {
			return err
		}
		if outcome != ReviewerVerdict {
			return fmt.Errorf("reviewer exited without a verdict (outcome: %s); the review request is closed and its token revoked; %s", outcome, retryHint)
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
	if err := requireIdleWorktree(task); err != nil {
		return err
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
	token, reqID, err := a.DB.OpenReviewRequestID(id, "", head)
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
	retry := fmt.Sprintf("task %s stays REVIEW_REQUESTED; run: harness work check %s, then harness review request %s", id, id, id)
	if err := a.deliverReview(review, reqID, id, task.Title, diff, prevComments, token, retry); err != nil {
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
		return fmt.Errorf("reviewer recorded a verdict but task %s is %s; see harness review history %s", id, task.State, id)
	}
	return nil
}
