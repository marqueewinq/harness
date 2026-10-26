package internal

import (
	"fmt"
	"strings"
)

func (a *App) CmdTaskNew(title, description, epicID string) error {
	if epicID != "" {
		e, err := a.DB.GetEpic(epicID)
		if err != nil {
			return err
		}
		if e.Status == "closed" {
			fmt.Printf("Warning: epic %s is closed\n", epicID)
		}
	}
	task, err := a.DB.CreatePlannedTask(title, description, epicID)
	if err != nil {
		return err
	}
	epicInfo := ""
	if epicID != "" {
		epicInfo = fmt.Sprintf(", %s", epicID)
	}
	fmt.Printf("Created %s: %q (PLANNED%s)\n", task.ID, title, epicInfo)
	if description != "" {
		fmt.Printf("Description: %s\n", description)
	}
	fmt.Printf("\nNext: harness task start %s\n", task.ID)
	return nil
}

func (a *App) CmdTaskEdit(taskID, title, description, epicID string, setTitle, setDescription, setEpic bool) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("can only edit PLANNED tasks (task %s is %s)", taskID, task.State)
	}

	// Apply only the fields that were explicitly set
	newTitle := task.Title
	if setTitle {
		newTitle = title
	}
	newDesc := task.Description
	if setDescription {
		newDesc = description
	}
	newEpic := task.EpicID
	if setEpic {
		newEpic = epicID
		// Validate epic if non-empty
		if newEpic != "" {
			if _, err := a.DB.GetEpic(newEpic); err != nil {
				return err
			}
		}
	}

	if err := a.DB.UpdatePlannedTask(taskID, newTitle, newDesc, newEpic); err != nil {
		return err
	}
	fmt.Printf("Updated %s: %q\n", taskID, newTitle)
	return nil
}

func (a *App) CmdTaskDelete(taskID string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if err := a.DB.DeletePlannedTask(taskID); err != nil {
		return err
	}
	fmt.Printf("Deleted %s: %q\n", task.ID, task.Title)
	return nil
}

func (a *App) CmdTaskView(taskID string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}

	fmt.Printf("Task: %s %q\n", task.ID, task.Title)
	if task.Description != "" {
		fmt.Printf("Description: %s\n", task.Description)
	}
	fmt.Printf("State: %s\n", task.State)
	if task.EpicID != "" {
		if epic, err := a.DB.GetEpic(task.EpicID); err == nil {
			fmt.Printf("Epic: %s %q\n", epic.ID, epic.Title)
			if proj, err := a.DB.GetProject(epic.ProjectID); err == nil {
				fmt.Printf("Project: %s %q\n", proj.ID, proj.Title)
			}
		}
	}

	a.printDependencies(task.ID)
	return nil
}

func (a *App) printDependencies(taskID string) {
	deps, _ := a.DB.GetDependencies(taskID)
	if len(deps) == 0 {
		return
	}
	fmt.Println("\nDependencies:")
	for _, d := range deps {
		if d.FromTaskID == taskID {
			if dt, _ := a.DB.GetTask(d.ToTaskID); dt != nil {
				fmt.Printf("  %s %s (%s) %q\n", d.DepType, d.ToTaskID, dt.State, dt.Title)
			}
		} else if dt, _ := a.DB.GetTask(d.FromTaskID); dt != nil {
			fmt.Printf("  blocked by %s (%s) %q\n", d.FromTaskID, dt.State, dt.Title)
		}
	}
}

func (a *App) CmdTaskList(filterEpic, filterProject, filterStatus string, readyOnly bool) error {
	var candidates []Task
	var err error

	if filterEpic != "" {
		candidates, err = a.DB.ListTasksByEpic(filterEpic)
	} else if filterProject != "" {
		epics, err2 := a.DB.ListEpicsByProject(filterProject)
		if err2 != nil {
			return err2
		}
		for _, e := range epics {
			tasks, _ := a.DB.ListTasksByEpic(e.ID)
			candidates = append(candidates, tasks...)
		}
	} else {
		candidates, err = a.DB.ListTasks()
	}
	if err != nil {
		return err
	}

	if filterStatus == "" {
		filterStatus = "planned"
	}

	var filtered []Task
	for _, t := range candidates {
		if filterStatus == "all" || strings.ToUpper(filterStatus) == string(t.State) {
			filtered = append(filtered, t)
		}
	}

	if readyOnly {
		var readyFiltered []Task
		for _, t := range filtered {
			if t.State != StatePlanned {
				readyFiltered = append(readyFiltered, t)
				continue
			}
			blockers, _ := a.DB.GetBlockers(t.ID)
			isBlocked := false
			for _, b := range blockers {
				bt, _ := a.DB.GetTask(b.FromTaskID)
				if bt != nil && bt.State != StateMerged && bt.State != StateClosed {
					isBlocked = true
					break
				}
			}
			if !isBlocked {
				readyFiltered = append(readyFiltered, t)
			}
		}
		filtered = readyFiltered
	}

	if len(filtered) == 0 {
		fmt.Println("No tasks match the filter.")
		return nil
	}

	projectByEpic := map[string]string{}
	for _, t := range filtered {
		if t.EpicID != "" {
			if _, ok := projectByEpic[t.EpicID]; !ok {
				if e, err := a.DB.GetEpic(t.EpicID); err == nil {
					projectByEpic[t.EpicID] = e.ProjectID
				}
			}
		}
	}

	fmt.Printf("%-10s %-20s %-10s %-10s %s\n", "ID", "State", "Project", "Epic", "Title")
	for _, t := range filtered {
		projInfo := ""
		epicInfo := ""
		if t.EpicID != "" {
			epicInfo = t.EpicID
			projInfo = projectByEpic[t.EpicID]
		}
		fmt.Printf("%-10s %-20s %-10s %-10s %s\n", t.ID, t.State, projInfo, epicInfo, t.Title)
	}
	return nil
}

func (a *App) CmdTaskClose(taskID string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("task close is for PLANNED tasks (task %s is %s); use 'harness work close' for active tasks", taskID, task.State)
	}
	if err := a.DB.UpdateTaskState(taskID, StateClosed); err != nil {
		return err
	}
	fmt.Printf("Task %s is now CLOSED.\n", taskID)
	return nil
}
