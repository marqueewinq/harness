package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSquashConflictLeavesTargetCheckoutClean(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.Config.Merge.Strategy = "squash"
	app.Config.Merge.RerunChecks = false
	dir := app.RepoRoot
	gitCommitFile(t, task.WorktreePath, "clash.txt", "branch\n", "branch side")
	gitCommitFile(t, dir, "clash.txt", "main\n", "main side")
	// Re-approve the new branch head so merge reaches the squash step.
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	mainBefore, _ := app.Git.RevParse("main")

	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Fatal("squash merge with a conflict should fail")
	}
	if status, _ := app.Git.run("status", "--porcelain"); status != "" {
		t.Errorf("main checkout dirtied by failed squash:\n%s", status)
	}
	if op, _ := app.Git.OperationInProgress(); op != "" {
		t.Errorf("main checkout left with %s", op)
	}
	if after, _ := app.Git.RevParse("main"); after != mainBefore {
		t.Error("main moved on a failed merge")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".worktrees")); len(entries) != 1 {
		t.Errorf("temporary worktrees left behind: %v", entries)
	}
}

func TestSquashIntoUncheckedOutTargetUpdatesRef(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	app.Config.Merge.Strategy = "squash"
	gitRun(t, app.RepoRoot, "checkout", "-b", "elsewhere")

	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	if _, err := app.Git.run("cat-file", "-e", "main:feature.txt"); err != nil {
		t.Error("feature.txt should be on main")
	}
	if branch, _ := app.Git.CurrentBranch(); branch != "elsewhere" {
		t.Errorf("checkout switched to %q", branch)
	}
}

func TestCheckRefusedDuringRebase(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()
	gitCommitFile(t, task.WorktreePath, "clash.txt", "branch\n", "branch side")
	gitCommitFile(t, app.RepoRoot, "clash.txt", "main\n", "main side")
	if err := app.CmdWorkSync(task.ID, false, false); err == nil {
		t.Fatal("sync should stop on conflict")
	}
	err := app.CmdWorkCheck(task.ID)
	if err == nil || !strings.Contains(err.Error(), "unfinished git operation") {
		t.Errorf("check during rebase: err = %v", err)
	}
	if err := app.CmdWorkSync(task.ID, false, true); err != nil {
		t.Fatal("abort:", err)
	}
}

func TestSyncNoopKeepsState(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdWorkSync(task.ID, false, false); err != nil {
		t.Fatal("sync:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state = %s, want CHECKS_PASSED (nothing changed)", task.State)
	}
}

func TestSyncAbortRefusedWhileMerging(t *testing.T) {
	t.Parallel()
	app, task := setupTaskInState(t, StateMerging)
	defer app.Close()
	err := app.CmdWorkSync(task.ID, false, true)
	if err == nil || !strings.Contains(err.Error(), "cannot abort in state MERGING") {
		t.Errorf("err = %v", err)
	}
}

func TestSetupKeepsStricterDefault(t *testing.T) {
	t.Parallel()
	out, err := buildOpencodeConfig([]byte(`{"permission":{"bash":{"*":"ask"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"*": "ask"`) {
		t.Errorf("user's '*': 'ask' was loosened:\n%s", out)
	}
}
