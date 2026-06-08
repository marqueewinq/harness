package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func RunInit(dir string) error {
	configPath := filepath.Join(dir, ".harness.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf(".harness.yaml already exists")
	}

	scaffold := `# Harness workflow configuration
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
  rerun_checks: true

review:
  type: human
  prompt: |
    Review this change for correctness, test coverage, and project conventions.
    Previous review comments: {{previous_comments}}
    Task description: {{task_description}}
    After reviewing, call: harness record-review approve
    or: harness record-review request-changes --comment "your comments"
`
	if err := os.WriteFile(configPath, []byte(scaffold), 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	harnessDir := filepath.Join(dir, ".harness")
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return fmt.Errorf("creating .harness/: %w", err)
	}

	db, err := OpenDB(filepath.Join(harnessDir, "harness.db"))
	if err != nil {
		return fmt.Errorf("creating database: %w", err)
	}
	db.Close()

	gitignorePath := filepath.Join(dir, ".gitignore")
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}
	defer f.Close()
	f.WriteString("\n.harness/\n.worktrees/\n")

	fmt.Println("Initialized harness.")
	fmt.Printf("  Config: %s\n", configPath)
	fmt.Printf("  Database: %s/harness.db\n", harnessDir)
	fmt.Println("\nEdit .harness.yaml to configure checks and merge requirements.")
	return nil
}
