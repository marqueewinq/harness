package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
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
    review: approved
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
	_, err := LoadConfig("/nonexistent/.harness.yaml")
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte("{{invalid yaml::"), 0644)

	_, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoadConfigEmptyChecks(t *testing.T) {
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
