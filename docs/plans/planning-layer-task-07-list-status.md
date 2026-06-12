### Task 7: Updated list + status + close Commands

**Files:**
- Modify: `internal/cmd_list.go`
- Modify: `internal/cmd_status.go`
- Modify: `internal/cmd_close.go`

---

- [ ] **Step 1: Write test for hierarchical list**

Add to `internal/e2e_test.go`:

```go
func TestCmdListHierarchy(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Create hierarchy
	proj, _ := app.DB.CreateProject("Auth project", "")
	epic, _ := app.DB.CreateEpic("OAuth2", "", proj.ID)
	app.CmdPlan("planned task", epic.ID)

	// Create an orphan task via CmdNew
	app.CmdNew("orphan task", "")

	// CmdList should not error
	err = app.CmdList()
	if err != nil {
		t.Fatal("list:", err)
	}
}
```

This is a smoke test — CmdList output goes to stdout. We're verifying it
doesn't crash with the new hierarchy. For detailed output verification,
capture stdout if desired, but a smoke test is sufficient.

- [ ] **Step 2: Run test to verify it passes (or identify issues)**

Run: `go test ./internal/ -run TestCmdListHierarchy -v`
Expected: May pass (existing CmdList still works) or fail if PLANNED tasks
with NULL branch cause issues in `printTaskLine`.

- [ ] **Step 3: Update cmd_list.go**

Replace `internal/cmd_list.go` with the hierarchical version:

```go
package internal

import "fmt"

func (a *App) CmdList() error {
	return a.cmdListFull("", "")
}

func (a *App) CmdListFiltered(projectID, epicID string) error {
	return a.cmdListFull(projectID, epicID)
}

func (a *App) cmdListFull(filterProject, filterEpic string) error {
	// If filtering by epic, just show tasks for that epic
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

	// Load all data
	projects, _ := a.DB.ListProjects()
	allTasks, _ := a.DB.ListTasks()

	// Track which tasks are shown under a project/epic
	shown := map[string]bool{}

	// Filter projects if requested
	if filterProject != "" {
		filtered := []Project{}
		for _, p := range projects {
			if p.ID == filterProject {
				filtered = append(filtered, p)
			}
		}
		projects = filtered
	}

	// Show project hierarchy
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
				epicProgress := ""
				var eDone int
				for _, t := range tasks {
					if t.State == StateMerged || t.State == StateClosed {
						eDone++
					}
				}
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

	// Show tasks not under any epic (orphans + old-style tasks)
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
		// Use existing parent-child display for orphans
		if len(projects) == 0 && len(orphans) == len(allTasks) {
			// No projects at all — use original format
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
	// Show blocker info for PLANNED tasks
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
```

- [ ] **Step 4: Run list test**

Run: `go test ./internal/ -run TestCmdListHierarchy -v`
Expected: PASS

- [ ] **Step 5: Update cmd_status.go**

Add epic/project context and dependency info to `CmdStatus`. Insert after the
"Parent" section (around line 28) and before the commit range section:

```go
// Show epic/project context
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
```

Also update the "Next" section at the bottom to handle PLANNED state:

```go
case StatePlanned:
	fmt.Printf("Next: run harness start %s\n", task.ID)
```

For PLANNED tasks, skip the commit range section (no branch exists):

```go
if task.State != StatePlanned && task.State != StateMerged {
	// show commit range...
}
```

- [ ] **Step 6: Guard CmdClose for PLANNED tasks**

Modify `internal/cmd_close.go` — when `--cleanup` is true, skip worktree/branch
removal if they're empty (PLANNED tasks have no git resources):

```go
if cleanup {
	if task.WorktreePath != "" {
		a.Git.RemoveWorktree(task.WorktreePath)
		fmt.Printf("  Worktree: %s (removed)\n", task.WorktreePath)
	}
	if task.Branch != "" {
		a.Git.DeleteBranch(task.Branch)
		fmt.Printf("  Branch: %s (deleted)\n", task.Branch)
	}
}
```

- [ ] **Step 7: Run existing status tests to verify no breakage**

Run: `go test ./internal/ -v`
Expected: All tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/cmd_list.go internal/cmd_status.go internal/cmd_close.go
git commit -m "hierarchical list + status with planning context"
```
