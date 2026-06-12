### Task 9: E2E Tests for Planning Workflow

**Files:**
- Modify: `internal/e2e_test.go`

This task adds comprehensive end-to-end tests covering the full planning
workflow: project creation -> epic creation -> task planning -> dependency
setup -> starting tasks -> full execution workflow -> verify hierarchy.

---

- [ ] **Step 1: Write E2E test for the full planning-to-execution flow**

Add to `internal/e2e_test.go`:

```go
func TestE2EPlanningWorkflow(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// === Phase 1: Planning ===

	// Create project
	err = app.CmdProjectNew("Auth redesign", "Redesign the auth system")
	if err != nil {
		t.Fatal("project new:", err)
	}
	projects, _ := app.DB.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	proj := projects[0]

	// Create epic
	err = app.CmdEpicNew("OAuth2 support", "", proj.ID)
	if err != nil {
		t.Fatal("epic new:", err)
	}
	epics, _ := app.DB.ListEpicsByProject(proj.ID)
	if len(epics) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(epics))
	}
	epic := epics[0]

	// Plan tasks
	err = app.CmdPlan("setup OAuth2 client", epic.ID)
	if err != nil {
		t.Fatal("plan 1:", err)
	}
	err = app.CmdPlan("implement token refresh", epic.ID)
	if err != nil {
		t.Fatal("plan 2:", err)
	}
	err = app.CmdPlan("integrate OAuth2 flow", epic.ID)
	if err != nil {
		t.Fatal("plan 3:", err)
	}

	tasks, _ := app.DB.ListTasksByEpic(epic.ID)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// All should be PLANNED
	for _, task := range tasks {
		if task.State != StatePlanned {
			t.Errorf("task %s: state = %s, want PLANNED", task.ID, task.State)
		}
		if task.Branch != "" {
			t.Errorf("task %s: branch should be empty, got %q", task.ID, task.Branch)
		}
	}

	taskA, taskB, taskC := tasks[0], tasks[1], tasks[2]

	// Add dependencies: A blocks B, B blocks C
	err = app.DB.AddDependency(taskA.ID, taskB.ID, "blocks")
	if err != nil {
		t.Fatal("dep add:", err)
	}
	err = app.DB.AddDependency(taskB.ID, taskC.ID, "blocks")
	if err != nil {
		t.Fatal("dep add:", err)
	}

	// Check ready: only A should be ready
	ready, blocked, _ := app.ComputeReady()
	if len(ready) != 1 || ready[0].ID != taskA.ID {
		t.Errorf("ready = %v, want [%s]", readyIDs(ready), taskA.ID)
	}
	if len(blocked) != 2 {
		t.Errorf("blocked count = %d, want 2", len(blocked))
	}

	// === Phase 2: Execute task A ===

	err = app.CmdStart(taskA.ID, "")
	if err != nil {
		t.Fatal("start:", err)
	}

	taskA, _ = app.DB.GetTask(taskA.ID)
	if taskA.State != StateInProgress {
		t.Errorf("task A state = %s, want IN_PROGRESS", taskA.State)
	}
	if taskA.Branch == "" {
		t.Error("task A branch should be set")
	}

	// Make a change in the worktree
	os.WriteFile(filepath.Join(taskA.WorktreePath, "oauth2.go"),
		[]byte("package main\n"), 0644)
	wtGit := NewGitClient(taskA.WorktreePath)
	runGit(t, taskA.WorktreePath, "add", "oauth2.go")
	runGit(t, taskA.WorktreePath, "commit", "-m", "add oauth2")
	_ = wtGit // suppress unused warning if needed

	// Run checks
	err = app.CmdCheck(taskA.ID)
	if err != nil {
		t.Fatal("check:", err)
	}

	// Request and approve review
	err = app.CmdRequestReview(taskA.ID)
	if err != nil {
		t.Fatal("request-review:", err)
	}
	err = app.CmdRecordReview(taskA.ID, "approve", "")
	if err != nil {
		t.Fatal("record-review:", err)
	}

	// Merge
	err = app.CmdMerge(taskA.ID)
	if err != nil {
		t.Fatal("merge:", err)
	}

	taskA, _ = app.DB.GetTask(taskA.ID)
	if taskA.State != StateMerged {
		t.Errorf("task A state = %s, want MERGED", taskA.State)
	}

	// === Phase 3: Verify B is now ready ===

	ready, blocked, _ = app.ComputeReady()
	if len(ready) != 1 || ready[0].ID != taskB.ID {
		t.Errorf("after merging A: ready = %v, want [%s]", readyIDs(ready), taskB.ID)
	}
	if len(blocked) != 1 || blocked[0].ID != taskC.ID {
		t.Errorf("after merging A: blocked = %v, want [%s]", readyIDs(blocked), taskC.ID)
	}

	// === Phase 4: Verify hierarchy displays work ===

	err = app.CmdList()
	if err != nil {
		t.Fatal("list:", err)
	}
	err = app.CmdProjectStatus(proj.ID)
	if err != nil {
		t.Fatal("project status:", err)
	}
	err = app.CmdEpicStatus(epic.ID)
	if err != nil {
		t.Fatal("epic status:", err)
	}
	err = app.CmdReady()
	if err != nil {
		t.Fatal("ready:", err)
	}
}

// runGit is a helper to run git commands in a directory
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
}
```

Note: `runGit` may already exist in e2e_test.go. If so, reuse it. If the
existing tests use inline `exec.Command` calls, this helper simplifies the new
test.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/ -run TestE2EPlanningWorkflow -v -timeout 60s`
Expected: PASS — the full planning-to-execution flow works end to end.

- [ ] **Step 3: Write E2E test for closing planned tasks**

```go
func TestE2EClosePlannedTask(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Plan and close
	app.CmdPlan("will not do this", "")
	tasks, _ := app.DB.ListTasks()
	taskID := tasks[0].ID

	err = app.CmdClose(taskID, false)
	if err != nil {
		t.Fatal("close planned:", err)
	}
	task, _ := app.DB.GetTask(taskID)
	if task.State != StateClosed {
		t.Errorf("state = %s, want CLOSED", task.State)
	}
}
```

- [ ] **Step 4: Write E2E test for blocked task start warning**

```go
func TestE2EStartBlockedTask(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Plan two tasks, A blocks B
	app.CmdPlan("task A", "")
	app.CmdPlan("task B", "")
	tasks, _ := app.DB.ListTasks()
	app.DB.AddDependency(tasks[0].ID, tasks[1].ID, "blocks")

	// Start B while A is still PLANNED — should warn but succeed
	err = app.CmdStart(tasks[1].ID, "")
	if err != nil {
		t.Fatal("start blocked task should succeed with warning:", err)
	}
	task, _ := app.DB.GetTask(tasks[1].ID)
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}
}
```

- [ ] **Step 5: Write E2E test for orphan planned tasks (no epic)**

```go
func TestE2EOrphanPlannedTask(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Plan without epic
	err = app.CmdPlan("orphan task", "")
	if err != nil {
		t.Fatal("plan orphan:", err)
	}

	tasks, _ := app.DB.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].EpicID != "" {
		t.Errorf("epic_id = %q, want empty", tasks[0].EpicID)
	}

	// Start it
	err = app.CmdStart(tasks[0].ID, "")
	if err != nil {
		t.Fatal("start orphan:", err)
	}

	task, _ := app.DB.GetTask(tasks[0].ID)
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}

	// List should show it under orphans section
	err = app.CmdList()
	if err != nil {
		t.Fatal("list:", err)
	}
}
```

- [ ] **Step 6: Write E2E test for mixed workflow (old new + planning)**

```go
func TestE2EMixedWorkflow(t *testing.T) {
	dir := initTestRepo(t)
	writeHarnessConfig(t, dir)
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// Create some tasks via old workflow
	err = app.CmdNew("old style task", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create some via planning
	proj, _ := app.DB.CreateProject("Test", "")
	epic, _ := app.DB.CreateEpic("Epic", "", proj.ID)
	app.CmdPlan("planned task", epic.ID)

	// List should show both
	err = app.CmdList()
	if err != nil {
		t.Fatal("list:", err)
	}

	// Both types of tasks coexist
	tasks, _ := app.DB.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}
```

- [ ] **Step 7: Run all new E2E tests**

Run: `go test ./internal/ -run "TestE2EPlanning|TestE2EClose|TestE2EStart|TestE2EOrphan|TestE2EMixed" -v -timeout 120s`
Expected: All pass.

- [ ] **Step 8: Run full test suite**

Run: `go test ./internal/ -v -timeout 120s`
Expected: All tests pass, including the original E2E tests.

- [ ] **Step 9: Commit**

```bash
git add internal/e2e_test.go
git commit -m "E2E tests for planning workflow"
```
