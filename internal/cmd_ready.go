package internal

import (
	"fmt"
	"strings"
)

func (a *App) ComputeReady() (ready []Task, blocked []Task, err error) {
	return a.ComputeReadyFiltered("", "")
}

func (a *App) ComputeReadyFiltered(filterEpic, filterProject string) (ready []Task, blocked []Task, err error) {
	var candidates []Task

	if filterEpic != "" {
		candidates, err = a.DB.ListTasksByEpic(filterEpic)
	} else if filterProject != "" {
		epics, err2 := a.DB.ListEpicsByProject(filterProject)
		if err2 != nil {
			return nil, nil, err2
		}
		for _, e := range epics {
			tasks, _ := a.DB.ListTasksByEpic(e.ID)
			candidates = append(candidates, tasks...)
		}
	} else {
		candidates, err = a.DB.ListTasks()
	}
	if err != nil {
		return nil, nil, err
	}

	for _, t := range candidates {
		if t.State != StatePlanned {
			continue
		}
		blockers, err := a.DB.GetBlockers(t.ID)
		if err != nil {
			return nil, nil, err
		}
		isBlocked := false
		for _, b := range blockers {
			blocker, err := a.DB.GetTask(b.FromTaskID)
			if err != nil {
				continue
			}
			if blocker.State != StateMerged && blocker.State != StateClosed {
				isBlocked = true
				break
			}
		}
		if isBlocked {
			blocked = append(blocked, t)
		} else {
			ready = append(ready, t)
		}
	}
	return ready, blocked, nil
}

func (a *App) CmdReady(filterEpic, filterProject string) error {
	ready, blocked, err := a.ComputeReadyFiltered(filterEpic, filterProject)
	if err != nil {
		return err
	}
	if len(ready) == 0 && len(blocked) == 0 {
		fmt.Println("No planned tasks. Run: harness plan new \"description\" --epic <id>")
		return nil
	}
	if len(ready) > 0 {
		fmt.Println("READY:")
		for _, t := range ready {
			epicInfo := ""
			if t.EpicID != "" {
				epicInfo = fmt.Sprintf("  (%s)", t.EpicID)
			}
			fmt.Printf("  %-10s %s%s\n", t.ID, t.Description, epicInfo)
		}
	}
	if len(blocked) > 0 {
		if len(ready) > 0 {
			fmt.Println()
		}
		fmt.Println("BLOCKED:")
		for _, t := range blocked {
			blockers, _ := a.DB.GetBlockers(t.ID)
			var blockerIDs []string
			for _, b := range blockers {
				bt, _ := a.DB.GetTask(b.FromTaskID)
				if bt != nil && bt.State != StateMerged && bt.State != StateClosed {
					blockerIDs = append(blockerIDs, b.FromTaskID)
				}
			}
			fmt.Printf("  %-10s %s  (blocked by: %s)\n",
				t.ID, t.Description, strings.Join(blockerIDs, ", "))
		}
	}
	return nil
}
