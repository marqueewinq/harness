package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeLeavesMainCheckoutOnItsBranch(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	dir := app.RepoRoot
	gitRun(t, dir, "checkout", "-b", "scratch")
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("uncommitted\n"), 0644)

	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	if branch, _ := app.Git.CurrentBranch(); branch != "scratch" {
		t.Errorf("main checkout switched to %q, want scratch", branch)
	}
	if _, err := os.Stat(filepath.Join(dir, "wip.txt")); err != nil {
		t.Error("uncommitted file in main checkout was lost")
	}
	if _, err := app.Git.run("cat-file", "-e", "main:feature.txt"); err != nil {
		t.Error("feature.txt should be on main")
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); !os.IsNotExist(err) {
		t.Error("feature.txt must not appear in the scratch checkout")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".worktrees")); len(entries) != 0 {
		t.Errorf("temporary worktrees left behind: %v", entries)
	}
}

func TestMergeRefusesTargetWithUnfinishedOperation(t *testing.T) {
	t.Parallel()
	app, task := approvedTask(t)
	mergeHead, err := app.Git.run("rev-parse", "--path-format=absolute", "--git-path", "MERGE_HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head, _ := app.Git.RevParse("HEAD")
	if err := os.WriteFile(mergeHead, []byte(head+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err = app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "unfinished git operation (MERGE_HEAD)") {
		t.Fatalf("err = %v, want unfinished git operation", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMergeFailed {
		t.Errorf("state = %s, want MERGE_FAILED", task.State)
	}
}

func TestEpicMergeLeavesMainCheckoutOnItsBranch(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false); err != nil {
		t.Fatal(err)
	}
	e, _ := app.DB.GetEpic("epic-001")
	tmp := filepath.Join(t.TempDir(), "epic-wt")
	gitRun(t, dir, "worktree", "add", tmp, e.Branch)
	gitCommitFile(t, tmp, "epic.txt", "epic\n", "epic work")
	gitRun(t, dir, "worktree", "remove", tmp)
	gitRun(t, dir, "checkout", "-b", "scratch")

	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewApproveDispatch("epic-001", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicMerge("epic-001"); err != nil {
		t.Fatal("epic merge:", err)
	}
	if branch, _ := app.Git.CurrentBranch(); branch != "scratch" {
		t.Errorf("main checkout switched to %q, want scratch", branch)
	}
	if _, err := app.Git.run("cat-file", "-e", "main:epic.txt"); err != nil {
		t.Error("epic.txt should be on main")
	}
}
