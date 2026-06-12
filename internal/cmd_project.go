package internal

import "fmt"

func (a *App) CmdProjectNew(title, description string) error {
	p, err := a.DB.CreateProject(title, description)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s: %q\n", p.ID, p.Title)
	return nil
}

func (a *App) CmdProjectList() error {
	projects, err := a.DB.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("No projects. Run: harness project new \"title\"")
		return nil
	}
	fmt.Printf("%-10s %-8s %s\n", "ID", "Status", "Title")
	for _, p := range projects {
		epics, _ := a.DB.ListEpicsByProject(p.ID)
		var totalTasks, doneTasks int
		for _, e := range epics {
			tasks, _ := a.DB.ListTasksByEpic(e.ID)
			totalTasks += len(tasks)
			for _, t := range tasks {
				if t.State == StateMerged || t.State == StateClosed {
					doneTasks++
				}
			}
		}
		progress := ""
		if totalTasks > 0 {
			progress = fmt.Sprintf(" (%d/%d tasks done)", doneTasks, totalTasks)
		}
		fmt.Printf("%-10s %-8s %s%s\n", p.ID, p.Status, p.Title, progress)
	}
	return nil
}

func (a *App) CmdProjectStatus(projectID string) error {
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	fmt.Printf("Project: %s %q\n", p.ID, p.Title)
	fmt.Printf("Status: %s\n", p.Status)
	if p.Description != "" {
		fmt.Printf("Description: %s\n", p.Description)
	}
	epics, _ := a.DB.ListEpicsByProject(p.ID)
	if len(epics) == 0 {
		fmt.Println("\nNo epics. Run: harness epic new \"title\" --project " + p.ID)
		return nil
	}
	fmt.Println("\nEpics:")
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
		fmt.Printf("  %-10s %-8s %s%s\n", e.ID, e.Status, e.Title, progress)
		for _, t := range tasks {
			fmt.Printf("    %-10s %-20s %s\n", t.ID, t.State, t.Description)
		}
	}
	return nil
}

func (a *App) CmdProjectClose(projectID string) error {
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	epics, _ := a.DB.ListEpicsByProject(p.ID)
	var openCount int
	for _, e := range epics {
		tasks, _ := a.DB.ListTasksByEpic(e.ID)
		for _, t := range tasks {
			if t.State != StateMerged && t.State != StateClosed {
				openCount++
			}
		}
	}
	if openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this project\n", openCount)
	}
	if err := a.DB.UpdateProjectStatus(projectID, "closed"); err != nil {
		return err
	}
	fmt.Printf("Closed %s: %q\n", p.ID, p.Title)
	return nil
}
