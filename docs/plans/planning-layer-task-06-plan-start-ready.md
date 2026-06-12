### Task 6: plan, start, ready Commands

**Files:**
- Create: `internal/cmd_plan.go`
- Create: `internal/cmd_start.go`
- Create: `internal/cmd_ready.go`

These are the three core planning-layer commands.

---

- [ ] **Step 1: Write test for CmdPlan**

Add to `internal/e2e_test.go` (or a new test file — but keeping it in e2e_test
follows the existing pattern since these need a full App with git repo):

```go
func TestCmdPlan(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Create project + epic first
	proj, _ := app.DB.CreateProject("Test project", "")
	epic, _ := app.DB.CreateEpic("Test epic", "", proj.ID)

	// Plan a task with epic
	err = app.CmdPlan("implement feature X", epic.ID)
	if err != nil {
		t.Fatal("plan:", err)
	}

	tasks, _ := app.DB.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.State != StatePlanned {
		t.Errorf("state = %s, want PLANNED", task.State)
	}
	if task.Branch != "" {
		t.Errorf("branch = %q, want empty", task.Branch)
	}
	if task.WorktreePath != "" {
		t.Errorf("worktree = %q, want empty", task.WorktreePath)
	}
	if task.EpicID != epic.ID {
		t.Errorf("epic_id = %q, want %q", task.EpicID, epic.ID)
	}

	// Plan a task without epic (orphan)
	err = app.CmdPlan("orphan task", "")
	if err != nil {
		t.Fatal("plan orphan:", err)
	}
	tasks, _ = app.DB.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[1].EpicID != "" {
		t.Errorf("orphan epic_id = %q, want empty", tasks[1].EpicID)
	}
}
```

Note: `initTestRepo` and `writeHarnessConfig` are helpers from the existing
e2e test file. If `writeHarnessConfig` doesn't exist, use the same setup
pattern as `TestE2EWorkflow`:

```go
func writeHarnessConfig(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "Makefile"),
		[]byte("test:\n\t@echo all tests pass\n"), 0644)
	cmd := exec.Command("git", "add", "Makefile")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add makefile")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	RunInit(dir)
	cmd = exec.Command("git", "add", ".harness.yaml", ".gitignore")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "init harness")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ -run TestCmdPlan -v`
Expected: FAIL — `CmdPlan` is undefined.

- [ ] **Step 3: Implement cmd_plan.go**

Create `internal/cmd_plan.go`:

```go
package internal

import "fmt"

func (a *App) CmdPlan(description, epicID string) error {
	// Validate epic if provided
	if epicID != "" {
		e, err := a.DB.GetEpic(epicID)
		if err != nil {
			return err
		}
		if e.Status == "closed" {
			fmt.Printf("Warning: epic %s is closed\n", epicID)
		}
	}

	task, err := a.DB.CreatePlannedTask(description, epicID)
	if err != nil {
		return err
	}

	epicInfo := ""
	if epicID != "" {
		epicInfo = fmt.Sprintf(", %s", epicID)
	}
	fmt.Printf("Created %s: %q (PLANNED%s)\n", task.ID, description, epicInfo)
	fmt.Printf("\nNext: harness start %s\n", task.ID)
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ -run TestCmdPlan -v`
Expected: PASS

- [ ] **Step 5: Write test for CmdStart**

Add to `internal/e2e_test.go`:

```go
func TestCmdStart(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Plan a task
	err = app.CmdPlan("implement feature X", "")
	if err != nil {
		t.Fatal("plan:", err)
	}

	tasks, _ := app.DB.ListTasks()
	taskID := tasks[0].ID

	// Start it
	err = app.CmdStart(taskID, "")
	if err != nil {
		t.Fatal("start:", err)
	}

	// Verify transition
	task, _ := app.DB.GetTask(taskID)
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}
	if task.Branch == "" {
		t.Error("branch should be set after start")
	}
	if task.WorktreePath == "" {
		t.Error("worktree_path should be set after start")
	}

	// Verify git branch exists
	branches, _ := app.Git.ListBranches()
	found := false
	for _, b := range branches {
		if b == task.Branch {
			found = true
		}
	}
	if !found {
		t.Errorf("branch %q not found in git", task.Branch)
	}

	// Verify worktree exists
	if _, err := os.Stat(task.WorktreePath); os.IsNotExist(err) {
		t.Errorf("worktree %q does not exist", task.WorktreePath)
	}
}

func TestCmdStartNotPlanned(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Create a task via CmdNew (already IN_PROGRESS)
	err = app.CmdNew("already started", "")
	if err != nil {
		t.Fatal(err)
	}
	tasks, _ := app.DB.ListTasks()

	// Try to start it — should fail
	err = app.CmdStart(tasks[0].ID, "")
	if err == nil {
		t.Fatal("expected error starting non-PLANNED task")
	}
}
```

Note: `ListBranches` may not exist on GitClient. If not, check with
`git branch --list` or just verify the worktree directory exists (which
implicitly means the branch exists).

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/ -run TestCmdStart -v`
Expected: FAIL — `CmdStart` is undefined.

- [ ] **Step 7: Implement cmd_start.go**

Create `internal/cmd_start.go`:

```go
package internal

import (
	"fmt"
	"path/filepath"
)

func (a *App) CmdStart(taskID, baseBranch string) error {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return err
	}

	if task.State != StatePlanned {
		return fmt.Errorf("task %s is %s, not PLANNED", taskID, task.State)
	}

	if baseBranch == "" {
		baseBranch = a.Config.MainBranch
	}

	// Check for blockers (warn but proceed)
	blockers, _ := a.DB.GetBlockers(taskID)
	for _, b := range blockers {
		blocker, err := a.DB.GetTask(b.FromTaskID)
		if err != nil {
			continue
		}
		if blocker.State != StateMerged && blocker.State != StateClosed {
			fmt.Printf("Warning: blocked by %s %q (%s)\n",
				blocker.ID, blocker.Description, blocker.State)
		}
	}

	// Detect parent task from base branch
	var parentTaskID string
	if parentTask, err := a.DB.GetTaskByBranch(baseBranch); err == nil {
		parentTaskID = parentTask.ID
	}

	branch := Slugify(task.Description)
	worktreePath := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, branch)

	if err := a.Git.CreateBranch(branch, baseBranch); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	if err := a.Git.CreateWorktree(branch, worktreePath); err != nil {
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating worktree: %w", err)
	}

	if err := a.DB.StartTask(taskID, branch, worktreePath, baseBranch, parentTaskID); err != nil {
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("updating task: %w", err)
	}

	fmt.Printf("Started %s: %q\n", taskID, task.Description)
	fmt.Printf("Branch: %s\n", branch)
	baseInfo := baseBranch
	if parentTaskID != "" {
		baseInfo = fmt.Sprintf("%s (%s)", baseBranch, parentTaskID)
	}
	fmt.Printf("Base: %s\n", baseInfo)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("\nNext: implement the changes in %s/\n", worktreePath)
	fmt.Println("      then run: harness check")
	return nil
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/ -run TestCmdStart -v`
Expected: PASS

- [ ] **Step 9: Write test for CmdReady**

Add to `internal/e2e_test.go`:

```go
func TestCmdReady(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Plan three tasks
	app.CmdPlan("task A", "")
	app.CmdPlan("task B", "")
	app.CmdPlan("task C", "")

	tasks, _ := app.DB.ListTasks()
	taskA, taskB, taskC := tasks[0], tasks[1], tasks[2]

	// A blocks B, B blocks C
	app.DB.AddDependency(taskA.ID, taskB.ID, "blocks")
	app.DB.AddDependency(taskB.ID, taskC.ID, "blocks")

	// Ready should return only A
	ready, blocked, err := app.ComputeReady()
	if err != nil {
		t.Fatal("ready:", err)
	}
	if len(ready) != 1 || ready[0].ID != taskA.ID {
		t.Errorf("ready = %v, want [%s]", readyIDs(ready), taskA.ID)
	}
	if len(blocked) != 2 {
		t.Errorf("blocked = %v, want 2 tasks", readyIDs(blocked))
	}
}

func readyIDs(tasks []Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `go test ./internal/ -run TestCmdReady -v`
Expected: FAIL — `ComputeReady` is undefined.

- [ ] **Step 11: Implement cmd_ready.go**

Create `internal/cmd_ready.go`:

```go
package internal

import "fmt"

// ComputeReady returns (ready, blocked) lists of PLANNED tasks.
// A task is ready if all its "blocks" dependencies are MERGED or CLOSED.
func (a *App) ComputeReady() (ready []Task, blocked []Task, err error) {
	tasks, err := a.DB.ListTasks()
	if err != nil {
		return nil, nil, err
	}

	for _, t := range tasks {
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

func (a *App) CmdReady() error {
	ready, blocked, err := a.ComputeReady()
	if err != nil {
		return err
	}

	if len(ready) == 0 && len(blocked) == 0 {
		fmt.Println("No planned tasks. Run: harness plan \"description\" --epic <id>")
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
			blockerIDs := make([]string, 0, len(blockers))
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
```

Import `"strings"` at the top of the file.

- [ ] **Step 12: Run test to verify it passes**

Run: `go test ./internal/ -run TestCmdReady -v`
Expected: PASS

- [ ] **Step 13: Run full test suite**

Run: `go test ./internal/ -v`
Expected: All tests pass.

- [ ] **Step 14: Commit**

```bash
git add internal/cmd_plan.go internal/cmd_start.go internal/cmd_ready.go internal/e2e_test.go
git commit -m "plan, start, ready commands"
```
