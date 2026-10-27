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
	if _, err := app.RecordTerminalVerdict(task.ID, "approve", "lgtm", "dashboard", ""); err != nil {
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

func TestRecordTerminalVerdictReturnsReportWithoutPrinting(t *testing.T) {
	app, task := requestedTask(t)
	var report string
	out, err := captureStdout(t, func() error {
		var err error
		report, err = app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", "")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("printed %q; the dashboard owns the terminal", out)
	}
	if !strings.Contains(report, "Task "+task.ID+" is now APPROVED.") {
		t.Errorf("report = %q", report)
	}
}

func TestCLIVerdictOutputUnchanged(t *testing.T) {
	app, task := requestedTask(t)
	out, err := captureStdout(t, func() error {
		return app.taskVerdict(task.ID, "request_changes", "fix it", terminalAuth("test"))
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Review recorded: REQUEST_CHANGES\nComment: fix it\nTask " + task.ID + " is now REQUEST_CHANGES.\nNext: address the comments, then run: harness work check\n"
	if out != want {
		t.Errorf("CLI output = %q, want %q", out, want)
	}
}

func TestRecordTerminalVerdictRefusesSupersededRequest(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	shown, _ := app.DB.GetOpenReviewRequest(task.ID, "")
	// Between the human opening the diff and confirming, the branch moves and
	// review is requested again at the new commit.
	gitCommitFile(t, task.WorktreePath, "late.txt", "x\n", "commit after request")
	head, err := NewGitClient(task.WorktreePath).RevParse("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.OpenReviewRequest(task.ID, "", head); err != nil {
		t.Fatal(err)
	}

	for _, decision := range []string{"approve", "request_changes"} {
		_, err := app.RecordTerminalVerdict(task.ID, decision, "seen", "dashboard", shown.CommitSHA)
		want := "changed since you opened it: was " + shortSHA(shown.CommitSHA) + ", now " + shortSHA(head)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", decision, err, want)
		}
	}
	req, _ := app.DB.GetOpenReviewRequest(task.ID, "")
	if req == nil || req.CommitSHA != head {
		t.Errorf("new request not left open: %+v", req)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateReviewRequested {
		t.Errorf("state = %s", got.State)
	}
	if _, err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", head); err != nil {
		t.Errorf("verdict at the current request SHA: %v", err)
	}
}

func TestRecordTerminalVerdictRejectNeedsComment(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	if _, err := app.RecordTerminalVerdict(task.ID, "request_changes", "", "dashboard", ""); err == nil {
		t.Fatal("reject without comment should fail")
	}
	if _, err := app.RecordTerminalVerdict(task.ID, "request_changes", "fix it", "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateRequestChanges {
		t.Errorf("state = %s, want REQUEST_CHANGES", task.State)
	}
}

func TestRecordTerminalVerdictRejectsUnknownDecision(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	_, err := app.RecordTerminalVerdict(task.ID, "reject", "", "dashboard", "")
	if err == nil || !strings.Contains(err.Error(), "unknown review decision") {
		t.Fatalf("err = %v, want unknown review decision", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", task.State)
	}
	if req, _ := app.DB.GetOpenReviewRequest(task.ID, ""); req == nil {
		t.Error("request must stay open")
	}
}

func TestRecordTerminalVerdictKeepsCLIChecks(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	gitCommitFile(t, task.WorktreePath, "late.txt", "x\n", "commit after request")
	_, err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", "")
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
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalVerdict("epic-001", "request_changes", "not yet", "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalVerdict("epic-001", "approve", "", "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	reviews, _ := app.DB.GetEpicReviews("epic-001")
	if len(reviews) != 2 || reviews[0].Decision != "request_changes" || reviews[1].Decision != "approve" {
		t.Errorf("epic reviews = %+v", reviews)
	}
}

// Not parallel: swaps package-level hooks and the process environment.
func TestRecordTerminalVerdictRefusals(t *testing.T) {
	app, task := requestedTask(t)

	t.Run("token env set", func(t *testing.T) {
		t.Setenv(ReviewTokenEnv, "anything")
		_, err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", "")
		if err == nil || !strings.Contains(err.Error(), "reviewer's environment") {
			t.Errorf("err = %v", err)
		}
	})

	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()
	_, err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", "")
	if err == nil || !strings.Contains(err.Error(), "not an interactive terminal") {
		t.Errorf("without tty: err = %v", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED after refusals", task.State)
	}
}
