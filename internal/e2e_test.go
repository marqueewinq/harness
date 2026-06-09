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
	err = app.CmdNew("add hello feature", "")
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
	err = app.CmdCheck(task.ID)
	if err != nil {
		t.Fatal("check:", err)
	}
	task2, _ := app.DB.GetTask(task.ID)
	if task2.State != StateChecksPassed {
		t.Errorf("state after check = %s, want CHECKS_PASSED", task2.State)
	}

	// harness request-review
	err = app.CmdRequestReview(task.ID)
	if err != nil {
		t.Fatal("request-review:", err)
	}
	task3, _ := app.DB.GetTask(task.ID)
	if task3.State != StateReviewRequested {
		t.Errorf("state after request-review = %s, want REVIEW_REQUESTED", task3.State)
	}

	// harness record-review approve
	err = app.CmdRecordReview(task.ID, "approve", "")
	if err != nil {
		t.Fatal("record-review:", err)
	}
	task4, _ := app.DB.GetTask(task.ID)
	if task4.State != StateApproved {
		t.Errorf("state after approve = %s, want APPROVED", task4.State)
	}

	// harness merge
	err = app.CmdMerge(task.ID)
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

	err = app.CmdNew("feature to close", "")
	if err != nil {
		t.Fatal("new:", err)
	}

	task, _ := app.DB.GetTask("task-001")

	// Make a commit so branch diverges
	os.WriteFile(filepath.Join(task.WorktreePath, "wip.txt"), []byte("wip\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "wip.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "wip")

	// Close without cleanup
	err = app.CmdClose(task.ID, false)
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

	err = app.CmdNew("feature to close with cleanup", "")
	if err != nil {
		t.Fatal("new:", err)
	}

	task, _ := app.DB.GetTask("task-001")
	wtPath := task.WorktreePath
	branch := task.Branch

	// Make a commit
	os.WriteFile(filepath.Join(wtPath, "wip.txt"), []byte("wip\n"), 0644)
	gitRun(t, wtPath, "add", "wip.txt")
	gitRun(t, wtPath, "commit", "-m", "wip")

	// Close with cleanup
	err = app.CmdClose(task.ID, true)
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
	err = app.CmdNew("big feature", "")
	if err != nil {
		t.Fatal("new parent:", err)
	}
	parent, _ := app.DB.GetTask("task-001")
	if parent.BaseBranch != "main" {
		t.Errorf("parent BaseBranch = %q, want main", parent.BaseBranch)
	}

	// Create subtask targeting parent's branch
	err = app.CmdNew("subtask one", parent.Branch)
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
	app.CmdCheck(subtask.ID)
	app.CmdRequestReview(subtask.ID)
	app.CmdRecordReview(subtask.ID, "approve", "")
	err = app.CmdMerge(subtask.ID)
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
	app.CmdCheck(parent.ID)
	app.CmdRequestReview(parent.ID)
	app.CmdRecordReview(parent.ID, "approve", "")
	err = app.CmdMerge(parent.ID)
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

// setupApp creates temp repo with passing Makefile, inits harness, returns app.
// Caller must defer app.Close().
func setupApp(t *testing.T) (*App, string) {
	t.Helper()
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
	return app, dir
}

// setupTask creates app + new task with a commit in worktree. Returns app, dir, task.
func setupTask(t *testing.T) (*App, string, *Task) {
	t.Helper()
	app, dir := setupApp(t)
	if err := app.CmdNew("test feature", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")
	// Add a commit so checks can run
	os.WriteFile(filepath.Join(task.WorktreePath, "feature.txt"), []byte("feature\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "feature.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add feature")
	return app, dir, task
}

func TestCmdCheckWrongState(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Move to CHECKS_PASSED
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("first check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Fatalf("expected CHECKS_PASSED, got %s", task.State)
	}

	// CmdCheck in CHECKS_PASSED -> error
	err := app.CmdCheck(task.ID)
	if err == nil {
		t.Error("expected error running check in CHECKS_PASSED state")
	}

	// Move to APPROVED
	app.CmdRequestReview(task.ID)
	app.CmdRecordReview(task.ID, "approve", "")
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Fatalf("expected APPROVED, got %s", task.State)
	}

	// CmdCheck in APPROVED -> error
	err = app.CmdCheck(task.ID)
	if err == nil {
		t.Error("expected error running check in APPROVED state")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot run checks") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdRequestReviewWrongState(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Task is IN_PROGRESS -> CmdRequestReview should fail
	err := app.CmdRequestReview(task.ID)
	if err == nil {
		t.Error("expected error requesting review in IN_PROGRESS state")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot request review") {
		t.Errorf("unexpected error message: %s", err)
	}

	// Move to CHECKS_FAILED: write failing Makefile in worktree, then check
	os.WriteFile(filepath.Join(task.WorktreePath, "Makefile"), []byte("test:\n\t@exit 1\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "Makefile")
	gitRun(t, task.WorktreePath, "commit", "-m", "break tests")
	app.CmdCheck(task.ID)
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksFailed {
		t.Fatalf("expected CHECKS_FAILED, got %s", task.State)
	}

	// CHECKS_FAILED -> CmdRequestReview should fail
	err = app.CmdRequestReview(task.ID)
	if err == nil {
		t.Error("expected error requesting review in CHECKS_FAILED state")
	}
}

func TestCmdRecordReviewWrongState(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Task is IN_PROGRESS -> CmdRecordReview should fail
	err := app.CmdRecordReview(task.ID, "approve", "")
	if err == nil {
		t.Error("expected error recording review in IN_PROGRESS state")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot record review") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdRecordReviewMissingComment(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Move to REVIEW_REQUESTED
	app.CmdCheck(task.ID)
	app.CmdRequestReview(task.ID)
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Fatalf("expected REVIEW_REQUESTED, got %s", task.State)
	}

	// request-changes without comment -> error
	err := app.CmdRecordReview(task.ID, "request-changes", "")
	if err == nil {
		t.Error("expected error for request-changes without comment")
	}
	if err != nil && !strings.Contains(err.Error(), "--comment is required") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdRecordReviewUnknownDecision(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Move to REVIEW_REQUESTED
	app.CmdCheck(task.ID)
	app.CmdRequestReview(task.ID)
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Fatalf("expected REVIEW_REQUESTED, got %s", task.State)
	}

	// Unknown decision -> error
	err := app.CmdRecordReview(task.ID, "invalid-decision", "")
	if err == nil {
		t.Error("expected error for unknown decision")
	}
	if err != nil && !strings.Contains(err.Error(), "unknown decision") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdMergeWrongState(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Task is IN_PROGRESS -> CmdMerge should fail
	err := app.CmdMerge(task.ID)
	if err == nil {
		t.Error("expected error merging in IN_PROGRESS state")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot merge") {
		t.Errorf("unexpected error message: %s", err)
	}
}

func TestCmdInitAlreadyInitialized(t *testing.T) {
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
	app, _, task := setupTask(t)
	defer app.Close()

	// Full workflow to MERGED
	app.CmdCheck(task.ID)
	app.CmdRequestReview(task.ID)
	app.CmdRecordReview(task.ID, "approve", "")
	if err := app.CmdMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Fatalf("expected MERGED, got %s", task.State)
	}

	// CmdClose on MERGED task -> error (terminal state, no transitions)
	err := app.CmdClose(task.ID, false)
	if err == nil {
		t.Error("expected error closing already-merged task")
	}
	if err != nil && !strings.Contains(err.Error(), "cannot close") {
		t.Errorf("unexpected error message: %s", err)
	}
}

// --- E2E Workflow Tests ---

func TestE2ERejectionCycle(t *testing.T) {
	app, dir, task := setupTask(t)
	defer app.Close()

	// check -> request-review -> request-changes
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "request-changes", "needs more tests"); err != nil {
		t.Fatal("record-review request-changes:", err)
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
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("second check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state after second check = %s, want CHECKS_PASSED", task.State)
	}

	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("second request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
		t.Fatal("approve:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state after approve = %s, want APPROVED", task.State)
	}

	if err := app.CmdMerge(task.ID); err != nil {
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

	if err := app.CmdNew("fix tests", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	// Add a commit
	os.WriteFile(filepath.Join(task.WorktreePath, "code.txt"), []byte("code\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "code.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add code")

	// Check -> should fail (Makefile exits 1)
	app.CmdCheck(task.ID)
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksFailed {
		t.Errorf("state after failing check = %s, want CHECKS_FAILED", task.State)
	}

	// Fix Makefile in worktree
	os.WriteFile(filepath.Join(task.WorktreePath, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "Makefile")
	gitRun(t, task.WorktreePath, "commit", "-m", "fix makefile")

	// Check again -> should pass
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("second check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state after fix = %s, want CHECKS_PASSED", task.State)
	}

	// Continue: request-review -> approve -> merge
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdMerge(task.ID); err != nil {
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

	if err := app.CmdNew("squash feature", ""); err != nil {
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
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdMerge(task.ID); err != nil {
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
	app, _, task := setupTask(t)
	defer app.Close()

	// check -> request-review -> approve
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
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

	// CmdMerge should NOT error, but task goes to IN_PROGRESS (stale review)
	err := app.CmdMerge(task.ID)
	if err != nil {
		t.Fatal("merge should not error on stale review:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateInProgress {
		t.Errorf("state after stale merge = %s, want IN_PROGRESS", task.State)
	}

	// Re-do: check -> request-review -> approve -> merge
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("re-check:", err)
	}
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("re-request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
		t.Fatal("re-approve:", err)
	}
	if err := app.CmdMerge(task.ID); err != nil {
		t.Fatal("re-merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state after re-merge = %s, want MERGED", task.State)
	}
}

func TestE2EMergeWithUnchangedReview(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()

	// Normal flow: check -> request-review -> approve -> merge (no new commits)
	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state = %s, want MERGED", task.State)
	}
}

func TestE2ECustomReviewType(t *testing.T) {
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

	// Full workflow should still work with agent review type
	if err := app.CmdNew("agent reviewed feature", ""); err != nil {
		t.Fatal("new:", err)
	}
	task, _ := app.DB.GetTask("task-001")

	os.WriteFile(filepath.Join(task.WorktreePath, "agent.txt"), []byte("agent\n"), 0644)
	gitRun(t, task.WorktreePath, "add", "agent.txt")
	gitRun(t, task.WorktreePath, "commit", "-m", "add agent file")

	if err := app.CmdCheck(task.ID); err != nil {
		t.Fatal("check:", err)
	}
	if err := app.CmdRequestReview(task.ID); err != nil {
		t.Fatal("request-review:", err)
	}
	if err := app.CmdRecordReview(task.ID, "approve", ""); err != nil {
		t.Fatal("approve:", err)
	}
	if err := app.CmdMerge(task.ID); err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state = %s, want MERGED", task.State)
	}
}
