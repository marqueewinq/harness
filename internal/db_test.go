package internal

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenDB(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
}

func TestTaskCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	task, err := db.CreateTask("fix bug", "fix-bug", ".worktrees/fix-bug", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "task-001" {
		t.Errorf("ID = %q, want %q", task.ID, "task-001")
	}
	if task.State != StateInProgress {
		t.Errorf("State = %q, want %q", task.State, StateInProgress)
	}

	task2, err := db.CreateTask("add feature", "add-feature", ".worktrees/add-feature", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if task2.ID != "task-002" {
		t.Errorf("ID = %q, want %q", task2.ID, "task-002")
	}

	got, err := db.GetTask("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "fix bug" {
		t.Errorf("Description = %q", got.Description)
	}

	tasks, err := db.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("len(tasks) = %d, want 2", len(tasks))
	}

	err = db.UpdateTaskState("task-001", StateChecksRunning)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTask("task-001")
	if got.State != StateChecksRunning {
		t.Errorf("State = %q, want %q", got.State, StateChecksRunning)
	}
}

func TestGetTaskByBranch(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("fix bug", "fix-bug", ".worktrees/fix-bug", "main", "")
	got, err := db.GetTaskByBranch("fix-bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "task-001" {
		t.Errorf("ID = %q", got.ID)
	}
}

func TestCheckRunCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	err = db.RecordCheckRun("task-001", "test", 0, "ok\n", "", 1, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = db.RecordCheckRun("task-001", "lint", 1, "", "fail\n", 1, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	runs, err := db.GetLatestCheckRuns("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Errorf("len(runs) = %d, want 2", len(runs))
	}
}

func TestReviewCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	err = db.RecordReview("task-001", "request_changes", "fix the edge case", "")
	if err != nil {
		t.Fatal(err)
	}
	err = db.RecordReview("task-001", "approve", "", "")
	if err != nil {
		t.Fatal(err)
	}

	reviews, err := db.GetReviews("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 {
		t.Errorf("len(reviews) = %d, want 2", len(reviews))
	}
	if reviews[0].Decision != "request_changes" {
		t.Errorf("reviews[0].Decision = %q", reviews[0].Decision)
	}
}

func TestMergeQueue(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("task A", "branch-a", ".worktrees/a", "main", "")
	db.CreateTask("task B", "branch-b", ".worktrees/b", "main", "")

	pos1, err := db.EnqueueTask("task-001")
	if err != nil {
		t.Fatal(err)
	}
	pos2, err := db.EnqueueTask("task-002")
	if err != nil {
		t.Fatal(err)
	}
	if pos1 >= pos2 {
		t.Errorf("pos1=%d should be < pos2=%d", pos1, pos2)
	}

	first, err := db.PeekQueue()
	if err != nil {
		t.Fatal(err)
	}
	if first != "task-001" {
		t.Errorf("first = %q, want task-001", first)
	}

	err = db.DequeueTask("task-001")
	if err != nil {
		t.Fatal(err)
	}

	first, err = db.PeekQueue()
	if err != nil {
		t.Fatal(err)
	}
	if first != "task-002" {
		t.Errorf("first after dequeue = %q, want task-002", first)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.GetTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestGetTaskByBranchNotFound(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.GetTaskByBranch("nope")
	if err == nil {
		t.Error("expected error for nonexistent branch")
	}
}

func TestGetSubtasks(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create parent
	parent, err := db.CreateTask("parent", "parent-branch", ".worktrees/parent", "main", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create 2 children
	_, err = db.CreateTask("child 1", "child-1", ".worktrees/child-1", parent.Branch, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateTask("child 2", "child-2", ".worktrees/child-2", parent.Branch, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	subtasks, err := db.GetSubtasks(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subtasks) != 2 {
		t.Errorf("len(subtasks) = %d, want 2", len(subtasks))
	}
	if subtasks[0].Description != "child 1" {
		t.Errorf("subtasks[0].Description = %q, want %q", subtasks[0].Description, "child 1")
	}
	if subtasks[1].Description != "child 2" {
		t.Errorf("subtasks[1].Description = %q, want %q", subtasks[1].Description, "child 2")
	}
}

func TestGetSubtasksNone(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	parent, err := db.CreateTask("parent", "parent-branch", ".worktrees/parent", "main", "")
	if err != nil {
		t.Fatal(err)
	}

	subtasks, err := db.GetSubtasks(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subtasks) != 0 {
		t.Errorf("len(subtasks) = %d, want 0", len(subtasks))
	}
}

func TestNextRunGroup(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	// No runs yet -> group 1
	group, err := db.NextRunGroup("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if group != 1 {
		t.Errorf("NextRunGroup (no runs) = %d, want 1", group)
	}

	// Record a run in group 1
	db.RecordCheckRun("task-001", "lint", 0, "", "", 1, time.Now(), time.Now())

	// Next group should be 2
	group, err = db.NextRunGroup("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if group != 2 {
		t.Errorf("NextRunGroup (after group 1) = %d, want 2", group)
	}
}

func TestGetLatestCheckRunsEmpty(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	runs, err := db.GetLatestCheckRuns("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if runs != nil {
		t.Errorf("GetLatestCheckRuns (empty) = %v, want nil", runs)
	}
}

func TestPeekQueueEmpty(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.PeekQueue()
	if err == nil {
		t.Error("expected error for empty queue")
	}
}

func TestGetQueuePositionMultiple(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("task A", "branch-a", ".worktrees/a", "main", "")
	db.CreateTask("task B", "branch-b", ".worktrees/b", "main", "")
	db.CreateTask("task C", "branch-c", ".worktrees/c", "main", "")

	db.EnqueueTask("task-001")
	db.EnqueueTask("task-002")
	db.EnqueueTask("task-003")

	pos1, err := db.GetQueuePosition("task-001")
	if err != nil {
		t.Fatal(err)
	}
	pos2, err := db.GetQueuePosition("task-002")
	if err != nil {
		t.Fatal(err)
	}
	pos3, err := db.GetQueuePosition("task-003")
	if err != nil {
		t.Fatal(err)
	}

	if pos1 != 1 {
		t.Errorf("position of task-001 = %d, want 1", pos1)
	}
	if pos2 != 2 {
		t.Errorf("position of task-002 = %d, want 2", pos2)
	}
	if pos3 != 3 {
		t.Errorf("position of task-003 = %d, want 3", pos3)
	}
}

func TestRecordReviewWithCommitSHA(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	sha := "abc123def456abc123def456abc123def456abc1"
	err = db.RecordReview("task-001", "approve", "", sha)
	if err != nil {
		t.Fatal(err)
	}

	reviews, err := db.GetReviews("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 {
		t.Fatalf("len(reviews) = %d, want 1", len(reviews))
	}
	if reviews[0].CommitSHA != sha {
		t.Errorf("CommitSHA = %q, want %q", reviews[0].CommitSHA, sha)
	}
}

func TestGetLatestApproval(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	// Record request_changes, then approve with SHA
	db.RecordReview("task-001", "request_changes", "fix it", "")
	sha := "abc123def456abc123def456abc123def456abc1"
	db.RecordReview("task-001", "approve", "", sha)

	approval, err := db.GetLatestApproval("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil {
		t.Fatal("expected non-nil approval")
	}
	if approval.Decision != "approve" {
		t.Errorf("Decision = %q, want %q", approval.Decision, "approve")
	}
	if approval.CommitSHA != sha {
		t.Errorf("CommitSHA = %q, want %q", approval.CommitSHA, sha)
	}
}

func TestGetLatestApprovalNone(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test", "main", "")

	approval, err := db.GetLatestApproval("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if approval != nil {
		t.Errorf("expected nil approval, got %+v", approval)
	}
}

func TestDequeueNonexistent(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// DELETE is idempotent - should not error
	err = db.DequeueTask("nonexistent-task")
	if err != nil {
		t.Errorf("DequeueTask nonexistent should not error, got: %v", err)
	}
}
