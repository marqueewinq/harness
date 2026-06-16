package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewMergeStrategyMerge(t *testing.T) {
	t.Parallel()
	s, err := NewMergeStrategy("merge")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "rebase+ff" {
		t.Errorf("Name() = %q, want %q", s.Name(), "rebase+ff")
	}
}

func TestNewMergeStrategyEmpty(t *testing.T) {
	t.Parallel()
	s, err := NewMergeStrategy("")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "rebase+ff" {
		t.Errorf("Name() = %q, want %q", s.Name(), "rebase+ff")
	}
}

func TestNewMergeStrategySquash(t *testing.T) {
	t.Parallel()
	s, err := NewMergeStrategy("squash")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "squash" {
		t.Errorf("Name() = %q, want %q", s.Name(), "squash")
	}
}

func TestNewMergeStrategyUnknown(t *testing.T) {
	t.Parallel()
	_, err := NewMergeStrategy("unknown")
	if err == nil {
		t.Error("expected error for unknown strategy")
	}
}

func TestSquashMerge(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	// Create feature branch + worktree
	err := git.CreateBranch("feature-squash", "main")
	if err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(dir, ".worktrees", "feature-squash")
	err = git.CreateWorktree("feature-squash", wtPath)
	if err != nil {
		t.Fatal(err)
	}

	wtGit := NewGitClient(wtPath)

	// Make 2 commits on feature branch
	gitCommitFile(t, wtPath, "a.txt", "a\n", "add a")
	gitCommitFile(t, wtPath, "b.txt", "b\n", "add b")

	strategy := &SquashMergeStrategy{}

	// Prepare = noop for squash
	err = strategy.Prepare(wtGit, "main")
	if err != nil {
		t.Fatal("Prepare:", err)
	}

	// Merge (squash into main)
	err = strategy.Merge(git, "feature-squash", "main", "squashed feature")
	if err != nil {
		t.Fatal("Merge:", err)
	}

	// Verify files exist on main
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); os.IsNotExist(err) {
		t.Error("a.txt should exist on main after squash merge")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); os.IsNotExist(err) {
		t.Error("b.txt should exist on main after squash merge")
	}

	// Verify single squash commit (init + squash = 2 commits on main)
	out, err := git.run("rev-list", "--count", "main")
	if err != nil {
		t.Fatal(err)
	}
	if out != "2" {
		t.Errorf("expected 2 commits on main (init + squash), got %s", out)
	}
}

func TestRebasePrepare(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	// Create feature branch + worktree
	err := git.CreateBranch("feature-rebase", "main")
	if err != nil {
		t.Fatal(err)
	}
	wtPath := filepath.Join(dir, ".worktrees", "feature-rebase")
	err = git.CreateWorktree("feature-rebase", wtPath)
	if err != nil {
		t.Fatal(err)
	}

	// Make a commit on main (so branches diverge)
	gitCommitFile(t, dir, "main-file.txt", "main\n", "main commit")

	// Make a commit on feature branch
	wtGit := NewGitClient(wtPath)
	gitCommitFile(t, wtPath, "feature-file.txt", "feature\n", "feature commit")

	strategy := &RebaseMergeStrategy{}

	// Prepare = rebase onto main
	err = strategy.Prepare(wtGit, "main")
	if err != nil {
		t.Fatal("Prepare (rebase):", err)
	}

	// Verify feature branch now has main-file.txt (rebased on top of main)
	if _, err := os.Stat(filepath.Join(wtPath, "main-file.txt")); os.IsNotExist(err) {
		t.Error("main-file.txt should exist in worktree after rebase")
	}

	// MergeFF should now work since feature is ahead of main
	err = strategy.Merge(git, "feature-rebase", "main", "")
	if err != nil {
		t.Fatal("Merge (ff):", err)
	}

	// Both files on main
	if _, err := os.Stat(filepath.Join(dir, "feature-file.txt")); os.IsNotExist(err) {
		t.Error("feature-file.txt should exist on main after ff merge")
	}
}
