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
	for _, t := range tasks {
		queueInfo := ""
		if t.State == StateQueued {
			pos, err := a.DB.GetQueuePosition(t.ID)
			if err == nil {
				queueInfo = fmt.Sprintf(" (#%d)", pos)
			}
		}
		fmt.Printf("%-10s %-20s %s%s\n", t.ID, t.State, t.Description, queueInfo)
	}
	return nil
}
