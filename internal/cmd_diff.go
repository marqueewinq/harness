package internal

import "fmt"

func (a *App) CmdDiff(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	diff, err := a.Git.Diff(a.Config.MainBranch, task.Branch)
	if err != nil {
		return fmt.Errorf("getting diff: %w", err)
	}
	if diff == "" {
		fmt.Println("No changes.")
	} else {
		fmt.Println(diff)
	}
	return nil
}
