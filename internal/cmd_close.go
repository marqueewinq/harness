package internal

import "fmt"

func (a *App) CmdWorkClose(taskID string, cleanup bool) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	a.showNewNotes(id)

	if !ValidTransition(task.State, StateClosed) {
		return fmt.Errorf("cannot close task in state %s", task.State)
	}

	// If queued, dequeue first
	if task.State == StateQueued {
		a.DB.DequeueTask(id)
	}

	if err := a.DB.UpdateTaskState(id, StateClosed); err != nil {
		return err
	}

	fmt.Printf("Task %s is now CLOSED.\n", id)

	if cleanup {
		if task.WorktreePath != "" {
			a.Git.RemoveWorktree(task.WorktreePath)
			fmt.Printf("  Worktree: %s (removed)\n", task.WorktreePath)
		}
		if task.Branch != "" {
			a.Git.DeleteBranch(task.Branch)
			fmt.Printf("  Branch: %s (deleted)\n", task.Branch)
		}
	}

	return nil
}
