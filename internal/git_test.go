package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "checkout", "-b", "main"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %s\n%s", args, err, out)
		}
	}
	return dir
}

func TestCreateBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	err := git.CreateBranch("feature-x", "main")
	if err != nil {
		t.Fatal(err)
	}

	out, err := git.run("branch", "--list", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "feature-x") {
		t.Errorf("branch not created: %q", out)
	}
}

func TestCreateWorktree(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	err := git.CreateBranch("feature-x", "main")
	if err != nil {
		t.Fatal(err)
	}
	err = git.CreateWorktree("feature-x", wtPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory not created")
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	err := git.RemoveWorktree(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed")
	}
}

func TestDiff(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	os.WriteFile(filepath.Join(wtPath, "hello.txt"), []byte("hello\n"), 0644)
	wtGit := NewGitClient(wtPath)
	wtGit.run("add", "hello.txt")
	wtGit.run("commit", "-m", "add hello")

	diff, err := git.Diff("main", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "hello.txt") {
		t.Errorf("diff should mention hello.txt: %q", diff)
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	branch, err := git.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want %q", branch, "main")
	}
}

func TestMergeFastForward(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	os.WriteFile(filepath.Join(wtPath, "hello.txt"), []byte("hello\n"), 0644)
	wtGit := NewGitClient(wtPath)
	wtGit.run("add", "hello.txt")
	wtGit.run("commit", "-m", "add hello")

	err := git.MergeFF("feature-x")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); os.IsNotExist(err) {
		t.Error("merged file should exist on main")
	}
}
