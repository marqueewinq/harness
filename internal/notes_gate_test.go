package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func state(t *testing.T, app *App, id string) TaskState {
	t.Helper()
	task, err := app.DB.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task.State
}

func TestApproveRefusedWhenNotesChangedDuringReview(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	editNotes(t, app, task.ID, "also handle the empty case")

	err := app.CmdReviewApprove(task.ID, "")
	if err == nil || !strings.Contains(err.Error(), "notes changed during review (edited ") ||
		!strings.Contains(err.Error(), "run harness work check task-001, then harness review request task-001") ||
		!strings.Contains(err.Error(), "also handle the empty case") {
		t.Fatalf("approve: err = %v, want notes-changed refusal", err)
	}
	if got := state(t, app, task.ID); got != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", got)
	}
	if req, _ := app.DB.GetOpenReviewRequest(task.ID, ""); req == nil {
		t.Error("request closed by a refused approve")
	}
	if _, err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", ""); err == nil {
		t.Error("dashboard approve accepted despite changed notes")
	}

	if err := app.CmdReviewReject(task.ID, "notes changed"); err != nil {
		t.Fatal("reject:", err)
	}
	if got := state(t, app, task.ID); got != StateRequestChanges {
		t.Fatalf("state = %s, want REQUEST_CHANGES", got)
	}

	// A new request snapshots the current notes; approving it works.
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal(err)
	}
	req, _ := app.DB.GetOpenReviewRequest(task.ID, "")
	if !req.NotesSnapshot.Valid || req.NotesSnapshot.String != "also handle the empty case" {
		t.Errorf("snapshot = %+v", req.NotesSnapshot)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve after re-request:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
}

func TestAgentApproveRefusedWhenNotesChangedDuringReview(t *testing.T) {
	t.Parallel()
	bin := buildHarnessBinary(t)
	gate := filepath.Join(t.TempDir(), "go")
	app, _ := setupAppWithReviewCmd(t, fmt.Sprintf(
		`while [ ! -f %s ]; do sleep 0.05; done; %s review approve {{task_id}} < /dev/null`, gate, bin))
	defer app.Close()
	if err := app.CmdNew("agent reviewed", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- app.CmdReviewRequest(task.ID) }()
	for i := 0; ; i++ {
		if req, _ := app.DB.GetOpenReviewRequest(task.ID, ""); req != nil {
			break
		}
		if i > 200 {
			t.Fatal("request never opened")
		}
		time.Sleep(25 * time.Millisecond)
	}
	cur := SubjectText{task.Title, task.Description, ""}
	if _, err := app.RecordTerminalEdit(task.ID, cur, SubjectText{task.Title, task.Description, "late note"}, "dashboard"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(gate, nil, 0644)

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("request: want no-verdict error, the approve must be refused")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("review request did not finish")
	}
	if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 0 {
		t.Errorf("reviews = %+v, want none", reviews)
	}
	if got := state(t, app, task.ID); got == StateApproved {
		t.Error("task approved on a prompt without the notes")
	}
}

func TestMergeRefusedWhenNotesChangedAfterApproval(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	editNotes(t, app, task.ID, "rename the flag first")
	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "new notes from the human on task-001 since approval") ||
		!strings.Contains(err.Error(), "rename the flag first") {
		t.Fatalf("merge: err = %v, want notes refusal", err)
	}
	if got := state(t, app, task.ID); got != StateApproved {
		t.Errorf("state = %s, want APPROVED (nothing queued)", got)
	}
}

func TestNoneMergeRefusedWhenNotesChangedAfterChecks(t *testing.T) {
	t.Parallel()
	app, _, task := checkedNoneTask(t)
	editNotes(t, app, task.ID, "check the docs too")
	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "since the last check run") || !strings.Contains(err.Error(), "check the docs too") {
		t.Fatalf("merge: err = %v, want notes refusal", err)
	}
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge after re-check:", err)
	}
}

func TestPreApproveAllowedWhenNotesChangedDuringReview(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	editNotes(t, app, task.ID, "human context")
	if _, err := app.RecordTerminalStop(task.ID, StopApprove, "dashboard", ""); err != nil {
		t.Fatal("pre-approve:", err)
	}
	approval, _ := app.DB.GetLatestApproval(task.ID)
	if approval == nil || approval.NotesSnapshot.String != "human context" {
		t.Fatalf("approval = %+v, want it to cover the current notes", approval)
	}
	if err := app.DB.SetReviewType(task.ID, ReviewNone, "terminal:test (dashboard)"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
}

func TestEpicNotesGate(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, ""); err != nil {
		t.Fatal(err)
	}
	e, _ := app.DB.GetEpic("epic-001")
	gitRun(t, dir, "checkout", e.Branch)
	gitCommitFile(t, dir, "e.txt", "e\n", "epic work")
	gitRun(t, dir, "checkout", "main")

	if err := app.CmdReviewRequestDispatch(e.ID); err != nil {
		t.Fatal(err)
	}
	editNotes(t, app, e.ID, "epic constraint")
	err := app.CmdReviewApproveDispatch(e.ID, "")
	if err == nil || !strings.Contains(err.Error(), "run harness review request epic-001") {
		t.Fatalf("epic approve: err = %v, want notes refusal", err)
	}
	if err := app.CmdReviewRequestDispatch(e.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewApproveDispatch(e.ID, ""); err != nil {
		t.Fatal("epic approve after re-request:", err)
	}
	editNotes(t, app, e.ID, "epic constraint, revised")
	err = app.CmdEpicMerge(e.ID)
	if err == nil || !strings.Contains(err.Error(), "since approval") || !strings.Contains(err.Error(), "epic constraint, revised") {
		t.Fatalf("epic merge: err = %v, want notes refusal", err)
	}
}
