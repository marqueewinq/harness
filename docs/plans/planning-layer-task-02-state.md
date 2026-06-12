### Task 2: PLANNED State + Task Model Changes

**Files:**
- Modify: `internal/state.go`
- Modify: `internal/state_test.go`
- Modify: `internal/task_repo.go`

Add the PLANNED state to the state machine and extend the Task model with
`EpicID` and nullable `Branch`/`WorktreePath`.

---

- [ ] **Step 1: Write test for PLANNED state transitions**

Add to `internal/state_test.go`:

```go
func TestPlannedTransitions(t *testing.T) {
	// PLANNED -> IN_PROGRESS: valid
	if !ValidTransition(StatePlanned, StateInProgress) {
		t.Error("PLANNED -> IN_PROGRESS should be valid")
	}
	// PLANNED -> CLOSED: valid
	if !ValidTransition(StatePlanned, StateClosed) {
		t.Error("PLANNED -> CLOSED should be valid")
	}
	// PLANNED -> CHECKS_RUNNING: invalid
	if ValidTransition(StatePlanned, StateChecksRunning) {
		t.Error("PLANNED -> CHECKS_RUNNING should be invalid")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ -run TestPlannedTransitions -v`
Expected: FAIL — `StatePlanned` is undefined.

- [ ] **Step 3: Add PLANNED state to state.go**

In `internal/state.go`, add the constant and transition:

```go
const (
	StatePlanned         TaskState = "PLANNED"
	// ...existing constants...
)

var validTransitions = map[TaskState][]TaskState{
	StatePlanned:         {StateInProgress, StateClosed},
	// ...existing transitions...
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ -run TestPlannedTransitions -v`
Expected: PASS

- [ ] **Step 5: Update Task struct in task_repo.go**

Add `EpicID` field and handle nullable `Branch`/`WorktreePath` using
`sql.NullString`:

```go
type Task struct {
	ID           string
	Description  string
	Branch       string    // empty when PLANNED
	WorktreePath string    // empty when PLANNED
	State        TaskState
	BaseBranch   string
	ParentTaskID string
	EpicID       string    // empty when no epic
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
```

Update all scan functions (`GetTask`, `GetTaskByBranch`, `ListTasks`,
`GetSubtasks`) to handle nullable columns. The pattern — use `sql.NullString`
for scanning, then assign `.String` (which is empty string when NULL):

```go
func (db *DB) GetTask(id string) (*Task, error) {
	t := &Task{}
	var parentID, epicID, branch, worktreePath sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, description, branch, worktree_path, state, base_branch,
		        parent_task_id, epic_id, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Description, &branch, &worktreePath, &t.State,
		&t.BaseBranch, &parentID, &epicID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("task %q not found: %w", id, err)
	}
	t.Branch = branch.String
	t.WorktreePath = worktreePath.String
	t.ParentTaskID = parentID.String
	t.EpicID = epicID.String
	return t, nil
}
```

Apply the same pattern to `GetTaskByBranch`, `ListTasks`, `GetSubtasks`.

- [ ] **Step 6: Add CreatePlannedTask function**

Add to `internal/task_repo.go`:

```go
func (db *DB) CreatePlannedTask(description, epicID string) (*Task, error) {
	id, err := db.nextTaskID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var epicVal interface{}
	if epicID != "" {
		epicVal = epicID
	}
	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, description, branch, worktree_path, state, epic_id, created_at, updated_at)
		 VALUES (?, ?, NULL, NULL, ?, ?, ?, ?)`,
		id, description, string(StatePlanned), epicVal, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating planned task: %w", err)
	}

	return &Task{
		ID: id, Description: description,
		State: StatePlanned, EpicID: epicID,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}
```

- [ ] **Step 7: Add StartTask function**

This fills in `branch` and `worktree_path` when transitioning from PLANNED:

```go
func (db *DB) StartTask(id, branch, worktreePath, baseBranch, parentTaskID string) error {
	_, err := db.conn.Exec(
		`UPDATE tasks SET branch = ?, worktree_path = ?, state = ?, base_branch = ?,
		        parent_task_id = ?, updated_at = ?
		 WHERE id = ? AND state = ?`,
		branch, worktreePath, string(StateInProgress), baseBranch,
		sql.NullString{String: parentTaskID, Valid: parentTaskID != ""},
		time.Now(), id, string(StatePlanned),
	)
	return err
}
```

- [ ] **Step 8: Run full test suite**

Run: `go test ./internal/ -v`
Expected: All tests pass. Existing tests use `CreateTask` which still creates
at IN_PROGRESS with non-null branch — unchanged.

- [ ] **Step 9: Commit**

```bash
git add internal/state.go internal/state_test.go internal/task_repo.go
git commit -m "add PLANNED state + task model changes"
```
