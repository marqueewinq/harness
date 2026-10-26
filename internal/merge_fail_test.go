package internal

import (
	"strings"
	"testing"
)

func TestMergeConflictFailsWithError(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()

	gitCommitFile(t, task.WorktreePath, "conflict.txt", "branch\n", "branch side")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	gitCommitFile(t, dir, "conflict.txt", "main\n", "main side")

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "prepare failed") {
		t.Fatalf("merge with conflict: err = %v, want prepare failed", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
	if first, _ := app.DB.PeekQueue(); first != "" {
		t.Errorf("queue head = %q, want empty", first)
	}
}
