package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("mustAtoi(%q): %v", s, err))
	}
	return n
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
}

func TestE2EWorkflow(t *testing.T) {
	t.Parallel()
	// Create a temp git repo with a passing test command
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo all tests pass\n"), 0644)
	cmd := exec.Command("git", "add", "Makefile")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add makefile")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()

	// harness init
	err := RunInit(dir)
	if err != nil {
		t.Fatal("init:", err)
	}

	// Commit the config
	cmd = exec.Command("git", "add", ".harness.yaml", ".gitignore")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add harness config")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()

	// Load app
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal("load app:", err)
	}
	defer app.Close()

	// harness new
	err = app.CmdNew("add hello feature", "", "", "")
	if err != nil {
		t.Fatal("new:", err)
	}

	// Verify task created
	tasks, _ := app.DB.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}

	// Make a change in the worktree
	os.WriteFile(filepath.Join(task.WorktreePath, "hello.txt"), []byte("hello\n"), 0644)
	cmd = exec.Command("git", "add", "hello.txt")
	cmd.Dir = task.WorktreePath
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add hello")
	cmd.Dir = task.WorktreePath
	cmd.Env = gitEnv()
	cmd.Run()

	// harness check
	err = app.CmdWorkCheck(task.ID)
	if err != nil {
		t.Fatal("check:", err)
	}
	task2, _ := app.DB.GetTask(task.ID)
	if task2.State != StateChecksPassed {
		t.Errorf("state after check = %s, want CHECKS_PASSED", task2.State)
	}

	// harness request-review
	err = app.CmdReviewRequest(task.ID)
	if err != nil {
		t.Fatal("request-review:", err)
	}
	task3, _ := app.DB.GetTask(task.ID)
	if task3.State != StateReviewRequested {
		t.Errorf("state after request-review = %s, want REVIEW_REQUESTED", task3.State)
	}

	// harness review approve
	err = app.CmdReviewApprove(task.ID, "")
	if err != nil {
		t.Fatal("record-review:", err)
	}
	task4, _ := app.DB.GetTask(task.ID)
	if task4.State != StateApproved {
		t.Errorf("state after approve = %s, want APPROVED", task4.State)
	}

	// harness merge
	err = app.CmdWorkMerge(task.ID)
	if err != nil {
		t.Fatal("merge:", err)
	}
	task5, _ := app.DB.GetTask(task.ID)
	if task5.State != StateMerged {
		t.Errorf("state after merge = %s, want MERGED", task5.State)
	}

	// Verify file exists on main
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); os.IsNotExist(err) {
		t.Error("hello.txt should exist on main after merge")
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %s\n%s", args, err, out)
	}
}

func TestCmdCloseWithoutCleanup(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()

	err := app.CmdWorkClose(task.ID, false)
	if err != nil {
		t.Fatal("close:", err)
	}

	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateClosed {
		t.Errorf("state = %s, want CLOSED", task.State)
	}

	// Worktree and branch should still exist
	if _, err := os.Stat(task.WorktreePath); os.IsNotExist(err) {
		t.Error("worktree should still exist without cleanup")
	}
}

func TestCmdCloseWithCleanup(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()

	wtPath := task.WorktreePath
	branch := task.Branch

	// Close with cleanup
	err := app.CmdWorkClose(task.ID, true)
	if err != nil {
		t.Fatal("close:", err)
	}

	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateClosed {
		t.Errorf("state = %s, want CLOSED", task.State)
	}

	// Worktree should be removed
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed with cleanup")
	}

	// Branch should be deleted
	out, berr := exec.Command("git", "-C", dir, "branch", "--list", branch).CombinedOutput()
	if berr != nil {
		t.Fatalf("git branch --list failed: %s", berr)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Errorf("branch %s should be deleted, but still exists", branch)
	}
}

func TestE2EFeatureBranch(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add makefile")

	RunInit(dir)
	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "add harness config")

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Create parent feature task
	err = app.CmdNew("big feature", "", "", "")
	if err != nil {
		t.Fatal("new parent:", err)
	}
	parent, _ := app.DB.GetTask("task-001")
	if parent.BaseBranch != "main" {
		t.Errorf("parent BaseBranch = %q, want main", parent.BaseBranch)
	}

	// Create subtask targeting parent's branch
	err = app.CmdNew("subtask one", parent.Branch, "", "")
	if err != nil {
		t.Fatal("new subtask:", err)
	}
	subtask, _ := app.DB.GetTask("task-002")
	if subtask.BaseBranch != parent.Branch {
		t.Errorf("subtask BaseBranch = %q, want %q", subtask.BaseBranch, parent.Branch)
	}
	if subtask.ParentTaskID != parent.ID {
		t.Errorf("subtask ParentTaskID = %q, want %q", subtask.ParentTaskID, parent.ID)
	}

	// Make a change in subtask worktree
	os.WriteFile(filepath.Join(subtask.WorktreePath, "sub.txt"), []byte("subtask\n"), 0644)
	gitRun(t, subtask.WorktreePath, "add", "sub.txt")
	gitRun(t, subtask.WorktreePath, "commit", "-m", "add sub.txt")

	// Subtask: check -> review -> merge (into parent branch)
	app.CmdWorkCheck(subtask.ID)
	app.CmdReviewRequest(subtask.ID)
	app.CmdReviewApprove(subtask.ID, "")
	err = app.CmdWorkMerge(subtask.ID)
	if err != nil {
		t.Fatal("merge subtask:", err)
	}

	// Verify sub.txt is on parent branch, not main
	parentGit := NewGitClient(parent.WorktreePath)
	parentGit.run("reset", "--hard", parent.Branch)
	if _, err := os.Stat(filepath.Join(parent.WorktreePath, "sub.txt")); os.IsNotExist(err) {
		t.Error("sub.txt should exist on parent branch after subtask merge")
	}
	if _, serr := os.Stat(filepath.Join(dir, "sub.txt")); !os.IsNotExist(serr) {
		t.Error("sub.txt should NOT exist on main yet")
	}

	// Now merge parent into main: check -> review -> merge
	app.CmdWorkCheck(parent.ID)
	app.CmdReviewRequest(parent.ID)
	app.CmdReviewApprove(parent.ID, "")
	err = app.CmdWorkMerge(parent.ID)
	if err != nil {
		t.Fatal("merge parent:", err)
	}

	// Now sub.txt should be on main
	mainGit := NewGitClient(dir)
	mainGit.Checkout("main")
	if _, err := os.Stat(filepath.Join(dir, "sub.txt")); os.IsNotExist(err) {
		t.Error("sub.txt should exist on main after parent merge")
	}
}

// --- Command Error Path Tests ---

// setupApp copies the pre-built app repo template and opens a fresh DB.
// Caller must defer app.Close().
func setupApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	if err := copyDir(testAppRepoTemplate, dir); err != nil {
		t.Fatalf("copying app template: %v", err)
	}
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	return app, dir
}

// setupTask creates app + new task with a commit in worktree. Returns app, dir, task.
func setupTask(t *testing.T) (*App, string, *Task) {
	t.Helper()
	app, dir := setupApp(t)
	if err := app.CmdNew("test feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	// Add a commit so checks can run
	gitCommitFile(t, task.WorktreePath, "feature.txt", "feature\n", "add feature")
	return app, dir, task
}

// setupTaskInState creates a DB-only task in the given state, with a minimal
// App that has no real git worktree. For tests that only check error messages
// on wrong-state commands (no actual git/check execution needed).
func setupTaskInState(t *testing.T, state TaskState) (*App, *Task) {
	t.Helper()
	db := openTestDB(t)
	task, err := db.CreateTask("test task", "", "fake-branch", "/tmp/fake-wt", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if state != StateInProgress {
		// Force state directly (bypass ValidTransition for test setup)
		db.conn.Exec(`UPDATE tasks SET state = ? WHERE id = ?`, string(state), task.ID)
		task.State = state
	}
	app := &App{
		Config:   &Config{MainBranch: "main", Merge: MergeConfig{Require: MergeRequire{Review: "approved"}}},
		DB:       db,
		Git:      NewGitClient(t.TempDir()),
		RepoRoot: t.TempDir(),
	}
	return app, task
}

func TestCmdCheckWrongState(t *testing.T) {
	t.Parallel()
	for _, state := range []TaskState{StateApproved, StateQueued} {
		app, task := setupTaskInState(t, state)
		err := app.CmdWorkCheck(task.ID)
		if err == nil {
			t.Errorf("expected error running check in %s state", state)
		}
		if err != nil && !strings.Contains(err.Error(), "cannot run checks") {
			t.Errorf("state %s: unexpected error message: %s", state, err)
		}
		app.Close()
	}
}

func TestCmdRequestReviewWrongState(t *testing.T) {
	t.Parallel()
	for _, state := range []TaskState{StateInProgress, StateChecksFailed} {
		app, task := setupTaskInState(t, state)
		err := app.CmdReviewRequest(task.ID)
		if err == nil {
			t.Errorf("expected error requesting review in %s state", state)
		}
		if err != nil && !strings.Contains(err.Error(), "cannot request review") {
			t.Errorf("state %s: unexpected error message: %s", state, err)
		}
		app.Close()
	}
}

func TestCmdRecordReviewWrongState(t *testing.T) {
	t.Parallel()
	app, task := setupTaskInState(t, StateInProgress)
	defer app.Close()

	err := app.CmdReviewApprove(task.ID, "")
	if err == nil {
		t.Error("expected error recording review in IN_PROGRESS state")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot record review") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdRecordReviewMissingComment(t *testing.T) {
	t.Parallel()
	app, task := setupTaskInState(t, StateReviewRequested)
	defer app.Close()

	err := app.CmdReviewReject(task.ID, "")
	if err == nil {
		t.Error("expected error for reject without comment")
	}
	if err != nil && !strings.Contains(err.Error(), "--comment is required for reject") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdMergeWrongState(t *testing.T) {
	t.Parallel()
	app, task := setupTaskInState(t, StateInProgress)
	defer app.Close()

	err := app.CmdWorkMerge(task.ID)
	if err == nil {
		t.Error("expected error merging in IN_PROGRESS state")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot merge") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdInitAlreadyInitialized(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add makefile")

	// First init should succeed
	err := RunInit(dir)
	if err != nil {
		t.Fatal("first init:", err)
	}

	// Second init should fail
	err = RunInit(dir)
	if err == nil {
		t.Error("expected error on second RunInit")
	}
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdCloseWrongState(t *testing.T) {
	t.Parallel()
	app, task := setupTaskInState(t, StateMerged)
	defer app.Close()

	err := app.CmdWorkClose(task.ID, false)
	if err == nil {
		t.Error("expected error closing already-merged task")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot close") {
		t.Errorf("unexpected error message: %s", err)
	}
}

// --- E2E Workflow Tests ---

func TestE2ERejectionCycle(t *testing.T) {
	t.Parallel()
	app, dir, task := setupTask(t)
	defer app.Close()

	// check -> request-review -> request-changes
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewReject(task.ID, "needs more tests"); err != nil {
		t.Fatal("review reject:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateRequestChanges {
		t.Errorf("state after rejection = %s, want REQUEST_CHANGES", task.State)
	}

	// Make another commit addressing feedback
	os.WriteFile(filepath.Join(task.WorktreePath, "tests.txt"), []byte("more tests\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "tests.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add more tests")

	// check again -> request-review -> approve -> merge
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("second check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state after second check = %s, want CHECKS_PASSED", task.State)
	}

	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("second request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state after approve = %s, want APPROVED", task.State)
	}

	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state after merge = %s, want MERGED", task.State)
	}

	// Verify files on main
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); os.IsNotExist(err) {
		t.Error("feature.txt should exist on main after merge")
	}
	if _, err := os.Stat(filepath.Join(dir, "tests.txt")); os.IsNotExist(err) {
		t.Error("tests.txt should exist on main after merge")
	}
}

func TestE2ECheckFailFix(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	// Start with FAILING Makefile
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@exit 1\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add failing makefile")

	RunInit(dir)
	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "add harness config")

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if err := app.CmdNew("fix tests", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	// Add a commit
	os.WriteFile(filepath.Join(task.WorktreePath, "code.txt"), []byte("code\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "code.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add code")

	if err := app.CmdWorkCheck(task.ID); err == nil || !strings.Contains(err.Error(), "checks failed") {
		t.Errorf("failing check: err = %v, want checks failed", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksFailed {
		t.Errorf("state after failing check = %s, want CHECKS_FAILED", task.State)
	}

	// Fix Makefile in worktree
	os.WriteFile(filepath.Join(task.WorktreePath, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "Makefile")
	gitRun(t, task.WorktreePath, "commit", "-m", "fix makefile")

	// Check again -> should pass
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("second check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state after fix = %s, want CHECKS_PASSED", task.State)
	}

	// Continue: request-review -> approve -> merge
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state after merge = %s, want MERGED", task.State)
	}

	// Verify file on main
	if _, err := os.Stat(filepath.Join(dir, "code.txt")); os.IsNotExist(err) {
		t.Error("code.txt should exist on main after merge")
	}
}

func TestE2ESquashMerge(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add makefile")

	RunInit(dir)

	// Modify .harness.yaml to use squash strategy before LoadApp
	cfgPath := filepath.Join(dir, ".harness.yaml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := strings.Replace(string(cfgData), "strategy: merge", "strategy: squash", 1)
	os.WriteFile(cfgPath, []byte(cfgStr), 0644)

	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "add harness config")

	// Count commits on main before merge
	countOut, cerr := exec.Command("git", "-C", dir, "rev-list", "--count", "main").CombinedOutput()
	if cerr != nil {
		t.Fatal(cerr)
	}
	baseCount := strings.TrimSpace(string(countOut))

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if err := app.CmdNew("squash feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	// Make 2 commits
	os.WriteFile(filepath.Join(task.WorktreePath, "a.txt"), []byte("a\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "a.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add a")

	os.WriteFile(filepath.Join(task.WorktreePath, "b.txt"), []byte("b\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "b.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add b")

	// check -> request-review -> approve -> merge
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state = %s, want MERGED", task.State)
	}

	// Verify files on main
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); os.IsNotExist(err) {
		t.Error("a.txt should exist on main")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); os.IsNotExist(err) {
		t.Error("b.txt should exist on main")
	}

	// Squash should add exactly 1 commit on main
	countOut, cerr = exec.Command("git", "-C", dir, "rev-list", "--count", "main").CombinedOutput()
	if cerr != nil {
		t.Fatal(cerr)
	}
	newCount := strings.TrimSpace(string(countOut))
	// baseCount was before merge; squash adds exactly 1 commit
	expectedCount := fmt.Sprintf("%d", mustAtoi(baseCount)+1)
	if newCount != expectedCount {
		t.Errorf("commit count on main = %s, want %s (base %s + 1 squash)", newCount, expectedCount, baseCount)
	}
}

func TestE2EStaleReviewBlocksMerge(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()

	// check -> request-review -> approve
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Fatalf("expected APPROVED, got %s", task.State)
	}

	// Make another commit AFTER approval
	os.WriteFile(filepath.Join(task.WorktreePath, "extra.txt"), []byte("extra\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "extra.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add extra after approval")

	err := app.CmdWorkMerge(task.ID)
	if err == nil || !strings.Contains(err.Error(), "review is stale") {
		t.Fatalf("merge on stale review: err = %v, want review is stale", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateInProgress {
		t.Errorf("state after stale merge = %s, want IN_PROGRESS", task.State)
	}

	// Re-do: check -> request-review -> approve -> merge
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("re-check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("re-request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("re-approve:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("re-merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state after re-merge = %s, want MERGED", task.State)
	}
}

func TestE2EMergeWithUnchangedReview(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()

	// Normal flow: check -> request-review -> approve -> merge (no new commits)
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state = %s, want MERGED", task.State)
	}
}

func TestE2ECustomReviewType(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add makefile")

	RunInit(dir)

	// Modify .harness.yaml to use agent review type
	cfgPath := filepath.Join(dir, ".harness.yaml")
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := strings.Replace(string(cfgData), "type: human", "type: agent", 1)
	os.WriteFile(cfgPath, []byte(cfgStr), 0644)

	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "add harness config")

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Verify config loaded with agent review type
	if app.Config.Review.Type != "agent" {
		t.Errorf("review type = %q, want %q", app.Config.Review.Type, "agent")
	}

	if err := app.CmdNew("agent reviewed feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	os.WriteFile(filepath.Join(task.WorktreePath, "agent.txt"), []byte("agent\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "agent.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add agent file")

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	// Agent review without review.cmd can never produce an authorized verdict.
	err = app.CmdReviewRequest(task.ID)
	if err == nil || !strings.Contains(err.Error(), "review.cmd is empty") {
		t.Fatalf("request-review: err = %v, want review.cmd is empty", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state = %s, want CHECKS_PASSED", task.State)
	}
}

// --- Planning E2E Tests ---

func TestE2EPlanningWorkflow(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	// Phase 1: Planning -- create project, epic, 3 tasks
	if err := app.CmdProjectNew("Test Project", "project description"); err != nil {
		t.Fatal("project new:", err)
	}
	if err := app.CmdEpicNew("Test Epic", "epic description", "proj-001", "", false); err != nil {
		t.Fatal("epic new:", err)
	}
	if err := app.CmdTaskNew("task A", "", "epic-001"); err != nil {
		t.Fatal("plan A:", err)
	}
	if err := app.CmdTaskNew("task B", "", "epic-001"); err != nil {
		t.Fatal("plan B:", err)
	}
	if err := app.CmdTaskNew("task C", "", "epic-001"); err != nil {
		t.Fatal("plan C:", err)
	}

	// Verify all PLANNED with empty branch/worktree
	for _, id := range []string{"task-001", "task-002", "task-003"} {
		task, err := app.DB.GetTask(id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if task.State != StatePlanned {
			t.Errorf("%s state = %s, want PLANNED", id, task.State)
		}
		if task.Branch != "" {
			t.Errorf("%s branch = %q, want empty", id, task.Branch)
		}
		if task.WorktreePath != "" {
			t.Errorf("%s worktree = %q, want empty", id, task.WorktreePath)
		}
		if task.EpicID != "epic-001" {
			t.Errorf("%s epicID = %q, want epic-001", id, task.EpicID)
		}
	}

	// Add dependencies: A blocks B, B blocks C
	if err := app.CmdDepAdd("task-001", "task-002", "blocks"); err != nil {
		t.Fatal("dep A->B:", err)
	}
	if err := app.CmdDepAdd("task-002", "task-003", "blocks"); err != nil {
		t.Fatal("dep B->C:", err)
	}

	// Verify ready: only A ready, B and C blocked
	ready, blocked, err := app.ComputeReady()
	if err != nil {
		t.Fatal("ComputeReady:", err)
	}
	if len(ready) != 1 || ready[0].ID != "task-001" {
		t.Errorf("ready = %v, want [task-001]", taskIDs(ready))
	}
	if len(blocked) != 2 {
		t.Errorf("blocked count = %d, want 2", len(blocked))
	}

	// Phase 2: Execute task A
	if err := app.CmdTaskStart("task-001", ""); err != nil {
		t.Fatal("start A:", err)
	}
	taskA, _ := app.DB.GetTask("task-001")
	if taskA.State != StateInProgress {
		t.Errorf("A state = %s, want IN_PROGRESS", taskA.State)
	}
	if taskA.Branch == "" {
		t.Error("A branch should be set after start")
	}
	if taskA.WorktreePath == "" {
		t.Error("A worktree should be set after start")
	}
	if _, err := os.Stat(taskA.WorktreePath); os.IsNotExist(err) {
		t.Error("A worktree dir should exist")
	}

	// Make change, commit in worktree
	os.WriteFile(filepath.Join(taskA.WorktreePath, "a.txt"), []byte("task A\n"), 0644)
	gitRun(t, taskA.WorktreePath, "add", "a.txt")
	gitRun(t, taskA.WorktreePath, "commit", "-m", "implement task A")

	// Full flow: check -> request-review -> approve -> merge
	if err := app.CmdWorkCheck(taskA.ID); err != nil {
		t.Fatal("check A:", err)
	}
	if err := app.CmdReviewRequest(taskA.ID); err != nil {
		t.Fatal("request-review A:", err)
	}
	if err := app.CmdReviewApprove(taskA.ID, ""); err != nil {
		t.Fatal("approve A:", err)
	}
	if err := app.CmdWorkMerge(taskA.ID); err != nil {
		t.Fatal("merge A:", err)
	}
	taskA, _ = app.DB.GetTask(taskA.ID)
	if taskA.State != StateMerged {
		t.Errorf("A state after merge = %s, want MERGED", taskA.State)
	}

	// Phase 3: Verify dependency resolution
	ready, blocked, err = app.ComputeReady()
	if err != nil {
		t.Fatal("ComputeReady after merge:", err)
	}
	if len(ready) != 1 || ready[0].ID != "task-002" {
		t.Errorf("ready after A merged = %v, want [task-002]", taskIDs(ready))
	}
	if len(blocked) != 1 || blocked[0].ID != "task-003" {
		t.Errorf("blocked after A merged = %v, want [task-003]", taskIDs(blocked))
	}

	// Phase 4: Smoke test display commands (should not error)
	if err := app.CmdTaskList("", "", "all", false); err != nil {
		t.Errorf("CmdTaskList: %v", err)
	}
	if err := app.CmdProjectView("proj-001"); err != nil {
		t.Errorf("CmdProjectView: %v", err)
	}
	if err := app.CmdEpicView("epic-001"); err != nil {
		t.Errorf("CmdEpicView: %v", err)
	}
	if err := app.CmdTaskList("", "", "planned", true); err != nil {
		t.Errorf("CmdTaskList (ready): %v", err)
	}
}

func TestE2EClosePlannedTask(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	// Plan a task without starting
	if err := app.CmdTaskNew("planned only", "", ""); err != nil {
		t.Fatal("plan:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	if task.State != StatePlanned {
		t.Fatalf("state = %s, want PLANNED", task.State)
	}

	// Close it without cleanup
	if err := app.CmdWorkClose("task-001", false); err != nil {
		t.Fatal("close:", err)
	}
	task, _ = app.DB.GetTask("task-001")
	if task.State != StateClosed {
		t.Errorf("state = %s, want CLOSED", task.State)
	}
	// No worktree/branch to worry about
	if task.Branch != "" {
		t.Errorf("branch = %q, want empty", task.Branch)
	}
	if task.WorktreePath != "" {
		t.Errorf("worktree = %q, want empty", task.WorktreePath)
	}

	// Close with cleanup should also work (no-op on branch/worktree)
	app2, _ := setupApp(t)
	defer app2.Close()
	if err := app2.CmdTaskNew("planned only 2", "", ""); err != nil {
		t.Fatal("plan2:", err)
	}
	if err := app2.CmdWorkClose("task-001", true); err != nil {
		t.Fatal("close with cleanup:", err)
	}
	task2, _ := app2.DB.GetTask("task-001")
	if task2.State != StateClosed {
		t.Errorf("state = %s, want CLOSED", task2.State)
	}
}

func TestE2EStartBlockedTask(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	// Plan A and B
	if err := app.CmdTaskNew("blocker task", "", ""); err != nil {
		t.Fatal("plan A:", err)
	}
	if err := app.CmdTaskNew("blocked task", "", ""); err != nil {
		t.Fatal("plan B:", err)
	}

	// A blocks B
	if err := app.CmdDepAdd("task-001", "task-002", "blocks"); err != nil {
		t.Fatal("dep:", err)
	}

	// Start B while A is still PLANNED -- should succeed with warning
	if err := app.CmdTaskStart("task-002", ""); err != nil {
		t.Fatal("start blocked task:", err)
	}
	task, _ := app.DB.GetTask("task-002")
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}
	if task.Branch == "" {
		t.Error("branch should be set")
	}
	if task.WorktreePath == "" {
		t.Error("worktree should be set")
	}
}

func TestE2EOrphanPlannedTask(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	// Plan without epic
	if err := app.CmdTaskNew("orphan task", "", ""); err != nil {
		t.Fatal("plan:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	if task.EpicID != "" {
		t.Errorf("epicID = %q, want empty", task.EpicID)
	}
	if task.State != StatePlanned {
		t.Errorf("state = %s, want PLANNED", task.State)
	}

	// Start it
	if err := app.CmdTaskStart("task-001", ""); err != nil {
		t.Fatal("start:", err)
	}
	task, _ = app.DB.GetTask("task-001")
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}

	// Make change and verify full flow works
	os.WriteFile(filepath.Join(task.WorktreePath, "orphan.txt"), []byte("orphan\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "orphan.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "orphan change")

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state = %s, want MERGED", task.State)
	}

	// CmdTaskList should not error (orphan shows in orphans section)
	if err := app.CmdTaskList("", "", "all", false); err != nil {
		t.Errorf("CmdTaskList: %v", err)
	}
}

func TestE2EMixedWorkflow(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	// Old style: CmdNew
	if err := app.CmdNew("old style task", "", "", ""); err != nil {
		t.Fatal("CmdNew:", err)
	}
	oldTask, _ := app.DB.GetTask("task-001")
	if oldTask.State != StateInProgress {
		t.Errorf("old task state = %s, want IN_PROGRESS", oldTask.State)
	}

	// New style: project + epic + plan
	if err := app.CmdProjectNew("Mixed Project", ""); err != nil {
		t.Fatal("project:", err)
	}
	if err := app.CmdEpicNew("Mixed Epic", "", "proj-001", "", false); err != nil {
		t.Fatal("epic:", err)
	}
	if err := app.CmdTaskNew("planned task", "", "epic-001"); err != nil {
		t.Fatal("plan:", err)
	}
	plannedTask, _ := app.DB.GetTask("task-002")
	if plannedTask.State != StatePlanned {
		t.Errorf("planned task state = %s, want PLANNED", plannedTask.State)
	}
	if plannedTask.EpicID != "epic-001" {
		t.Errorf("planned task epicID = %q, want epic-001", plannedTask.EpicID)
	}

	// Both coexist -- list shows both
	tasks, err := app.DB.ListTasks()
	if err != nil {
		t.Fatal("list tasks:", err)
	}
	if len(tasks) != 2 {
		t.Errorf("task count = %d, want 2", len(tasks))
	}

	// CmdTaskList should not error
	if err := app.CmdTaskList("", "", "all", false); err != nil {
		t.Errorf("CmdTaskList: %v", err)
	}
}

// taskIDs extracts IDs from task slice for error messages.
func taskIDs(tasks []Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}

// --- Epic Feature Branch Tests ---

func TestE2EEpicFeatureBranch(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()

	// Create project + epic (auto-creates epic branch)
	if err := app.CmdProjectNew("Feature Project", ""); err != nil {
		t.Fatal("project:", err)
	}
	if err := app.CmdEpicNew("Big Feature", "", "proj-001", "", false); err != nil {
		t.Fatal("epic new:", err)
	}

	epic, _ := app.DB.GetEpic("epic-001")
	if epic.Branch != "epic/big-feature" {
		t.Errorf("epic branch = %q, want %q", epic.Branch, "epic/big-feature")
	}

	// Verify git branch exists
	if _, err := app.Git.RevParse("epic/big-feature"); err != nil {
		t.Fatal("epic branch should exist in git:", err)
	}

	// Create a task under epic (should auto-resolve base from epic branch)
	if err := app.CmdTaskNew("subtask one", "", "epic-001"); err != nil {
		t.Fatal("plan subtask:", err)
	}

	// Start task - should use epic branch as base
	if err := app.CmdTaskStart("task-001", ""); err != nil {
		t.Fatal("start:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	if task.BaseBranch != "epic/big-feature" {
		t.Errorf("task base branch = %q, want %q", task.BaseBranch, "epic/big-feature")
	}

	// Make a change in task worktree
	os.WriteFile(filepath.Join(task.WorktreePath, "sub.txt"), []byte("subtask\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "sub.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add sub.txt")

	// check -> review -> merge (into epic branch)
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdReviewApprove(task.ID, ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdWorkMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("task state = %s, want MERGED", task.State)
	}

	// sub.txt should NOT be on main yet
	mainGit := NewGitClient(dir)
	mainGit.Checkout("main")
	if _, err := os.Stat(filepath.Join(dir, "sub.txt")); !os.IsNotExist(err) {
		t.Error("sub.txt should NOT be on main yet")
	}

	// Now review the epic
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal("epic review request:", err)
	}
	if err := app.CmdReviewApproveDispatch("epic-001", ""); err != nil {
		t.Fatal("epic review approve:", err)
	}

	// Merge epic into main
	if err := app.CmdEpicMerge("epic-001"); err != nil {
		t.Fatal("epic merge:", err)
	}

	// Verify epic is closed and branch cleared
	epic, _ = app.DB.GetEpic("epic-001")
	if epic.Status != "closed" {
		t.Errorf("epic status = %q, want closed", epic.Status)
	}

	// sub.txt should be on main now
	mainGit.Checkout("main")
	if _, err := os.Stat(filepath.Join(dir, "sub.txt")); os.IsNotExist(err) {
		t.Error("sub.txt should exist on main after epic merge")
	}
}

func TestE2EEpicBranchFlag(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}

	// --branch=main -> store main, no new branch
	if err := app.CmdEpicNew("No Branch Epic", "", "proj-001", "main", true); err != nil {
		t.Fatal("epic new:", err)
	}
	epic, _ := app.DB.GetEpic("epic-001")
	if epic.Branch != "main" {
		t.Errorf("epic branch = %q, want %q", epic.Branch, "main")
	}

	// Tasks under this epic should use main as base
	if err := app.CmdTaskNew("task A", "", "epic-001"); err != nil {
		t.Fatal("plan:", err)
	}
	if err := app.CmdTaskStart("task-001", ""); err != nil {
		t.Fatal("start:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	if task.BaseBranch != "main" {
		t.Errorf("task base = %q, want main", task.BaseBranch)
	}
}

func TestE2EEpicDisabledFeatureBranches(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add makefile")

	RunInit(dir)

	// Add epic_feature_branches: false to config
	cfgPath := filepath.Join(dir, ".harness.yaml")
	cfgData, _ := os.ReadFile(cfgPath)
	cfgStr := string(cfgData) + "\nepic_feature_branches: false\n"
	os.WriteFile(cfgPath, []byte(cfgStr), 0644)

	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "add harness config")

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if app.Config.EpicFeatureBranchesEnabled() {
		t.Error("EpicFeatureBranchesEnabled should be false")
	}

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}

	// Epic without --branch and epic_feature_branches=false -> no branch
	if err := app.CmdEpicNew("No Branch Epic", "", "proj-001", "", false); err != nil {
		t.Fatal("epic new:", err)
	}
	epic, _ := app.DB.GetEpic("epic-001")
	if epic.Branch != "" {
		t.Errorf("epic branch = %q, want empty", epic.Branch)
	}

	// Tasks under this epic should use main as base
	if err := app.CmdTaskNew("task A", "", "epic-001"); err != nil {
		t.Fatal("plan:", err)
	}
	if err := app.CmdTaskStart("task-001", ""); err != nil {
		t.Fatal("start:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	if task.BaseBranch != "main" {
		t.Errorf("task base = %q, want main", task.BaseBranch)
	}
}

func TestE2EEpicReviewHistory(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}
	if err := app.CmdEpicNew("Review Epic", "", "proj-001", "", false); err != nil {
		t.Fatal("epic new:", err)
	}

	// Request review
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal("request:", err)
	}

	// Reject
	if err := app.CmdReviewRejectDispatch("epic-001", "needs work"); err != nil {
		t.Fatal("reject:", err)
	}

	// Request again
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal("request 2:", err)
	}

	// Approve
	if err := app.CmdReviewApproveDispatch("epic-001", ""); err != nil {
		t.Fatal("approve:", err)
	}

	// History should have 2 reviews
	reviews, err := app.DB.GetEpicReviews("epic-001")
	if err != nil {
		t.Fatal("get reviews:", err)
	}
	if len(reviews) != 2 {
		t.Errorf("review count = %d, want 2", len(reviews))
	}
	if reviews[0].Decision != "request_changes" {
		t.Errorf("review[0] = %q, want request_changes", reviews[0].Decision)
	}
	if reviews[1].Decision != "approve" {
		t.Errorf("review[1] = %q, want approve", reviews[1].Decision)
	}

	// CmdReviewHistoryDispatch should not error
	if err := app.CmdReviewHistoryDispatch("epic-001"); err != nil {
		t.Fatal("history:", err)
	}
}

func TestE2EEpicMergeNoApproval(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}
	if err := app.CmdEpicNew("Merge Epic", "", "proj-001", "", false); err != nil {
		t.Fatal("epic new:", err)
	}

	// Merge without approval -> error
	err := app.CmdEpicMerge("epic-001")
	if err == nil {
		t.Error("expected error merging epic without approval")
	}
	if err != nil && !strings.Contains(err.Error(), "no approval") {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestE2EEpicMergeNoBranch(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}

	// Create epic with epic_feature_branches default but --branch="" explicitly means use default behavior
	// Actually to test no branch, we need to use --branch=main which means no merge needed,
	// or use epic_feature_branches=false. Let's use the DB directly for a clean test.
	app.DB.CreateEpic("No Branch", "", "proj-001", "")

	err := app.CmdEpicMerge("epic-001")
	if err == nil {
		t.Error("expected error merging epic with no branch")
	}
	if err != nil && !strings.Contains(err.Error(), "no branch") {
		t.Errorf("unexpected error: %s", err)
	}
}

func TestE2EEpicNewWithCmdNew(t *testing.T) {
	t.Parallel()
	// Test that CmdNew (harness new) resolves epic branch
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}
	if err := app.CmdEpicNew("Feature", "", "proj-001", "", false); err != nil {
		t.Fatal("epic:", err)
	}

	epic, _ := app.DB.GetEpic("epic-001")
	if epic.Branch == "" {
		t.Fatal("epic should have branch")
	}

	// CmdNew with --epic should resolve to epic branch
	if err := app.CmdNew("subtask via new", "", "epic-001", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	if task.BaseBranch != epic.Branch {
		t.Errorf("task base = %q, want %q", task.BaseBranch, epic.Branch)
	}
}

func TestE2EEpicDiff(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}
	if err := app.CmdEpicNew("Diff Epic", "", "proj-001", "", false); err != nil {
		t.Fatal("epic:", err)
	}

	// CmdDiffDispatch with epic ID should not error
	if err := app.CmdDiffDispatch("epic-001"); err != nil {
		t.Fatal("epic diff:", err)
	}
}

func TestE2EEpicBranchWithExistingBranch(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}

	// Create a branch manually
	if err := app.Git.CreateBranch("custom-epic", "main"); err != nil {
		t.Fatal("create branch:", err)
	}

	// --branch=custom-epic -> use existing branch
	if err := app.CmdEpicNew("Custom", "", "proj-001", "custom-epic", true); err != nil {
		t.Fatal("epic new:", err)
	}

	epic, _ := app.DB.GetEpic("epic-001")
	if epic.Branch != "custom-epic" {
		t.Errorf("epic branch = %q, want custom-epic", epic.Branch)
	}
}

func TestE2EEpicBranchNonexistent(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()

	if err := app.CmdProjectNew("Test Project", ""); err != nil {
		t.Fatal("project:", err)
	}

	// --branch=nonexistent -> error
	err := app.CmdEpicNew("Bad", "", "proj-001", "nonexistent-branch", true)
	if err == nil {
		t.Error("expected error for nonexistent branch")
	}
}

// --- Automated Reviewer Tests ---

// buildHarnessBinary builds the harness binary for use in automated reviewer tests.
func buildHarnessBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "harness")
	cmd := exec.Command("go", "build", "-o", bin, "..")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build harness binary: %s\n%s", err, out)
	}
	return bin
}

// setupAppWithReviewCmd creates a test repo with a configured review.cmd.
func setupAppWithReviewCmd(t *testing.T, reviewCmd string) (*App, string) {
	t.Helper()
	dir := initTestRepo(t)
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, dir, "add", "Makefile")
	gitRun(t, dir, "commit", "-m", "add makefile")

	RunInit(dir)

	// Rewrite config with review.cmd
	cfgPath := filepath.Join(dir, ".harness.yaml")
	cfgContent := fmt.Sprintf(`
main_branch: main
worktree_dir: .worktrees
checks:
  test:
    run: make test
merge:
  require:
    checks: [test]
    review: approved
  strategy: merge
review:
  type: agent
  cmd: '%s'
  prompt: |
    Review this diff:
    {{diff}}
    Task: {{task_description}}
    Previous: {{previous_comments}}
`, reviewCmd)
	os.WriteFile(cfgPath, []byte(cfgContent), 0644)

	gitRun(t, dir, "add", ".harness.yaml", ".gitignore")
	gitRun(t, dir, "commit", "-m", "add harness config")

	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	return app, dir
}

func TestE2EAutomatedReviewerApprove(t *testing.T) {
	t.Parallel()
	bin := buildHarnessBinary(t)

	// The reviewer cmd reads the prompt file and then approves
	reviewCmd := fmt.Sprintf(
		`cat {{prompt_file}} > /dev/null && %s review approve {{task_id}}`,
		bin,
	)

	app, _ := setupAppWithReviewCmd(t, reviewCmd)
	defer app.Close()

	if err := app.CmdNew("auto-reviewed feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	os.WriteFile(filepath.Join(task.WorktreePath, "feature.txt"), []byte("feature\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "feature.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add feature")

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}

	// CmdReviewRequest should launch the reviewer and auto-approve
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}

	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state after auto-review = %s, want APPROVED", task.State)
	}

	// Should have a review record
	reviews, _ := app.DB.GetReviews(task.ID)
	if len(reviews) != 1 {
		t.Errorf("review count = %d, want 1", len(reviews))
	}
	if len(reviews) > 0 && reviews[0].Decision != "approve" {
		t.Errorf("review decision = %q, want approve", reviews[0].Decision)
	}
}

func TestE2EAutomatedReviewerReject(t *testing.T) {
	t.Parallel()
	bin := buildHarnessBinary(t)

	reviewCmd := fmt.Sprintf(
		`%s review reject --comment "needs tests" {{task_id}}`,
		bin,
	)

	app, _ := setupAppWithReviewCmd(t, reviewCmd)
	defer app.Close()

	if err := app.CmdNew("rejected feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	os.WriteFile(filepath.Join(task.WorktreePath, "feature.txt"), []byte("feature\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "feature.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add feature")

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}

	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}

	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateRequestChanges {
		t.Errorf("state after rejection = %s, want REQUEST_CHANGES", task.State)
	}

	reviews, _ := app.DB.GetReviews(task.ID)
	if len(reviews) != 1 {
		t.Errorf("review count = %d, want 1", len(reviews))
	}
	if len(reviews) > 0 && reviews[0].Comment != "needs tests" {
		t.Errorf("review comment = %q, want %q", reviews[0].Comment, "needs tests")
	}
}

func TestE2EAutomatedReviewerNoVerdict(t *testing.T) {
	t.Parallel()
	// Reviewer cmd exits without calling harness
	app, _ := setupAppWithReviewCmd(t, "echo noop")
	defer app.Close()

	if err := app.CmdNew("no-verdict feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	os.WriteFile(filepath.Join(task.WorktreePath, "feature.txt"), []byte("feature\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "feature.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add feature")

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}

	// Should not error even though reviewer gave no verdict
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}

	// State should still be REVIEW_REQUESTED
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", task.State)
	}
}

func TestE2EAutomatedReviewerPromptFile(t *testing.T) {
	t.Parallel()
	// Verify the prompt file contains the expected template-substituted content
	outputFile := filepath.Join(t.TempDir(), "prompt-output.txt")
	reviewCmd := fmt.Sprintf(`cp {{prompt_file}} %s`, outputFile)

	app, _ := setupAppWithReviewCmd(t, reviewCmd)
	defer app.Close()

	if err := app.CmdNew("prompt-test feature", "", "", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	os.WriteFile(filepath.Join(task.WorktreePath, "hello.txt"), []byte("hello\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "hello.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add hello")

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}

	// Read the captured prompt
	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatal("read prompt output:", err)
	}
	prompt := string(data)

	// Should contain task description (template var {{task_description}})
	if !strings.Contains(prompt, "prompt-test feature") {
		t.Error("prompt should contain task description")
	}

	// Should contain diff content (template var {{diff}})
	if !strings.Contains(prompt, "hello.txt") {
		t.Error("prompt should contain diff (hello.txt)")
	}

	// Should contain the review instructions from the prompt template
	if !strings.Contains(prompt, "Review this diff") {
		t.Error("prompt should contain review instructions")
	}
}

func TestE2EFallbackNoCmd(t *testing.T) {
	t.Parallel()
	// When review.cmd is empty, should fall back to printing instructions
	app, _, task := setupTask(t)
	defer app.Close()

	// Default config from RunInit has no review.cmd
	if app.Config.Review.Cmd != "" {
		t.Fatalf("default config should have empty review.cmd, got %q", app.Config.Review.Cmd)
	}

	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	// Should not error (fallback to manual instructions)
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}

	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", task.State)
	}
}
