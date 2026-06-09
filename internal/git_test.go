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

func TestDeleteBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	git.CreateBranch("to-delete", "main")

	// Verify branch exists
	out, _ := git.run("branch", "--list", "to-delete")
	if !strings.Contains(out, "to-delete") {
		t.Fatal("branch should exist before delete")
	}

	err := git.DeleteBranch("to-delete")
	if err != nil {
		t.Fatal(err)
	}

	out, _ = git.run("branch", "--list", "to-delete")
	if strings.Contains(out, "to-delete") {
		t.Error("branch should be gone after delete")
	}
}

func TestCheckout(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	git.CreateBranch("other", "main")
	err := git.Checkout("other")
	if err != nil {
		t.Fatal(err)
	}

	branch, err := git.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "other" {
		t.Errorf("CurrentBranch after checkout = %q, want %q", branch, "other")
	}
}

func TestRevParse(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	sha, err := git.RevParse("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) < 40 {
		t.Errorf("RevParse(HEAD) = %q, want 40-char SHA", sha)
	}
}

func TestRevParseShort(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	sha, err := git.RevParseShort("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) == 0 || len(sha) >= 40 {
		t.Errorf("RevParseShort(HEAD) = %q, want short SHA", sha)
	}
}

func TestDefaultBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	branch, err := git.DefaultBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("DefaultBranch() = %q, want %q", branch, "main")
	}
}

func TestMergeSquash(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "squash-test")
	git.CreateBranch("squash-test", "main")
	git.CreateWorktree("squash-test", wtPath)

	wtGit := NewGitClient(wtPath)

	// Two commits on feature branch
	os.WriteFile(filepath.Join(wtPath, "x.txt"), []byte("x\n"), 0644)
	wtGit.run("add", "x.txt")
	wtGit.run("commit", "-m", "add x")

	os.WriteFile(filepath.Join(wtPath, "y.txt"), []byte("y\n"), 0644)
	wtGit.run("add", "y.txt")
	wtGit.run("commit", "-m", "add y")

	err := git.MergeSquash("squash-test", "squashed commit")
	if err != nil {
		t.Fatal(err)
	}

	// Verify single squash commit on main (init + squash = 2)
	out, err := git.run("rev-list", "--count", "main")
	if err != nil {
		t.Fatal(err)
	}
	if out != "2" {
		t.Errorf("commit count on main = %s, want 2 (init + squash)", out)
	}

	// Verify both files exist
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); os.IsNotExist(err) {
		t.Error("x.txt should exist on main")
	}
	if _, err := os.Stat(filepath.Join(dir, "y.txt")); os.IsNotExist(err) {
		t.Error("y.txt should exist on main")
	}
}

func TestRebase(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "rebase-test")
	git.CreateBranch("rebase-test", "main")
	git.CreateWorktree("rebase-test", wtPath)

	// Diverge: commit on main
	os.WriteFile(filepath.Join(dir, "main-only.txt"), []byte("main\n"), 0644)
	git.run("add", "main-only.txt")
	git.run("commit", "-m", "main diverge")

	// Commit on feature branch
	wtGit := NewGitClient(wtPath)
	os.WriteFile(filepath.Join(wtPath, "feature-only.txt"), []byte("feat\n"), 0644)
	wtGit.run("add", "feature-only.txt")
	wtGit.run("commit", "-m", "feature commit")

	// Rebase feature onto main
	err := wtGit.Rebase("main")
	if err != nil {
		t.Fatal(err)
	}

	// After rebase, feature worktree should have main-only.txt
	if _, err := os.Stat(filepath.Join(wtPath, "main-only.txt")); os.IsNotExist(err) {
		t.Error("main-only.txt should exist in worktree after rebase")
	}

	// Verify linear history: feature should be directly ahead of main
	out, err := git.run("log", "--oneline", "main..rebase-test")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 commit ahead of main, got %d: %q", len(lines), out)
	}
}

func TestDiffEmpty(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	diff, err := git.Diff("main", "main")
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Errorf("Diff(main, main) = %q, want empty", diff)
	}
}
