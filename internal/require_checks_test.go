package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergeRefusesWhenRequiredCheckFailedOnApprovedCommit(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	sha, _ := app.Git.RevParse(task.Branch)
	// Simulate tampering/legacy data: the latest run on the approved commit failed.
	runGroup, _ := app.DB.NextRunGroup(task.ID)
	approval, _ := app.DB.GetLatestApproval(task.ID)
	if err := app.DB.RecordCheckRun(task.ID, "test", 1, "", "boom", runGroup, approval.CreatedAt, approval.CreatedAt, sha); err != nil {
		t.Fatal(err)
	}

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "required checks not passed") {
		t.Fatalf("err = %v, want required checks not passed", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state = %s, want APPROVED (nothing queued)", task.State)
	}
	if entries, _ := app.DB.ListQueue(); len(entries) != 0 {
		t.Errorf("queue = %+v, want empty", entries)
	}
}

func TestMergeRefusesApprovalWithoutCommit(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.DB.conn.Exec(`UPDATE reviews SET commit_sha = '' WHERE task_id = ?`, task.ID)

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "no approval pinned") {
		t.Fatalf("err = %v, want no approval pinned", err)
	}
}

func TestPassedChecksAtUsesLatestRunOnCommit(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()
	now := time.Now()
	db.RecordCheckRun("task-001", "test", 0, "", "", 1, now, now, "aaa")
	db.RecordCheckRun("task-001", "test", 1, "", "", 2, now, now, "aaa")
	db.RecordCheckRun("task-001", "test", 0, "", "", 3, now, now, "bbb")

	passed, err := db.PassedChecksAt("task-001", "aaa")
	if err != nil {
		t.Fatal(err)
	}
	if passed["test"] {
		t.Error("latest run on aaa failed; test must not count as passed")
	}
	passed, _ = db.PassedChecksAt("task-001", "bbb")
	if !passed["test"] {
		t.Error("test passed on bbb")
	}
}

func TestUnknownRequiredCheckRejected(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".harness.yaml")
	if err := os.WriteFile(path, []byte("checks:\n  test:\n    run: make test\nmerge:\n  require:\n    checks: [lint]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), `"lint" is not a configured check`) {
		t.Errorf("err = %v, want unknown required check error", err)
	}
}
