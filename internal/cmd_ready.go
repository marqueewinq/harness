package internal

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


