package internal

import "fmt"

func (a *App) CmdPlanNew(description, notes, epicID string) error {
	if epicID != "" {
		e, err := a.DB.GetEpic(epicID)
		if err != nil {
			return err
		}
		if e.Status == "closed" {
			fmt.Printf("Warning: epic %s is closed\n", epicID)
		}
	}
	task, err := a.DB.CreatePlannedTask(description, notes, epicID)
	if err != nil {
		return err
	}
	epicInfo := ""
	if epicID != "" {
		epicInfo = fmt.Sprintf(", %s", epicID)
	}
	fmt.Printf("Created %s: %q (PLANNED%s)\n", task.ID, description, epicInfo)
	if notes != "" {
		fmt.Printf("Description: %s\n", notes)
	}
	fmt.Printf("\nNext: harness start %s\n", task.ID)
	return nil
}

func (a *App) CmdPlanEdit(taskID, description, notes, epicID string, setDescription, setNotes, setEpic bool) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("can only edit PLANNED tasks (task %s is %s)", taskID, task.State)
	}

	// Apply only the fields that were explicitly set
	newDesc := task.Description
	if setDescription {
		newDesc = description
	}
	newNotes := task.Notes
	if setNotes {
		newNotes = notes
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

	if err := a.DB.UpdatePlannedTask(taskID, newDesc, newNotes, newEpic); err != nil {
		return err
	}
	fmt.Printf("Updated %s: %q\n", taskID, newDesc)
	return nil
}

func (a *App) CmdPlanDelete(taskID string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}
	if err := a.DB.DeletePlannedTask(taskID); err != nil {
		return err
	}
	fmt.Printf("Deleted %s: %q\n", task.ID, task.Description)
	return nil
}

func (a *App) CmdPlanView(taskID string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}

	fmt.Printf("Task: %s %q\n", task.ID, task.Description)
	if task.Notes != "" {
		fmt.Printf("Description: %s\n", task.Notes)
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

	// Show dependencies
	deps, _ := a.DB.GetDependencies(task.ID)
	if len(deps) > 0 {
		fmt.Println("\nDependencies:")
		for _, d := range deps {
			if d.FromTaskID == task.ID {
				dt, _ := a.DB.GetTask(d.ToTaskID)
				if dt != nil {
					fmt.Printf("  %s %s (%s) %q\n", d.DepType, d.ToTaskID, dt.State, dt.Description)
				}
			} else {
				dt, _ := a.DB.GetTask(d.FromTaskID)
				if dt != nil {
					fmt.Printf("  blocked by %s (%s) %q\n", d.FromTaskID, dt.State, dt.Description)
				}
			}
		}
	}

	return nil
}

func (a *App) CmdPlanList(filterEpic, filterProject string) error {
	var candidates []Task
	var err error

	if filterEpic != "" {
		candidates, err = a.DB.ListTasksByEpic(filterEpic)
	} else if filterProject != "" {
		// Collect tasks from all epics under this project
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

	var planned []Task
	for _, t := range candidates {
		if t.State == StatePlanned {
			planned = append(planned, t)
		}
	}

	if len(planned) == 0 {
		fmt.Println("No planned tasks. Run: harness plan new \"description\"")
		return nil
	}

	// Build epic->project lookup
	projectByEpic := map[string]string{}
	for _, t := range planned {
		if t.EpicID != "" {
			if _, ok := projectByEpic[t.EpicID]; !ok {
				if e, err := a.DB.GetEpic(t.EpicID); err == nil {
					projectByEpic[t.EpicID] = e.ProjectID
				}
			}
		}
	}

	fmt.Printf("%-10s %-10s %-10s %s\n", "ID", "Project", "Epic", "Description")
	for _, t := range planned {
		projInfo := ""
		epicInfo := ""
		if t.EpicID != "" {
			epicInfo = t.EpicID
			projInfo = projectByEpic[t.EpicID]
		}
		fmt.Printf("%-10s %-10s %-10s %s\n", t.ID, projInfo, epicInfo, t.Description)
	}
	return nil
}
