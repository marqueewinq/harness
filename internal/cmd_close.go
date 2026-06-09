package internal

import "fmt"

func (a *App) CmdClose(taskID string, cleanup bool) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

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
		a.Git.RemoveWorktree(task.WorktreePath)
		a.Git.DeleteBranch(task.Branch)
		fmt.Printf("  Worktree: %s (removed)\n", task.WorktreePath)
		fmt.Printf("  Branch: %s (deleted)\n", task.Branch)
	}

	return nil
}
