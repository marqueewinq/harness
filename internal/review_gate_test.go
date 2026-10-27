package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// checkedNoneTask returns an app with allow_none committed and a review-type
// none task whose head has passed checks.
func checkedNoneTask(t *testing.T) (*App, string, *Task) {
	t.Helper()
	app, dir := setupApp(t)
	t.Cleanup(app.Close)
	commitAllowNone(t, dir)
	if err := app.CmdNew("no review", "", "", "", "none"); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Fatalf("state = %s, want CHECKS_PASSED", task.State)
	}
	return app, dir, task
}

func TestNoneTaskMergesFromChecksPassed(t *testing.T) {
	t.Parallel()
	app, dir, task := checkedNoneTask(t)
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	got, _ := app.DB.GetTask(task.ID)
	if got.State != StateMerged {
		t.Fatalf("state = %s, want MERGED", got.State)
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); err != nil {
		t.Errorf("f.txt not merged into main: %v", err)
	}
	transitions, _ := app.DB.GetTaskTransitions(task.ID)
	found := false
	for _, tr := range transitions {
		if tr.FromState == string(StateChecksPassed) && tr.ToState == string(StateQueued) {
			found = strings.Contains(tr.Note, "merged without review (review: none · self-declared (allow_none) · ")
		}
	}
	if !found {
		t.Errorf("no CHECKS_PASSED -> QUEUED transition with the no-review note: %+v", transitions)
	}
}

func TestNoneTaskMergeRefusedWhenHeadMoved(t *testing.T) {
	t.Parallel()
	app, _, task := checkedNoneTask(t)
	gitCommitFile(t, task.WorktreePath, "g.txt", "g\n", "add g")
	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "re-run checks") {
		t.Fatalf("merge after head moved: err = %v, want re-run checks", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateChecksPassed {
		t.Errorf("state = %s, want CHECKS_PASSED", got.State)
	}
}

func recordRunAtHead(t *testing.T, app *App, task *Task, check string, exit int) {
	t.Helper()
	head, err := app.Git.RevParse(task.Branch)
	if err != nil {
		t.Fatal(err)
	}
	group, _ := app.DB.NextRunGroup(task.ID)
	now := time.Now()
	if err := app.DB.RecordCheckRun(task.ID, check, exit, "", "", group, now, now, head); err != nil {
		t.Fatal(err)
	}
}

func TestNoneTaskMergeRefusedWhenLatestRunOnHeadFailed(t *testing.T) {
	t.Parallel()
	app, _, task := checkedNoneTask(t)
	// An older passing run on the same head must not count.
	recordRunAtHead(t, app, task, "test", 1)
	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "did not pass") {
		t.Fatalf("merge: err = %v, want latest run failure", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateChecksPassed {
		t.Errorf("state = %s, want CHECKS_PASSED", got.State)
	}
}

func TestNoneTaskMergeRequiresRequiredChecksOnHead(t *testing.T) {
	t.Parallel()
	app, _, task := checkedNoneTask(t)
	// merge.require.checks is [test]; the latest run on head lacks it.
	recordRunAtHead(t, app, task, "other", 0)
	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "required checks not passed") {
		t.Fatalf("merge: err = %v, want required checks refusal", err)
	}
}

func TestReviewedTypesNeedApprovalToMerge(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	for i, rt := range []string{"default", "human"} {
		if err := app.CmdNew("feature "+rt, "", "", "", rt); err != nil {
			t.Fatal(err)
		}
		id := []string{"task-001", "task-002"}[i]
		task, _ := app.DB.GetTask(id)
		gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
		if err := app.CmdWorkCheck(id); err != nil {
			t.Fatal(err)
		}
		err := app.CmdWorkMerge(id)
		if err == nil || !strings.Contains(err.Error(), "need APPROVED") {
			t.Errorf("%s merge from CHECKS_PASSED: err = %v, want need APPROVED", rt, err)
		}
		if err := app.DB.QueueWithoutReview(id, "x"); err == nil {
			t.Errorf("%s task queued without review", rt)
		}
		if got, _ := app.DB.GetTask(id); got.State != StateChecksPassed {
			t.Errorf("%s state = %s, want CHECKS_PASSED", rt, got.State)
		}
	}
}

func TestReviewRequestOnNoneRefused(t *testing.T) {
	t.Parallel()
	app, _, task := checkedNoneTask(t)
	err := app.CmdReviewRequest(task.ID)
	if err == nil || !strings.Contains(err.Error(), "harness work merge") {
		t.Errorf("task review request: err = %v, want refusal pointing to work merge", err)
	}
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, "none"); err != nil {
		t.Fatal(err)
	}
	err = app.CmdReviewRequestDispatch("epic-001")
	if err == nil || !strings.Contains(err.Error(), "harness epic merge") {
		t.Errorf("epic review request: err = %v, want refusal pointing to epic merge", err)
	}
}

func TestNoneEpicMergesWithoutApproval(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()
	commitAllowNone(t, dir)
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("Quick", "", "proj-001", "", false, "none"); err != nil {
		t.Fatal(err)
	}
	e, _ := app.DB.GetEpic("epic-001")
	gitRun(t, dir, "checkout", e.Branch)
	gitCommitFile(t, dir, "e.txt", "e\n", "epic work")
	gitRun(t, dir, "checkout", "main")

	if err := app.CmdEpicMerge("epic-001"); err != nil {
		t.Fatal("epic merge:", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "e.txt")); err != nil {
		t.Errorf("e.txt not merged into main: %v", err)
	}
	e, _ = app.DB.GetEpic("epic-001")
	if e.Status != EpicStatusClosed {
		t.Errorf("epic status = %s, want closed", e.Status)
	}
	transitions, _ := app.DB.GetEpicTransitions("epic-001")
	last := transitions[len(transitions)-1]
	if !strings.Contains(last.Note, "merged without review (review: none · self-declared (allow_none)") {
		t.Errorf("last epic transition note = %q", last.Note)
	}
}

func TestHumanReviewRequestLaunchesNoReviewer(t *testing.T) {
	t.Parallel()
	bin := buildHarnessBinary(t)
	marker := filepath.Join(t.TempDir(), "reviewer-ran")
	app, dir := setupAppWithReviewCmd(t, "touch "+marker)
	defer app.Close()

	if err := app.CmdNew("human feature", "", "", "", "human"); err != nil {
		t.Fatal(err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request:", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("review.cmd ran for a review-type human task")
	}
	req, _ := app.DB.GetOpenReviewRequest(task.ID, "")
	if req == nil || req.TokenHash != "" {
		t.Fatalf("open request = %+v, want one with no token", req)
	}

	// A reviewer agent's token verdict is refused.
	cmd := exec.Command(bin, "review", "approve", task.ID)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), ReviewTokenEnv+"=forged")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("token verdict accepted on a human request:\n%s", out)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateReviewRequested {
		t.Fatalf("state = %s, want REVIEW_REQUESTED", got.State)
	}

	// The human at the terminal decides.
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("terminal approve:", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateApproved {
		t.Fatalf("state = %s, want APPROVED", got.State)
	}
}

func TestHumanEpicReviewRequestLaunchesNoReviewer(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "reviewer-ran")
	app, _ := setupAppWithReviewCmd(t, "touch "+marker)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, "human"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal("request:", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("review.cmd ran for a review-type human epic")
	}
	req, _ := app.DB.GetOpenReviewRequest("", "epic-001")
	if req == nil || req.TokenHash != "" {
		t.Fatalf("open request = %+v, want one with no token", req)
	}
}

func TestWorkStatusNextHintPerReviewType(t *testing.T) {
	app, _, _ := checkedNoneTask(t)
	for _, rt := range []string{"default", "human"} {
		if err := app.CmdNew("feature "+rt, "", "", "", rt); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"task-002", "task-003"} {
		task, _ := app.DB.GetTask(id)
		gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
		if err := app.CmdWorkCheck(id); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string]string{
		"task-001": "Next: run harness work merge (review type none",
		"task-002": "Next: run harness review request\n",
		"task-003": "Next: run harness review request; then a human approves or rejects",
	} {
		out, err := captureStdout(t, func() error { return app.CmdWorkStatus(id) })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, want) {
			t.Errorf("%s status lacks %q:\n%s", id, want, out)
		}
	}

	if err := app.CmdReviewRequest("task-003"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return app.CmdWorkStatus("task-003") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Next: waiting for a human verdict (review type human)") {
		t.Errorf("human REVIEW_REQUESTED hint:\n%s", out)
	}
}
