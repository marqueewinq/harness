package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requestWithReviewer runs a full check + review request whose review.cmd is
// reviewCmd (with %s replaced by a freshly built harness binary) and returns
// the review request's error.
func requestWithReviewer(t *testing.T, reviewCmd string) (*App, *Task, error) {
	t.Helper()
	return requestWithReviewerConfig(t, reviewCmd, "")
}

func requestWithReviewerConfig(t *testing.T, reviewCmd, extraReview string) (*App, *Task, error) {
	t.Helper()
	bin := buildHarnessBinary(t)
	cmd := reviewCmd
	if strings.Contains(reviewCmd, "%s") {
		cmd = fmt.Sprintf(reviewCmd, bin)
	}
	app, _ := setupAppWithReviewConfig(t, cmd, extraReview)
	t.Cleanup(func() { app.Close() })

	if err := app.CmdNew("token feature", "", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	os.WriteFile(filepath.Join(task.WorktreePath, "f.txt"), []byte("f\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "f.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	reqErr := app.CmdReviewRequest(task.ID)
	task, _ = app.DB.GetTask(task.ID)
	return app, task, reqErr
}

func TestReviewerWithTokenApproves(t *testing.T) {
	t.Parallel()
	app, task, err := requestWithReviewer(t, `%s review approve {{task_id}} < /dev/null`)
	if err != nil {
		t.Fatal("request:", err)
	}
	if task.State != StateApproved {
		t.Fatalf("state = %s, want APPROVED", task.State)
	}
	reviews, _ := app.DB.GetReviews(task.ID)
	if len(reviews) != 1 || reviews[0].Reviewer != "review.cmd" {
		t.Errorf("reviews = %+v, want one by review.cmd", reviews)
	}
	run := latestRun(t, app, task.ID)
	if run.ReviewerOutcome != ReviewerVerdict || run.ReviewerExitCode != 0 {
		t.Errorf("run = %+v, want outcome verdict, exit 0", run)
	}
}

func TestApproveWithoutTokenRefused(t *testing.T) {
	t.Parallel()
	// Simulates the implementing agent calling approve: no token, no terminal.
	app, task, err := requestWithReviewer(t, `env -u `+ReviewTokenEnv+` %s review approve {{task_id}} < /dev/null`)
	if err == nil {
		t.Fatal("request: want error, reviewer recorded no verdict")
	}
	if task.State != StateReviewRequested {
		t.Fatalf("state = %s, want REVIEW_REQUESTED", task.State)
	}
	if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 0 {
		t.Errorf("reviews = %+v, want none", reviews)
	}
}

func TestRejectWithWrongTokenRefused(t *testing.T) {
	t.Parallel()
	app, task, err := requestWithReviewer(t, ReviewTokenEnv+`=forged %s review reject --comment no {{task_id}} < /dev/null`)
	if err == nil {
		t.Fatal("request: want error, reviewer recorded no verdict")
	}
	if task.State != StateReviewRequested {
		t.Fatalf("state = %s, want REVIEW_REQUESTED", task.State)
	}
	if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 0 {
		t.Errorf("reviews = %+v, want none", reviews)
	}
}

func TestLocalReviewCmdEditIgnored(t *testing.T) {
	t.Parallel()
	bin := buildHarnessBinary(t)
	app, dir := setupAppWithReviewCmd(t, "echo noop")
	app.Close()

	// The implementer rewrites the uncommitted config to self-approve.
	cfgPath := filepath.Join(dir, ".harness.yaml")
	data, _ := os.ReadFile(cfgPath)
	forged := strings.Replace(string(data), "'echo noop'", fmt.Sprintf("'%s review approve {{task_id}} < /dev/null'", bin), 1)
	if forged == string(data) {
		t.Fatal("test setup: review.cmd not found in config")
	}
	os.WriteFile(cfgPath, []byte(forged), 0644)

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.CmdNew("forged", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequest(task.ID); err == nil {
		t.Fatal("request: want error, reviewer recorded no verdict")
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED (forged local review.cmd must not run)", task.State)
	}
}

func TestComposeReviewPromptSinglePass(t *testing.T) {
	t.Parallel()
	got := composeReviewPrompt(ReviewConfig{Prompt: "{{task_id}}: {{diff}}"}, "task-7", "t", "+ {{task_id}} {{previous_comments}}", "prev")
	if got != "task-7: + {{task_id}} {{previous_comments}}" {
		t.Errorf("prompt = %q; placeholders inside the diff must survive", got)
	}
}
