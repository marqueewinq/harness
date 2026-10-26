package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func forceState(t *testing.T, app *App, id string, state TaskState) {
	t.Helper()
	if _, err := app.DB.conn.Exec(`UPDATE tasks SET state = ? WHERE id = ?`, string(state), id); err != nil {
		t.Fatal(err)
	}
}

func taskState(t *testing.T, app *App, id string) TaskState {
	t.Helper()
	task, err := app.DB.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task.State
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func rebaseInProgress(t *testing.T, dir string) bool {
	t.Helper()
	ok, err := NewGitClient(dir).RebaseInProgress()
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestWorkSyncCleanRebase(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()
	gitCommitFile(t, dir, "base.txt", "base\n", "base change")
	forceState(t, app, task.ID, StateMergeFailed)

	if err := app.CmdWorkSync(task.ID, false, false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(task.WorktreePath, "base.txt")); err != nil {
		t.Errorf("base commit not picked up: %v", err)
	}
	if mb := gitIn(t, task.WorktreePath, "merge-base", "HEAD", "main"); mb != gitIn(t, dir, "rev-parse", "main") {
		t.Errorf("task branch not based on main tip")
	}
	if s := taskState(t, app, task.ID); s != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", s)
	}
}

func TestWorkSyncInProgressStaysInProgress(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()
	gitCommitFile(t, dir, "base.txt", "base\n", "base change")
	if err := app.CmdWorkSync(task.ID, false, false); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if s := taskState(t, app, task.ID); s != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", s)
	}
}

func setupConflict(t *testing.T) (*App, *Task) {
	t.Helper()
	app, dir, task := setupTask(t)
	t.Cleanup(func() { app.Close() })
	gitCommitFile(t, dir, "feature.txt", "base version\n", "conflicting base change")
	forceState(t, app, task.ID, StateMergeFailed)

	err := app.CmdWorkSync(task.ID, false, false)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
	if !rebaseInProgress(t, task.WorktreePath) {
		t.Fatal("rebase should be left in progress")
	}
	if s := taskState(t, app, task.ID); s != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED while conflicted", s)
	}
	return app, task
}

func TestWorkSyncConflictThenContinue(t *testing.T) {
	t.Parallel()
	app, task := setupConflict(t)

	files, err := NewGitClient(task.WorktreePath).ConflictedFiles()
	if err != nil || len(files) != 1 || files[0] != "feature.txt" {
		t.Fatalf("conflicted files = %v, %v", files, err)
	}

	if err := app.CmdWorkSync(task.ID, false, false); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("plain sync during rebase: got %v", err)
	}
	if err := app.CmdWorkSync(task.ID, true, false); err == nil {
		t.Error("continue with unresolved conflicts should fail")
	}

	os.WriteFile(filepath.Join(task.WorktreePath, "feature.txt"), []byte("merged\n"), 0644)
	gitIn(t, task.WorktreePath, "add", "feature.txt")
	if err := app.CmdWorkSync(task.ID, true, false); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if rebaseInProgress(t, task.WorktreePath) {
		t.Error("rebase should be finished")
	}
	if s := taskState(t, app, task.ID); s != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", s)
	}
	if got := gitIn(t, task.WorktreePath, "log", "-1", "--format=%s"); got != "add feature" {
		t.Errorf("HEAD subject = %q, want task commit", got)
	}
}

func TestWorkSyncAbort(t *testing.T) {
	t.Parallel()
	app, task := setupConflict(t)
	before := gitIn(t, task.WorktreePath, "rev-parse", "ORIG_HEAD")

	if err := app.CmdWorkSync(task.ID, false, true); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if rebaseInProgress(t, task.WorktreePath) {
		t.Error("rebase should be aborted")
	}
	if after := gitIn(t, task.WorktreePath, "rev-parse", "HEAD"); after != before {
		t.Errorf("HEAD = %s, want restored %s", after, before)
	}
	if s := taskState(t, app, task.ID); s != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED after abort", s)
	}
	if err := app.CmdWorkSync(task.ID, false, true); err == nil {
		t.Error("abort with no rebase in progress should fail")
	}
}

func TestWorkSyncDirtyWorktree(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()
	gitCommitFile(t, dir, "base.txt", "base\n", "base change")
	head := gitIn(t, task.WorktreePath, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(task.WorktreePath, "feature.txt"), []byte("dirty\n"), 0644)

	err := app.CmdWorkSync(task.ID, false, false)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("expected uncommitted changes error, got %v", err)
	}
	if got := gitIn(t, task.WorktreePath, "rev-parse", "HEAD"); got != head {
		t.Error("HEAD moved despite refusal")
	}
}

func TestWorkSyncWrongState(t *testing.T) {
	t.Parallel()
	for _, state := range []TaskState{StateReviewRequested, StateApproved, StateQueued, StateMerging, StateMerged, StateClosed} {
		app, task := setupTaskInState(t, state)
		err := app.CmdWorkSync(task.ID, false, false)
		if err == nil || !strings.Contains(err.Error(), "cannot sync") {
			t.Errorf("state %s: got %v", state, err)
		}
		if state == StateApproved && (err == nil || !strings.Contains(err.Error(), "harness work check")) {
			t.Errorf("APPROVED refusal should say to re-run checks: %v", err)
		}
		app.Close()
	}
}
