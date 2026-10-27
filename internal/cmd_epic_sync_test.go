package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func epicWithCommit(t *testing.T, file, content string) (*App, string, *Epic) {
	t.Helper()
	app, dir := setupApp(t)
	t.Cleanup(func() { app.Close() })
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, ""); err != nil {
		t.Fatal(err)
	}
	e, _ := app.DB.GetEpic("epic-001")
	tmp := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", tmp, e.Branch)
	gitCommitFile(t, tmp, file, content, "epic work")
	gitRun(t, dir, "worktree", "remove", tmp)
	return app, dir, e
}

func TestEpicSyncPicksUpMain(t *testing.T) {
	t.Parallel()
	app, dir, e := epicWithCommit(t, "epic.txt", "epic\n")
	gitCommitFile(t, dir, "main.txt", "main\n", "main moves on")

	if err := app.CmdEpicSync(e.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Git.run("merge-base", "--is-ancestor", "main", e.Branch); err != nil {
		t.Error("epic branch should now contain main")
	}
	if _, err := app.Git.run("cat-file", "-e", e.Branch+":epic.txt"); err != nil {
		t.Error("epic work lost")
	}
	if _, err := os.Stat(app.epicSyncWorktree(e.ID)); !os.IsNotExist(err) {
		t.Error("sync worktree left behind")
	}
}

func TestEpicSyncConflictContinueAndAbort(t *testing.T) {
	t.Parallel()
	app, dir, e := epicWithCommit(t, "clash.txt", "epic\n")
	gitCommitFile(t, dir, "clash.txt", "main\n", "main side")
	before, _ := app.Git.RevParse(e.Branch)

	err := app.CmdEpicSync(e.ID, false, false)
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("err = %v, want conflicts", err)
	}
	if err := app.CmdEpicSync(e.ID, false, false); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("second sync: err = %v", err)
	}
	if err := app.CmdEpicSync(e.ID, false, true); err != nil {
		t.Fatal("abort:", err)
	}
	if after, _ := app.Git.RevParse(e.Branch); after != before {
		t.Error("abort must leave the epic branch unchanged")
	}

	if err := app.CmdEpicSync(e.ID, false, false); err == nil {
		t.Fatal("want conflict again")
	}
	wt := app.epicSyncWorktree(e.ID)
	if err := os.WriteFile(filepath.Join(wt, "clash.txt"), []byte("resolved\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, wt, "add", "clash.txt")
	if err := app.CmdEpicSync(e.ID, true, false); err != nil {
		t.Fatal("continue:", err)
	}
	if out, _ := app.Git.run("show", e.Branch+":clash.txt"); out != "resolved" {
		t.Errorf("clash.txt = %q, want resolved", out)
	}
}

func TestEpicSyncIgnoresStrayDirectory(t *testing.T) {
	t.Parallel()
	app, _, e := epicWithCommit(t, "epic.txt", "epic\n")
	if err := os.MkdirAll(app.epicSyncWorktree(e.ID), 0755); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][2]bool{{false, false}, {true, false}, {false, true}} {
		err := app.CmdEpicSync(e.ID, flags[0], flags[1])
		if err == nil || !strings.Contains(err.Error(), "not an epic sync worktree") {
			t.Errorf("flags %v: err = %v", flags, err)
		}
	}
}

func TestEpicSyncNothingInProgress(t *testing.T) {
	t.Parallel()
	app, _, e := epicWithCommit(t, "epic.txt", "epic\n")
	for _, flags := range [][2]bool{{true, false}, {false, true}} {
		if err := app.CmdEpicSync(e.ID, flags[0], flags[1]); err == nil || !strings.Contains(err.Error(), "no epic sync in progress") {
			t.Errorf("flags %v: err = %v", flags, err)
		}
	}
}

func TestEpicSyncRefusedWhileTaskMerging(t *testing.T) {
	t.Parallel()
	app, _, e := epicWithCommit(t, "epic.txt", "epic\n")
	if err := app.CmdTaskNew("t", "", e.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdTaskStart("task-001", ""); err != nil {
		t.Fatal(err)
	}
	forceState(t, app, "task-001", StateMerging)
	err := app.CmdEpicSync(e.ID, false, false)
	if err == nil || !strings.Contains(err.Error(), "MERGING") {
		t.Errorf("err = %v, want refusal while merging", err)
	}
}
