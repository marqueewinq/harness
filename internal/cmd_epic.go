package internal

import "fmt"

func (a *App) CmdEpicNew(title, description, projectID, branchFlag string, branchFlagSet bool, review string) error {
	if projectID == "" {
		return fmt.Errorf("--project is required")
	}
	reviewSetting, err := a.creationReviewType(review)
	if err != nil {
		return err
	}
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	if p.Status == ProjectStatusClosed {
		fmt.Printf("Warning: project %s is closed\n", projectID)
	}

	// Determine branch
	var branch string
	if branchFlagSet {
		// Explicit --branch flag
		branch = branchFlag
		if branch != "" && branch != a.Config.MainBranch {
			// Validate branch exists
			if _, err := a.Git.RevParse(branch); err != nil {
				return fmt.Errorf("branch %q does not exist", branch)
			}
		}
	} else if a.Config.EpicFeatureBranchesEnabled() {
		// Default: auto-create epic/<slug> branch
		branch = "epic/" + Slugify(title)
		if err := a.Git.CreateBranch(branch, a.Config.MainBranch); err != nil {
			return fmt.Errorf("creating epic branch: %w", err)
		}
	}
	// else: epic_feature_branches: false + no --branch -> no branch

	e, err := a.DB.CreateEpic(title, description, projectID, branch, reviewSetting)
	if err != nil {
		// Clean up branch if we created it
		if branch != "" && !branchFlagSet {
			a.Git.DeleteBranch(branch)
		}
		return err
	}
	fmt.Printf("Created %s: %q (%s, review: %s)\n", e.ID, e.Title, projectID, e.ReviewType)
	if branch != "" {
		fmt.Printf("Branch: %s\n", branch)
	}
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
	fmt.Printf("%-10s %-10s %-8s %-20s %s\n", "ID", "Project", "Status", "Branch", "Title")
	for _, e := range epics {
		tasks, _ := a.DB.ListTasksByEpic(e.ID)
		done, total := CountDone(tasks)
		progress := ""
		if total > 0 {
			progress = fmt.Sprintf(" (%d/%d)", done, total)
		}
		branchDisplay := "-"
		if e.Branch != "" {
			branchDisplay = e.Branch
		}
		fmt.Printf("%-10s %-10s %-8s %-20s %s%s\n", e.ID, e.ProjectID, e.Status, branchDisplay, e.Title, progress)
	}
	return nil
}

func (a *App) CmdEpicView(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	p, err := a.DB.GetProject(e.ProjectID)
	if err != nil {
		return fmt.Errorf("epic %s: %w", e.ID, err)
	}
	fmt.Printf("Epic: %s %q\n", e.ID, e.Title)
	fmt.Printf("Project: %s %q\n", p.ID, p.Title)
	fmt.Printf("Status: %s\n", e.Status)
	if m, err := a.LastEpicMergeFailure(e.ID); err == nil && m != nil {
		fmt.Printf("Merge failed: %s\n", m)
	}
	fmt.Printf("Review type: %s\n", FormatReviewType(e.ReviewType, e.ReviewTypeSetBy, e.ReviewTypeSetAt))
	if e.Branch != "" {
		fmt.Printf("Branch: %s\n", e.Branch)
	}
	if e.Description != "" {
		fmt.Printf("Description: %s\n", e.Description)
	}
	printNotes(e.Notes)
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	if len(tasks) == 0 {
		fmt.Printf("\nNo tasks. Run: harness task new \"title\" --epic %s\n", e.ID)
		return nil
	}
	fmt.Println("\nTasks:")
	for _, t := range tasks {
		fmt.Printf("  %-10s %-20s %-8s %s\n", t.ID, t.State, t.ReviewType, t.Title)
	}
	return nil
}

func (a *App) CmdEpicClose(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	done, total := CountDone(tasks)
	if openCount := total - done; openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this epic\n", openCount)
	}
	if err := a.DB.UpdateEpicStatus(epicID, EpicStatusClosed); err != nil {
		return err
	}
	fmt.Printf("Closed %s: %q\n", e.ID, e.Title)
	return nil
}

func (a *App) CmdEpicEdit(epicID, title, description, projectID string, setTitle, setDescription, setProject bool) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Status == EpicStatusClosed {
		return fmt.Errorf("cannot edit closed epic %s", epicID)
	}
	newTitle := e.Title
	if setTitle {
		newTitle = title
	}
	newDesc := e.Description
	if setDescription {
		newDesc = description
	}
	newProject := e.ProjectID
	if setProject {
		if projectID == "" {
			return fmt.Errorf("--project cannot be empty: epics require a project")
		}
		if _, err := a.DB.GetProject(projectID); err != nil {
			return err
		}
		newProject = projectID
	}
	if err := a.DB.UpdateEpic(epicID, newTitle, newDesc, newProject); err != nil {
		return err
	}
	fmt.Printf("Updated %s: %q\n", epicID, newTitle)
	return nil
}
