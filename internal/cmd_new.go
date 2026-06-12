package internal

import (
	"fmt"
	"path/filepath"
)

func (a *App) CmdNew(description, baseBranch, epicID, notes string) error {
	if baseBranch == "" {
		baseBranch = a.Config.MainBranch
	}

	// Check if baseBranch is another task's branch -> set parent
	var parentTaskID string
	if parentTask, err := a.DB.GetTaskByBranch(baseBranch); err == nil {
		parentTaskID = parentTask.ID
	}

	branch := Slugify(description)
	worktreePath := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, branch)

	if err := a.Git.CreateBranch(branch, baseBranch); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	if err := a.Git.CreateWorktree(branch, worktreePath); err != nil {
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating worktree: %w", err)
	}

	task, err := a.DB.CreateTask(description, notes, branch, worktreePath, baseBranch, parentTaskID, epicID)
	if err != nil {
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating task: %w", err)
	}

	fmt.Printf("Created %s: %q\n", task.ID, description)
	fmt.Printf("Branch: %s\n", branch)
	baseInfo := baseBranch
	if parentTaskID != "" {
		baseInfo = fmt.Sprintf("%s (%s)", baseBranch, parentTaskID)
	}
	fmt.Printf("Base: %s\n", baseInfo)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("\nNext: implement the changes in %s/\n", worktreePath)
	fmt.Println("      then run: harness check")
	return nil
}
