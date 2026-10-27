package internal

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func approvedTask(t *testing.T) (*App, *Task) {
	t.Helper()
	app, _, task := setupTask(t)
	t.Cleanup(func() { app.Close() })
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	return app, task
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func insertQueueRow(t *testing.T, app *App, taskID string, pid int, heartbeat time.Time) {
	t.Helper()
	if _, err := app.DB.conn.Exec(
		`INSERT INTO merge_queue (task_id, owner_pid, heartbeat_unix) VALUES (?, ?, ?)`,
		taskID, pid, heartbeat.Unix(),
	); err != nil {
		t.Fatal(err)
	}
}

func TestMergeReclaimsDeadHead(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	ghost, err := app.DB.CreateTask("ghost", "", "ghost", "/tmp/ghost", "main", "", "", ReviewSetting{})
	if err != nil {
		t.Fatal(err)
	}
	app.DB.conn.Exec(`UPDATE tasks SET state = ? WHERE id = ?`, string(StateMerging), ghost.ID)
	insertQueueRow(t, app, ghost.ID, deadPID(t), time.Now())

	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state = %s, want MERGED", task.State)
	}
	ghost, _ = app.DB.GetTask(ghost.ID)
	if ghost.State != StateMergeFailed {
		t.Errorf("reclaimed task state = %s, want MERGE_FAILED", ghost.State)
	}
}

func TestMergeReclaimsExpiredLease(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	insertQueueRow(t, app, "task-999", os.Getppid(), time.Now().Add(-2*QueueLeaseTTL))

	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	if entries, _ := app.DB.ListQueue(); len(entries) != 0 {
		t.Errorf("queue = %+v, want empty", entries)
	}
}

func TestMergeWaitsForLiveLeaseThenTimesOut(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.Config.Merge.QueueTimeout = 1
	insertQueueRow(t, app, "task-999", os.Getppid(), time.Now())

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "merge queue timeout") {
		t.Fatalf("err = %v, want queue timeout", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
	entries, _ := app.DB.ListQueue()
	if len(entries) != 1 || entries[0].TaskID != "task-999" {
		t.Errorf("queue = %+v, want only the live foreign entry", entries)
	}
}

func TestWaiterExitsWhenItsEntryIsCleared(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.Config.Merge.QueueTimeout = 60
	insertQueueRow(t, app, "task-999", os.Getppid(), time.Now())

	done := make(chan error, 1)
	go func() { done <- app.CmdWorkMerge(task.ID) }()
	for i := 0; ; i++ {
		if pos, _ := app.DB.GetQueuePosition(task.ID); pos == 2 {
			break
		}
		if i > 100 {
			t.Fatal("waiter never enqueued")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := app.CmdQueueClear(task.ID, true); err != nil {
		t.Fatal("clear:", err)
	}

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "removed from the merge queue") {
			t.Errorf("err = %v, want removed from the merge queue", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("waiter did not notice its entry was cleared")
	}
	entries, _ := app.DB.ListQueue()
	if len(entries) != 1 || entries[0].TaskID != "task-999" {
		t.Errorf("queue = %+v, want only the foreign entry", entries)
	}
}

func TestQueueClearRespectsLiveLease(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	insertQueueRow(t, app, "task-998", os.Getppid(), time.Now())
	insertQueueRow(t, app, "task-999", 0, time.Time{})

	if err := app.CmdQueueClear("task-998", false); err == nil {
		t.Error("clearing a live lease without --force should fail")
	}
	if err := app.CmdQueueClear("task-999", false); err != nil {
		t.Errorf("clearing a stale lease: %v", err)
	}
	if err := app.CmdQueueClear("task-998", true); err != nil {
		t.Errorf("force clear: %v", err)
	}
	if entries, _ := app.DB.ListQueue(); len(entries) != 0 {
		t.Errorf("queue = %+v, want empty", entries)
	}
	if err := app.CmdQueueList(); err != nil {
		t.Error(err)
	}
}
