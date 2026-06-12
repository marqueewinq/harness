package internal

import "fmt"

func (a *App) CmdEpicNew(title, description, projectID string) error {
	if projectID == "" {
		return fmt.Errorf("--project is required")
	}
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	if p.Status == "closed" {
		fmt.Printf("Warning: project %s is closed\n", projectID)
	}
	e, err := a.DB.CreateEpic(title, description, projectID)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s: %q (%s)\n", e.ID, e.Title, projectID)
	return nil
}

func (a *App) CmdEpicList(projectID string) error {
	var epics []Epic
	var err error
	if projectID != "" {
		epics, err = a.DB.ListEpicsByProject(projectID)
	} else {
		epics, err = a.DB.ListEpics()
	}
	if err != nil {
		return err
	}
	if len(epics) == 0 {
		fmt.Println("No epics. Run: harness epic new \"title\" --project <id>")
		return nil
	}
	fmt.Printf("%-10s %-10s %-8s %s\n", "ID", "Project", "Status", "Title")
	for _, e := range epics {
		tasks, _ := a.DB.ListTasksByEpic(e.ID)
		var done int
		for _, t := range tasks {
			if t.State == StateMerged || t.State == StateClosed {
				done++
			}
		}
		progress := ""
		if len(tasks) > 0 {
			progress = fmt.Sprintf(" (%d/%d)", done, len(tasks))
		}
		fmt.Printf("%-10s %-10s %-8s %s%s\n", e.ID, e.ProjectID, e.Status, e.Title, progress)
	}
	return nil
}

func (a *App) CmdEpicStatus(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	p, _ := a.DB.GetProject(e.ProjectID)
	fmt.Printf("Epic: %s %q\n", e.ID, e.Title)
	fmt.Printf("Project: %s %q\n", p.ID, p.Title)
	fmt.Printf("Status: %s\n", e.Status)
	if e.Description != "" {
		fmt.Printf("Description: %s\n", e.Description)
	}
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	if len(tasks) == 0 {
		fmt.Printf("\nNo tasks. Run: harness plan \"description\" --epic %s\n", e.ID)
		return nil
	}
	fmt.Println("\nTasks:")
	for _, t := range tasks {
		fmt.Printf("  %-10s %-20s %s\n", t.ID, t.State, t.Description)
	}
	return nil
}

func (a *App) CmdEpicClose(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	var openCount int
	for _, t := range tasks {
		if t.State != StateMerged && t.State != StateClosed {
			openCount++
		}
	}
	if openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this epic\n", openCount)
	}
	if err := a.DB.UpdateEpicStatus(epicID, "closed"); err != nil {
		return err
	}
	fmt.Printf("Closed %s: %q\n", e.ID, e.Title)
	return nil
}
