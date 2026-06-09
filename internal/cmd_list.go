package internal

import "fmt"

func (a *App) CmdList() error {
	tasks, err := a.DB.ListTasks()
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No tasks. Run: harness new \"description\"")
		return nil
	}
	fmt.Printf("%-10s %-20s %s\n", "ID", "State", "Description")

	// Separate root tasks and subtasks
	printed := map[string]bool{}
	for _, t := range tasks {
		if t.ParentTaskID != "" {
			continue // skip subtasks in first pass
		}
		a.printTaskLine(t, "")
		printed[t.ID] = true
		// Print subtasks under this parent
		for _, st := range tasks {
			if st.ParentTaskID == t.ID {
				a.printTaskLine(st, "  ")
				printed[st.ID] = true
			}
		}
	}
	// Print any orphaned subtasks (parent not in current list)
	for _, t := range tasks {
		if !printed[t.ID] {
			a.printTaskLine(t, "")
		}
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
	if t.BaseBranch != a.Config.MainBranch {
		target = fmt.Sprintf(" -> %s", t.BaseBranch)
	}
	fmt.Printf("%s%-10s %-20s %s%s%s\n", indent, t.ID, t.State, t.Description, target, queueInfo)
}
