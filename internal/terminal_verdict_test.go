package internal

import (
	"strings"
	"testing"
)

func requestedTask(t *testing.T) (*App, *Task) {
	t.Helper()
	app, _, task := setupTask(t)
	t.Cleanup(func() { app.Close() })
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal(err)
	}
	return app, task
}

func TestRecordTerminalVerdictApproves(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	if err := app.RecordTerminalVerdict(task.ID, "approve", "lgtm", "dashboard"); err != nil {
		t.Fatal(err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state = %s, want APPROVED", task.State)
	}
	reviews, _ := app.DB.GetReviews(task.ID)
	if len(reviews) != 1 || !strings.HasSuffix(reviews[0].Reviewer, "(dashboard)") || !strings.HasPrefix(reviews[0].Reviewer, "terminal:") {
		t.Errorf("reviews = %+v, want one by terminal:<user> (dashboard)", reviews)
	}
}

func TestRecordTerminalVerdictRejectNeedsComment(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	if err := app.RecordTerminalVerdict(task.ID, "request_changes", "", "dashboard"); err == nil {
		t.Fatal("reject without comment should fail")
	}
	if err := app.RecordTerminalVerdict(task.ID, "request_changes", "fix it", "dashboard"); err != nil {
		t.Fatal(err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateRequestChanges {
		t.Errorf("state = %s, want REQUEST_CHANGES", task.State)
	}
}

func TestRecordTerminalVerdictKeepsCLIChecks(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	gitCommitFile(t, task.WorktreePath, "late.txt", "x\n", "commit after request")
	err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard")
	if err == nil || !strings.Contains(err.Error(), "moved since review was requested") {
		t.Errorf("err = %v, want moved since review was requested", err)
	}
}

func TestRecordTerminalVerdictEpic(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	if err := app.RecordTerminalVerdict("epic-001", "approve", "", "dashboard"); err != nil {
		t.Fatal(err)
	}
	if a, _ := app.DB.GetLatestEpicApproval("epic-001"); a == nil {
		t.Error("epic approval not recorded")
	}
}

// Not parallel: swaps package-level hooks and the process environment.
func TestRecordTerminalVerdictRefusals(t *testing.T) {
	app, task := requestedTask(t)

	t.Setenv(ReviewTokenEnv, "anything")
	err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard")
	if err == nil || !strings.Contains(err.Error(), "reviewer's environment") {
		t.Errorf("with token env: err = %v", err)
	}
	t.Setenv(ReviewTokenEnv, "")

	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()
	err = app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard")
	if err == nil || !strings.Contains(err.Error(), "not an interactive terminal") {
		t.Errorf("without tty: err = %v", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED after refusals", task.State)
	}
}
