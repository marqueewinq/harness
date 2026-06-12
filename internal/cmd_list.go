package internal

import "fmt"

func (a *App) CmdList() error {
	return a.cmdListFull("", "")
}

func (a *App) CmdListFiltered(projectID, epicID string) error {
	return a.cmdListFull(projectID, epicID)
}

func (a *App) cmdListFull(filterProject, filterEpic string) error {
	if filterEpic != "" {
		e, err := a.DB.GetEpic(filterEpic)
		if err != nil {
			return err
		}
		fmt.Printf("Epic: %s %q\n\n", e.ID, e.Title)
		tasks, _ := a.DB.ListTasksByEpic(filterEpic)
		if len(tasks) == 0 {
			fmt.Println("No tasks.")
			return nil
		}
		fmt.Printf("%-10s %-20s %s\n", "ID", "State", "Description")
		for _, t := range tasks {
			a.printTaskLine(t, "")
		}
		return nil
	}

	projects, _ := a.DB.ListProjects()
	allTasks, _ := a.DB.ListTasks()
	shown := map[string]bool{}

	if filterProject != "" {
		filtered := []Project{}
		for _, p := range projects {
			if p.ID == filterProject {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}

	if len(projects) > 0 {
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
				progress = fmt.Sprintf("  (%d/%d tasks done)", doneTasks, totalTasks)
			}
			fmt.Printf("%-10s %-8s %s%s\n", p.ID, p.Status, p.Title, progress)
			for _, e := range epics {
				tasks, _ := a.DB.ListTasksByEpic(e.ID)
				var eDone int
				for _, t := range tasks {
					if t.State == StateMerged || t.State == StateClosed {
						eDone++
					}
				}
				epicProgress := ""
				if len(tasks) > 0 {
					epicProgress = fmt.Sprintf("  (%d/%d)", eDone, len(tasks))
				}
				fmt.Printf("  %-10s %-8s %s%s\n", e.ID, e.Status, e.Title, epicProgress)
				for _, t := range tasks {
					a.printTaskLine(t, "    ")
					shown[t.ID] = true
				}
			}
		}
	}

	var orphans []Task
	for _, t := range allTasks {
		if !shown[t.ID] {
			orphans = append(orphans, t)
		}
	}

	if len(orphans) > 0 {
		if len(projects) > 0 {
			fmt.Println()
		}
		if len(projects) == 0 && len(orphans) == len(allTasks) {
			fmt.Printf("%-10s %-20s %s\n", "ID", "State", "Description")
		}
		printed := map[string]bool{}
		for _, t := range orphans {
			if t.ParentTaskID != "" {
				continue
			}
			a.printTaskLine(t, "")
			printed[t.ID] = true
			for _, st := range orphans {
				if st.ParentTaskID == t.ID {
					a.printTaskLine(st, "  ")
					printed[st.ID] = true
				}
			}
		}
		for _, t := range orphans {
			if !printed[t.ID] {
				a.printTaskLine(t, "")
			}
		}
	}

	if len(projects) == 0 && len(orphans) == 0 {
		fmt.Println("No tasks. Run: harness project new \"title\" or harness new \"description\"")
	}
	return nil
}

func (a *App) printTaskLine(t Task, indent string) {
	queueInfo := ""
	if t.State == StateQueued {
		pos, err := a.DB.GetQueuePosition(t.ID)
		if err == nil {
			queueInfo = fmt.Sprintf(" (#%d)", pos)
		}
	}
	target := ""
	if t.BaseBranch != "" && t.BaseBranch != a.Config.MainBranch {
		target = fmt.Sprintf(" -> %s", t.BaseBranch)
	}
	blockerInfo := ""
	if t.State == StatePlanned {
		blockers, _ := a.DB.GetBlockers(t.ID)
		var openBlockers []string
		for _, b := range blockers {
			bt, _ := a.DB.GetTask(b.FromTaskID)
			if bt != nil && bt.State != StateMerged && bt.State != StateClosed {
				openBlockers = append(openBlockers, b.FromTaskID)
			}
		}
		if len(openBlockers) > 0 {
			blockerInfo = fmt.Sprintf(" [blocked by %s]", joinIDs(openBlockers))
		}
	}
	fmt.Printf("%s%-10s %-20s %s%s%s%s\n", indent, t.ID, t.State, t.Description, target, queueInfo, blockerInfo)
}

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	result := ids[0]
	for _, id := range ids[1:] {
		result += ", " + id
	}
	return result
}
