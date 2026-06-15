package internal

import "fmt"

func (a *App) CmdTaskStart(taskID, baseBranch string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("task %s is %s, not PLANNED", taskID, task.State)
	}

	baseBranch = a.resolveBaseBranch(baseBranch, task.EpicID)

	// Warn about blockers but proceed
	blockers, _ := a.DB.GetBlockers(taskID)
	for _, b := range blockers {
		blocker, err := a.DB.GetTask(b.FromTaskID)
		if err != nil {
			continue
		}
		if blocker.State != StateMerged && blocker.State != StateClosed {
			fmt.Printf("Warning: blocked by %s %q (%s)\n", blocker.ID, blocker.Title, blocker.State)
		}
	}

	slug := Slugify(task.Title)
	branch, worktreePath, parentTaskID, err := a.createTaskBranch(slug, baseBranch)
	if err != nil {
		return err
	}
	if err := a.DB.StartTask(taskID, branch, worktreePath, baseBranch, parentTaskID); err != nil {
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("updating task: %w", err)
	}

	fmt.Printf("Started %s: %q\n", taskID, task.Title)
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
