package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepoTemplate is a bare git repo (main branch, one empty commit).
// testAppRepoTemplate is a harness-initialized repo (Makefile + .harness.yaml committed, no DB).
// testDBTemplate is a pre-migrated empty harness.db file.
// All created once in TestMain, copied per-test.
var testRepoTemplate string
var testAppRepoTemplate string
var testDBTemplate string

func TestMain(m *testing.M) {
	// The developer's global git config (gpg signing, hooks) must not leak
	// into test repos.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	gitRun := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "git %s failed: %v\n%s", strings.Join(args, " "), err, out)
			os.Exit(1)
		}
	}

	// Template 1: bare git repo
	dir, err := os.MkdirTemp("", "harness-test-template-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating template dir: %v\n", err)
		os.Exit(1)
	}
	gitRun(dir, "init")
	gitRun(dir, "checkout", "-b", "main")
	gitRun(dir, "commit", "--allow-empty", "-m", "init")
	testRepoTemplate = dir

	// Template 2: harness-initialized repo (Makefile + config, no DB)
	appDir, err := os.MkdirTemp("", "harness-test-app-template-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating app template dir: %v\n", err)
		os.Exit(1)
	}
	if err := copyDir(dir, appDir); err != nil {
		fmt.Fprintf(os.Stderr, "copying base template: %v\n", err)
		os.Exit(1)
	}
	os.WriteFile(filepath.Join(appDir, "Makefile"), []byte("test:\n\t@echo pass\n"), 0644)
	gitRun(appDir, "add", "Makefile")
	gitRun(appDir, "commit", "-m", "add makefile")
	if err := RunInit(appDir); err != nil {
		fmt.Fprintf(os.Stderr, "RunInit: %v\n", err)
		os.Exit(1)
	}
	gitRun(appDir, "add", ".harness.yaml", ".gitignore")
	gitRun(appDir, "commit", "-m", "add harness config")
	// Remove .harness/ (contains DB) so each test gets a fresh one via LoadApp
	os.RemoveAll(filepath.Join(appDir, ".harness"))
	testAppRepoTemplate = appDir

	// Template 3: pre-migrated empty DB file
	dbDir, err := os.MkdirTemp("", "harness-test-db-template-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating db template dir: %v\n", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(dbDir, "harness.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating template DB: %v\n", err)
		os.Exit(1)
	}
	db.Close()
	testDBTemplate = dbPath

	code := m.Run()

	os.RemoveAll(dir)
	os.RemoveAll(appDir)
	os.RemoveAll(dbDir)
	os.Exit(code)
}

// openTestDB copies the pre-migrated template DB to a temp dir and opens it.
// Skips all 6 migrations since the schema is already up to date.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dst := filepath.Join(dir, "harness.db")
	data, err := os.ReadFile(testDBTemplate)
	if err != nil {
		t.Fatalf("reading DB template: %v", err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("writing DB copy: %v", err)
	}
	db, err := OpenDB(dst)
	if err != nil {
		t.Fatalf("opening DB: %v", err)
	}
	return db
}

// gitCommitFile is a shortcut: writes file, stages, commits in one subprocess.
func gitCommitFile(t *testing.T, dir, filename, content, message string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
	cmd := exec.Command("sh", "-c", fmt.Sprintf("git add %s && git commit -m %q", filename, message))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gitCommitFile(%s): %v\n%s", filename, err, out)
	}
}

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := copyDir(testRepoTemplate, dir); err != nil {
		t.Fatalf("copying template repo: %v", err)
	}
	return dir
}

// copyDir recursively copies src to dst (which must already exist).
func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func TestCreateBranch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	gitCommitFile(t, wtPath, "hello.txt", "hello\n", "add hello")

	diff, err := git.Diff("main", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "hello.txt") {
		t.Errorf("diff should mention hello.txt: %q", diff)
	}
}

func TestCurrentBranch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	gitCommitFile(t, wtPath, "hello.txt", "hello\n", "add hello")

	err := git.MergeFF("feature-x")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); os.IsNotExist(err) {
		t.Error("merged file should exist on main")
	}
}

func TestDeleteBranch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "squash-test")
	git.CreateBranch("squash-test", "main")
	git.CreateWorktree("squash-test", wtPath)

	// Two commits on feature branch
	gitCommitFile(t, wtPath, "x.txt", "x\n", "add x")
	gitCommitFile(t, wtPath, "y.txt", "y\n", "add y")

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
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "rebase-test")
	git.CreateBranch("rebase-test", "main")
	git.CreateWorktree("rebase-test", wtPath)

	// Diverge: commit on main
	gitCommitFile(t, dir, "main-only.txt", "main\n", "main diverge")

	// Commit on feature branch
	wtGit := NewGitClient(wtPath)
	gitCommitFile(t, wtPath, "feature-only.txt", "feat\n", "feature commit")

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

func TestCommit(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("test\n"), 0644)
	git.run("add", "test.txt")

	err := git.Commit("test commit")
	if err != nil {
		t.Fatal(err)
	}

	out, err := git.run("log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "test commit") {
		t.Errorf("commit message = %q, want to contain %q", out, "test commit")
	}
}

func TestDiffEmpty(t *testing.T) {
	t.Parallel()
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
