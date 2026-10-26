package internal

import (
	"strings"
	"testing"
)

func TestApproveRefusesCommitAfterRequest(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request:", err)
	}
	reviewed, _ := app.Git.RevParse(task.Branch)

	gitCommitFile(t, task.WorktreePath, "sneaky.txt", "x\n", "commit during review")

	err := app.CmdReviewApprove(task.ID, "")
	if err == nil || !strings.Contains(err.Error(), "moved since review was requested") {
		t.Fatalf("approve after new commit: err = %v, want 'moved since review was requested'", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", task.State)
	}

	if err := app.CmdReviewReject(task.ID, "saw old diff"); err != nil {
		t.Fatal("reject:", err)
	}
	reviews, _ := app.DB.GetReviews(task.ID)
	if len(reviews) != 1 || reviews[0].CommitSHA != reviewed {
		t.Fatalf("reviews = %+v, want one review pinned to %s", reviews, reviewed)
	}
}

func TestRequestRefusesCommitAfterChecks(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	gitCommitFile(t, task.WorktreePath, "unchecked.txt", "x\n", "commit after checks")

	err := app.CmdReviewRequest(task.ID)
	if err == nil || !strings.Contains(err.Error(), "moved since checks ran") {
		t.Fatalf("request after new commit: err = %v, want 'moved since checks ran'", err)
	}

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("re-check from CHECKS_PASSED:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request after re-check:", err)
	}
}

func TestApproveIsSingleUsePerRequest(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()
	task, err := db.CreateTask("t", "", "b", "/tmp/x", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.OpenReviewRequest(task.ID, "", "abc"); err != nil {
		t.Fatal(err)
	}
	req, _ := db.GetOpenReviewRequest(task.ID, "")
	if err := db.ResolveReviewRequest(req, "approve", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.ResolveReviewRequest(req, "approve", ""); err == nil {
		t.Error("second verdict on same request should fail")
	}
	if open, _ := db.GetOpenReviewRequest(task.ID, ""); open != nil {
		t.Errorf("request should be resolved, got %+v", open)
	}
}

func TestEpicApproveRequiresRequest(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false); err != nil {
		t.Fatal(err)
	}
	err := app.cmdEpicReviewApprove("epic-001", "")
	if err == nil || !strings.Contains(err.Error(), "no open review request") {
		t.Fatalf("epic approve without request: err = %v", err)
	}
	if err := app.cmdEpicReviewRequest("epic-001"); err != nil {
		t.Fatal("epic request:", err)
	}
	if err := app.cmdEpicReviewApprove("epic-001", ""); err != nil {
		t.Fatal("epic approve:", err)
	}
	if err := app.cmdEpicReviewApprove("epic-001", ""); err == nil {
		t.Error("second epic approve on same request should fail")
	}
}
