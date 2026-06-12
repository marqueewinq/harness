package internal

import "fmt"

func (a *App) CmdDepAdd(fromTaskID, toTaskID, depType string) error {
	if depType == "" {
		depType = "blocks"
	}
	if depType != "blocks" && depType != "related" {
		return fmt.Errorf("invalid dependency type %q (use blocks or related)", depType)
	}
	if err := a.DB.AddDependency(fromTaskID, toTaskID, depType); err != nil {
		return err
	}
	fromTask, _ := a.DB.GetTask(fromTaskID)
	toTask, _ := a.DB.GetTask(toTaskID)
	fmt.Printf("%s %q %s %s %q\n", fromTaskID, fromTask.Description, depType, toTaskID, toTask.Description)
	return nil
}

func (a *App) CmdDepRm(fromTaskID, toTaskID string) error {
	if err := a.DB.RemoveDependency(fromTaskID, toTaskID); err != nil {
		return err
	}
	fmt.Printf("Removed dependency %s -> %s\n", fromTaskID, toTaskID)
	return nil
}
