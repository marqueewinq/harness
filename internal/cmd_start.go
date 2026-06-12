package internal

import (
	"fmt"
	"path/filepath"
)

func (a *App) CmdTaskStart(taskID, baseBranch string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("task %s is %s, not PLANNED", taskID, task.State)
	}
	if baseBranch == "" && task.EpicID != "" {
		// Resolve base branch from epic
		if epic, err := a.DB.GetEpic(task.EpicID); err == nil && epic.Branch != "" {
			baseBranch = epic.Branch
		}
	}
	if baseBranch == "" {
		baseBranch = a.Config.MainBranch
	}

	// Warn about blockers but proceed
	blockers, _ := a.DB.GetBlockers(taskID)
	for _, b := range blockers {
		blocker, err := a.DB.GetTask(b.FromTaskID)
		if err != nil {
			continue
		}
		if blocker.State != StateMerged && blocker.State != StateClosed {
			fmt.Printf("Warning: blocked by %s %q (%s)\n", blocker.ID, blocker.Description, blocker.State)
		}
	}

	// Detect parent task from base branch
	var parentTaskID string
	if parentTask, err := a.DB.GetTaskByBranch(baseBranch); err == nil {
		parentTaskID = parentTask.ID
	}

	branch := Slugify(task.Description)
	worktreePath := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, branch)

	if err := a.Git.CreateBranch(branch, baseBranch); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}
	if err := a.Git.CreateWorktree(branch, worktreePath); err != nil {
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating worktree: %w", err)
	}
	if err := a.DB.StartTask(taskID, branch, worktreePath, baseBranch, parentTaskID); err != nil {
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("updating task: %w", err)
	}

	fmt.Printf("Started %s: %q\n", taskID, task.Description)
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
