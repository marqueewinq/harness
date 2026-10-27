package internal

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func assertMergeFailure(t *testing.T, app *App, taskID, want string) *MergeFailure {
	t.Helper()
	m, err := app.LastMergeFailure(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil || !strings.Contains(m.Reason, want) {
		t.Fatalf("merge failure of %s = %+v, want reason containing %q", taskID, m, want)
	}
	return m
}

func readMergeLog(t *testing.T, app *App, id string) string {
	t.Helper()
	data, err := os.ReadFile(MergeLogPath(app.RepoRoot, id))
	if err != nil {
		t.Fatalf("merge log: %v", err)
	}
	return string(data)
}

func TestMergeFailureRecordsRebaseConflict(t *testing.T) {
	app, dir, task := setupTask(t)
	defer app.Close()
	gitCommitFile(t, task.WorktreePath, "conflict.txt", "branch\n", "branch side")
	for _, step := range []func(string) error{app.CmdWorkCheck, app.CmdReviewRequest} {
		if err := step(task.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	gitCommitFile(t, dir, "conflict.txt", "main\n", "main side")

	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Fatal("want merge failure")
	}
	m := assertMergeFailure(t, app, task.ID, "prepare failed")
	if !strings.Contains(m.Reason, "conflict in conflict.txt") || strings.Contains(m.Reason, "\n") {
		t.Errorf("reason = %q, want one line naming conflict.txt", m.Reason)
	}
	if m.LogPath != MergeLogPath(app.RepoRoot, task.ID) {
		t.Errorf("log path = %q", m.LogPath)
	}
	if log := readMergeLog(t, app, task.ID); !strings.Contains(log, "CONFLICT") || !strings.Contains(log, "MERGE FAILED") {
		t.Errorf("merge log lacks git output:\n%s", log)
	}

	for name, cmd := range map[string]func() error{
		"work status": func() error { return app.CmdWorkStatus(task.ID) },
		"task view":   func() error { return app.CmdTaskView(task.ID) },
	} {
		out, err := captureStdout(t, cmd)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Merge failed: prepare failed") || !strings.Contains(out, "· log: "+m.LogPath) {
			t.Errorf("%s lacks the merge failure:\n%s", name, out)
		}
	}
}

func TestMergeFailureNamesChecksFailedAfterRebase(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()
	for _, step := range []func(string) error{app.CmdWorkCheck, app.CmdReviewRequest} {
		if err := step(task.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	// main now breaks the test target; the rebase brings it into the branch.
	gitCommitFile(t, dir, "Makefile", "test:\n\t@echo broken-after-rebase; exit 1\n", "break test")

	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Fatal("want merge failure")
	}
	assertMergeFailure(t, app, task.ID, "checks failed after rebase: test")
	if log := readMergeLog(t, app, task.ID); !strings.Contains(log, "check test: FAIL") || !strings.Contains(log, "broken-after-rebase") {
		t.Errorf("merge log lacks check output:\n%s", log)
	}
}

func TestMergeFailureRecordsQueueTimeout(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.Config.Merge.QueueTimeout = 1
	insertQueueRow(t, app, "task-999", os.Getppid(), time.Now())
	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Fatal("want queue timeout")
	}
	assertMergeFailure(t, app, task.ID, "merge queue timeout")
}

func TestMergeFailureRecordsReclaimedLease(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	ghost, err := app.DB.CreateTask("ghost", "", "ghost", "/tmp/ghost", "main", "", "", ReviewSetting{})
	if err != nil {
		t.Fatal(err)
	}
	forceState(t, app, ghost.ID, StateMerging)
	insertQueueRow(t, app, ghost.ID, deadPID(t), time.Now())
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal(err)
	}
	assertMergeFailure(t, app, ghost.ID, "stale merge queue lease reclaimed")
}

func TestMergeFailureRecordsRecoveredAbandonedMerge(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	forceState(t, app, task.ID, StateQueued)
	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Fatal("want recovery error")
	}
	assertMergeFailure(t, app, task.ID, "abandoned merge recovered")
}

func TestLastMergeFailureShownAfterRetry(t *testing.T) {
	app, task := approvedTask(t)
	forceState(t, app, task.ID, StateMerging)
	if err := app.DB.UpdateTaskStateNote(task.ID, StateMergeFailed, "merge failed (into main): boom"); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.UpdateTaskState(task.ID, StateChecksRunning); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return app.CmdWorkStatus(task.ID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Last merge failure: merge failed (into main): boom") || strings.Contains(out, "\nMerge failed:") {
		t.Errorf("status:\n%s", out)
	}
}

func TestEpicMergeFailureRecorded(t *testing.T) {
	app, dir := setupApp(t)
	defer app.Close()
	commitAllowNone(t, dir)
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, "none"); err != nil {
		t.Fatal(err)
	}
	e, _ := app.DB.GetEpic("epic-001")
	gitRun(t, dir, "checkout", e.Branch)
	gitCommitFile(t, dir, "x.txt", "epic\n", "epic side")
	gitRun(t, dir, "checkout", "main")
	gitCommitFile(t, dir, "x.txt", "main\n", "main side")

	if err := app.CmdEpicMerge(e.ID); err == nil {
		t.Fatal("want epic merge failure")
	}
	m, err := app.LastEpicMergeFailure(e.ID)
	if err != nil || m == nil || !strings.Contains(m.Reason, "conflict in x.txt") {
		t.Fatalf("epic merge failure = %+v, %v", m, err)
	}
	out, err := captureStdout(t, func() error { return app.CmdEpicView(e.ID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merge failed: prepare failed") {
		t.Errorf("epic view:\n%s", out)
	}
	if e, _ = app.DB.GetEpic(e.ID); e.Status != EpicStatusOpen {
		t.Errorf("epic status = %s, want open", e.Status)
	}
}

func TestMergeFailureReasonOneLine(t *testing.T) {
	t.Parallel()
	err := errors.New("prepare failed: git rebase main: exit status 1\nAuto-merging a.txt\nCONFLICT (content): Merge conflict in a.txt\nCONFLICT (content): Merge conflict in b.go\n")
	if got := mergeFailureReason(err); got != "prepare failed: git rebase main: exit status 1 (conflict in a.txt, b.go)" {
		t.Errorf("reason = %q", got)
	}
}
