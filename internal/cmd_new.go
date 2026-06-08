package internal

import (
	"fmt"
	"path/filepath"
)

func (a *App) CmdNew(description string) error {
	branch := Slugify(description)
	worktreePath := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, branch)

	if err := a.Git.CreateBranch(branch, a.Config.MainBranch); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	if err := a.Git.CreateWorktree(branch, worktreePath); err != nil {
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating worktree: %w", err)
	}

	task, err := a.DB.CreateTask(description, branch, worktreePath)
	if err != nil {
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating task: %w", err)
	}

	fmt.Printf("Created %s: %q\n", task.ID, description)
	fmt.Printf("Branch: %s\n", branch)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("\nNext: implement the changes in %s/\n", worktreePath)
	fmt.Println("      then run: harness check")
	return nil
}
