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

	task, err := db.CreateTask("fix bug", "fix-bug", ".worktrees/fix-bug")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "task-001" {
		t.Errorf("ID = %q, want %q", task.ID, "task-001")
	}
	if task.State != StateInProgress {
		t.Errorf("State = %q, want %q", task.State, StateInProgress)
	}

	task2, err := db.CreateTask("add feature", "add-feature", ".worktrees/add-feature")
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

	db.CreateTask("fix bug", "fix-bug", ".worktrees/fix-bug")
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

	db.CreateTask("test", "test-branch", ".worktrees/test")

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

	db.CreateTask("test", "test-branch", ".worktrees/test")

	err = db.RecordReview("task-001", "request_changes", "fix the edge case")
	if err != nil {
		t.Fatal(err)
	}
	err = db.RecordReview("task-001", "approve", "")
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

	db.CreateTask("task A", "branch-a", ".worktrees/a")
	db.CreateTask("task B", "branch-b", ".worktrees/b")

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
