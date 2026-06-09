package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
