package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Left behind by a merger that timed out under the old state machine or was
// killed: state QUEUED, no queue row.
func TestMergeRecoversQueuedTaskWithoutQueueRow(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	forceState(t, app, task.ID, StateQueued)

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "abandoned merge") {
		t.Fatalf("err = %v, want abandoned merge recovery", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
}

func TestMergeRecoversMergingTaskWithDeadLease(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	forceState(t, app, task.ID, StateMerging)
	insertQueueRow(t, app, task.ID, deadPID(t), time.Now())

	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Fatal("want recovery error")
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
	if entries, _ := app.DB.ListQueue(); len(entries) != 0 {
		t.Errorf("queue = %+v, want dead lease removed", entries)
	}
}

func TestMergeLeavesLiveQueuedTaskAlone(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	forceState(t, app, task.ID, StateQueued)
	insertQueueRow(t, app, task.ID, os.Getppid(), time.Now())

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "already being merged") {
		t.Fatalf("err = %v, want already being merged", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateQueued {
		t.Errorf("state = %s, want QUEUED (live merger owns it)", task.State)
	}
}

func TestStaleRowBehindLiveHeadIsReclaimed(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.Config.Merge.QueueTimeout = 1
	insertQueueRow(t, app, "task-998", os.Getppid(), time.Now())
	insertQueueRow(t, app, "task-999", deadPID(t), time.Now())

	if err := app.CmdWorkMerge(task.ID); err == nil || !strings.Contains(err.Error(), "merge queue timeout") {
		t.Fatalf("err = %v, want queue timeout", err)
	}
	entries, _ := app.DB.ListQueue()
	if len(entries) != 1 || entries[0].TaskID != "task-998" {
		t.Errorf("queue = %+v, want only the live head", entries)
	}
}

// The reported bug: Ctrl-C during a real merge must not leave a queue row
// or a task stuck in MERGING.
func TestInterruptedMergeReleasesQueue(t *testing.T) {
	t.Parallel()
	bin := buildHarnessBinary(t)
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@if [ -n \"$$HARNESS_SLOW\" ]; then sleep 30; fi\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "slow-able makefile")
	if err := RunInit(dir); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "config")
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.CmdNew("slow", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "f")
	for _, step := range []func(string) error{app.CmdWorkCheck, app.CmdReviewRequest} {
		if err := step(task.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "work", "merge", task.ID)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HARNESS_SLOW=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		if task, _ = app.DB.GetTask(task.ID); task.State == StateMerging {
			break
		}
		if i > 200 {
			t.Fatal("merge never started")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("interrupted merge did not exit")
	}

	if entries, _ := app.DB.ListQueue(); len(entries) != 0 {
		t.Errorf("queue = %+v, want empty after interrupt", entries)
	}
	if task, _ = app.DB.GetTask(task.ID); task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
}

func TestQueueClearRecoversTaskWithoutQueueRow(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	forceState(t, app, task.ID, StateQueued)

	if err := app.CmdQueueClear(task.ID, false); err != nil {
		t.Fatal("clear:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
}
