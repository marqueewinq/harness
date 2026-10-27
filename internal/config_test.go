package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
main_branch: main
worktree_dir: .worktrees
checks:
  test:
    run: make test
  lint:
    run: make lint
    needs: [test]
merge:
  require:
    checks: [lint]
  strategy: squash
  rerun_checks: true
review:
  type: agent
  prompt: "Review this."
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MainBranch != "main" {
		t.Errorf("MainBranch = %q, want %q", cfg.MainBranch, "main")
	}
	if cfg.WorktreeDir != ".worktrees" {
		t.Errorf("WorktreeDir = %q, want %q", cfg.WorktreeDir, ".worktrees")
	}
	if len(cfg.Checks) != 2 {
		t.Errorf("len(Checks) = %d, want 2", len(cfg.Checks))
	}
	if cfg.Checks["lint"].Run != "make lint" {
		t.Errorf("Checks[lint].Run = %q", cfg.Checks["lint"].Run)
	}
	if len(cfg.Checks["lint"].Needs) != 1 || cfg.Checks["lint"].Needs[0] != "test" {
		t.Errorf("Checks[lint].Needs = %v", cfg.Checks["lint"].Needs)
	}
	if cfg.Merge.Strategy != "squash" {
		t.Errorf("Merge.Strategy = %q, want %q", cfg.Merge.Strategy, "squash")
	}
	if !cfg.Merge.RerunChecks {
		t.Error("Merge.RerunChecks should be true")
	}
	if cfg.Review.Type != "agent" {
		t.Errorf("Review.Type = %q, want %q", cfg.Review.Type, "agent")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
checks:
  test:
    run: make test
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// MainBranch is resolved at LoadApp time, not LoadConfig time
	if cfg.MainBranch != "" {
		t.Errorf("MainBranch default = %q, want empty (resolved later)", cfg.MainBranch)
	}
	if cfg.WorktreeDir != ".worktrees" {
		t.Errorf("WorktreeDir default = %q, want %q", cfg.WorktreeDir, ".worktrees")
	}
	if cfg.Merge.Strategy != "merge" {
		t.Errorf("Merge.Strategy default = %q, want %q", cfg.Merge.Strategy, "merge")
	}
}

func TestLoadConfigMissing(t *testing.T) {
	t.Parallel()
	_, err := LoadConfig("/nonexistent/.harness.yaml")
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte("{{invalid yaml::"), 0644)

	_, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadConfigEmptyChecks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
main_branch: main
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Checks) != 0 {
		t.Errorf("len(Checks) = %d, want 0", len(cfg.Checks))
	}
}

func TestLoadConfigReviewType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
checks:
  test:
    run: echo ok
review:
  type: human
  prompt: "Please review"
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Review.Type != "human" {
		t.Errorf("Review.Type = %q, want %q", cfg.Review.Type, "human")
	}
	if cfg.Review.Prompt != "Please review" {
		t.Errorf("Review.Prompt = %q, want %q", cfg.Review.Prompt, "Please review")
	}
}

func TestLoadConfigReviewCmd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
checks:
  test:
    run: echo ok
review:
  type: agent
  cmd: 'claude -p "$(cat {{prompt_file}})"'
  prompt: "Review {{diff}}"
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Review.Type != "agent" {
		t.Errorf("Review.Type = %q, want %q", cfg.Review.Type, "agent")
	}
	if cfg.Review.Cmd == "" {
		t.Error("Review.Cmd should not be empty")
	}
	if cfg.Review.Prompt != "Review {{diff}}" {
		t.Errorf("Review.Prompt = %q", cfg.Review.Prompt)
	}
}

func TestLoadConfigCommitMessage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
main_branch: main
commit:
  message: "[{{proj}}] [{{epic}}] [{{task}}] {{message}}"
checks:
  test:
    run: echo ok
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Commit.Message != "[{{proj}}] [{{epic}}] [{{task}}] {{message}}" {
		t.Errorf("Commit.Message = %q", cfg.Commit.Message)
	}
}

func TestParseConfigRejectsMergeRequireReview(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"approved", "none", "''"} {
		_, err := ParseConfig([]byte("merge:\n  require:\n    review: " + value + "\n"))
		if err == nil {
			t.Errorf("merge.require.review: %s accepted", value)
			continue
		}
		for _, want := range []string{"merge.require.review", "review.allow_none", "--review"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not mention %s", err, want)
			}
		}
	}
}

func TestParseConfigAllowNone(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte("review:\n  type: human\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Review.AllowNone {
		t.Error("allow_none should default to false")
	}
	cfg, err = ParseConfig([]byte("review:\n  allow_none: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Review.AllowNone {
		t.Error("allow_none: true not parsed")
	}
}

func TestInitScaffoldParses(t *testing.T) {
	t.Parallel()
	dir := initTestRepo(t)
	if err := RunInit(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(filepath.Join(dir, ".harness.yaml")); err != nil {
		t.Errorf("init scaffold does not load: %v", err)
	}
}

func TestLoadConfigCommitMessageDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := `
main_branch: main
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Commit.Message != "{{message}}" {
		t.Errorf("Commit.Message default = %q, want %q", cfg.Commit.Message, "{{message}}")
	}
}
