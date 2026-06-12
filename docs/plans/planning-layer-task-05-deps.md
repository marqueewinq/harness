### Task 5: Dependency System + CLI

**Files:**
- Create: `internal/dep_repo.go`
- Create: `internal/dep_repo_test.go`
- Create: `internal/cmd_dep.go`

---

- [ ] **Step 1: Write test for basic dependency CRUD**

Create `internal/dep_repo_test.go`:

```go
package internal

import (
	"path/filepath"
	"testing"
)

func TestDepCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create two planned tasks
	t1, _ := db.CreatePlannedTask("task one", "")
	t2, _ := db.CreatePlannedTask("task two", "")

	// Add dependency
	err = db.AddDependency(t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal("add:", err)
	}

	// List deps
	deps, err := db.GetBlockers(t2.ID)
	if err != nil {
		t.Fatal("get blockers:", err)
	}
	if len(deps) != 1 || deps[0].FromTaskID != t1.ID {
		t.Errorf("blockers = %v, want [%s]", deps, t1.ID)
	}

	// Remove
	err = db.RemoveDependency(t1.ID, t2.ID)
	if err != nil {
		t.Fatal("remove:", err)
	}
	deps, _ = db.GetBlockers(t2.ID)
	if len(deps) != 0 {
		t.Errorf("blockers after remove = %v, want empty", deps)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ -run TestDepCRUD -v`
Expected: FAIL — `AddDependency` is undefined.

- [ ] **Step 3: Implement dep_repo.go (CRUD only)**

Create `internal/dep_repo.go`:

```go
package internal

import "fmt"

type TaskDependency struct {
	FromTaskID string
	ToTaskID   string
	DepType    string // "blocks" or "related"
}

func (db *DB) AddDependency(fromTaskID, toTaskID, depType string) error {
	if fromTaskID == toTaskID {
		return fmt.Errorf("a task cannot depend on itself")
	}

	// Verify both tasks exist
	if _, err := db.GetTask(fromTaskID); err != nil {
		return err
	}
	if _, err := db.GetTask(toTaskID); err != nil {
		return err
	}

	// Idempotent: INSERT OR IGNORE
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO task_dependencies (from_task_id, to_task_id, dep_type)
		 VALUES (?, ?, ?)`,
		fromTaskID, toTaskID, depType,
	)
	if err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	return nil
}

func (db *DB) RemoveDependency(fromTaskID, toTaskID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM task_dependencies WHERE from_task_id = ? AND to_task_id = ?`,
		fromTaskID, toTaskID,
	)
	return err
}

// GetBlockers returns all "blocks" dependencies where toTaskID is the blocked task.
func (db *DB) GetBlockers(toTaskID string) ([]TaskDependency, error) {
	rows, err := db.conn.Query(
		`SELECT from_task_id, to_task_id, dep_type
		 FROM task_dependencies WHERE to_task_id = ? AND dep_type = 'blocks'`, toTaskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []TaskDependency
	for rows.Next() {
		var d TaskDependency
		if err := rows.Scan(&d.FromTaskID, &d.ToTaskID, &d.DepType); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}

// GetDependencies returns all dependencies (both directions) for a task.
func (db *DB) GetDependencies(taskID string) ([]TaskDependency, error) {
	rows, err := db.conn.Query(
		`SELECT from_task_id, to_task_id, dep_type
		 FROM task_dependencies WHERE from_task_id = ? OR to_task_id = ?`,
		taskID, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []TaskDependency
	for rows.Next() {
		var d TaskDependency
		if err := rows.Scan(&d.FromTaskID, &d.ToTaskID, &d.DepType); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ -run TestDepCRUD -v`
Expected: PASS

- [ ] **Step 5: Write test for self-dependency rejection**

Add to `internal/dep_repo_test.go`:

```go
func TestDepSelfDependency(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "")
	err = db.AddDependency(t1.ID, t1.ID, "blocks")
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/ -run TestDepSelfDependency -v`
Expected: PASS (already handled in AddDependency)

- [ ] **Step 7: Write test for cycle detection**

Add to `internal/dep_repo_test.go`:

```go
func TestDepCycleDetection(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "")
	t2, _ := db.CreatePlannedTask("task two", "")
	t3, _ := db.CreatePlannedTask("task three", "")

	// A blocks B, B blocks C — fine
	db.AddDependency(t1.ID, t2.ID, "blocks")
	db.AddDependency(t2.ID, t3.ID, "blocks")

	// C blocks A — would create cycle
	err = db.AddDependency(t3.ID, t1.ID, "blocks")
	if err == nil {
		t.Fatal("expected cycle detection error")
	}

	// "related" type should NOT trigger cycle detection
	err = db.AddDependency(t3.ID, t1.ID, "related")
	if err != nil {
		t.Fatal("related deps should allow cycles:", err)
	}
}

func TestDepIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "")
	t2, _ := db.CreatePlannedTask("task two", "")

	// Add twice — should succeed both times
	err = db.AddDependency(t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	err = db.AddDependency(t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal("duplicate should be idempotent:", err)
	}
}
```

- [ ] **Step 8: Run test to verify cycle test fails**

Run: `go test ./internal/ -run TestDepCycleDetection -v`
Expected: FAIL — no cycle detection yet.

- [ ] **Step 9: Add cycle detection to AddDependency**

Update `AddDependency` in `internal/dep_repo.go` to check for cycles on
`blocks` type:

```go
func (db *DB) AddDependency(fromTaskID, toTaskID, depType string) error {
	if fromTaskID == toTaskID {
		return fmt.Errorf("a task cannot depend on itself")
	}

	// Verify both tasks exist
	if _, err := db.GetTask(fromTaskID); err != nil {
		return err
	}
	if _, err := db.GetTask(toTaskID); err != nil {
		return err
	}

	// Cycle detection for "blocks" type only
	if depType == "blocks" {
		if err := db.detectCycle(fromTaskID, toTaskID); err != nil {
			return err
		}
	}

	// Idempotent: INSERT OR IGNORE
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO task_dependencies (from_task_id, to_task_id, dep_type)
		 VALUES (?, ?, ?)`,
		fromTaskID, toTaskID, depType,
	)
	if err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	return nil
}

// detectCycle checks if adding fromTaskID->toTaskID would create a cycle.
// It walks from fromTaskID backwards through the "blocks" graph to see if
// toTaskID is reachable (which would mean toTaskID -> ... -> fromTaskID exists).
func (db *DB) detectCycle(fromTaskID, toTaskID string) error {
	// BFS from toTaskID through "blocks" edges to see if we reach fromTaskID
	visited := map[string]bool{}
	queue := []string{toTaskID}

	// Also record the path for error message
	parent := map[string]string{}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if current == fromTaskID {
			// Build cycle path for error message
			path := []string{fromTaskID}
			node := fromTaskID
			for node != toTaskID {
				node = parent[node]
				path = append(path, node)
			}
			// Reverse
			for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
				path[i], path[j] = path[j], path[i]
			}
			return fmt.Errorf("circular dependency detected: %s -> %s",
				joinPath(path), fromTaskID)
		}

		if visited[current] {
			continue
		}
		visited[current] = true

		// Get all tasks that "current" blocks (current is from_task_id)
		rows, err := db.conn.Query(
			`SELECT to_task_id FROM task_dependencies
			 WHERE from_task_id = ? AND dep_type = 'blocks'`, current,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var next string
			rows.Scan(&next)
			if !visited[next] {
				parent[next] = current
				queue = append(queue, next)
			}
		}
		rows.Close()
	}

	return nil
}

func joinPath(path []string) string {
	result := path[0]
	for _, p := range path[1:] {
		result += " -> " + p
	}
	return result
}
```

The cycle detection walks forward from toTaskID through "blocks" edges. If
toTaskID (transitively) blocks fromTaskID, adding fromTaskID->toTaskID would
create a cycle.

Replace the `detectCycle` and `joinPath` functions above with this corrected
version:

```go
// detectCycle checks if adding "fromTaskID blocks toTaskID" would create a cycle.
// A cycle exists if toTaskID already transitively blocks fromTaskID.
func (db *DB) detectCycle(fromTaskID, toTaskID string) error {
	// BFS: start from toTaskID, follow "blocks" edges forward
	visited := map[string]bool{}
	queue := []string{toTaskID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		// "current blocks X" means edge current -> X
		rows, err := db.conn.Query(
			`SELECT to_task_id FROM task_dependencies
			 WHERE from_task_id = ? AND dep_type = 'blocks'`, current,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var next string
			rows.Scan(&next)
			if next == fromTaskID {
				rows.Close()
				return fmt.Errorf("circular dependency detected")
			}
			if !visited[next] {
				queue = append(queue, next)
			}
		}
		rows.Close()
	}

	return nil
}
```

Do NOT include the earlier `joinPath` or `parent` tracking code — only
implement this simplified version.

- [ ] **Step 10: Run cycle detection test**

Run: `go test ./internal/ -run "TestDepCycle|TestDepIdempotent" -v`
Expected: PASS

- [ ] **Step 11: Implement cmd_dep.go**

Create `internal/cmd_dep.go`:

```go
package internal

import "fmt"

func (a *App) CmdDepAdd(fromTaskID, toTaskID, depType string) error {
	if depType == "" {
		depType = "blocks"
	}
	if depType != "blocks" && depType != "related" {
		return fmt.Errorf("invalid dependency type %q (use blocks or related)", depType)
	}

	if err := a.DB.AddDependency(fromTaskID, toTaskID, depType); err != nil {
		return err
	}

	fromTask, _ := a.DB.GetTask(fromTaskID)
	toTask, _ := a.DB.GetTask(toTaskID)
	fmt.Printf("%s %q %s %s %q\n",
		fromTaskID, fromTask.Description, depType, toTaskID, toTask.Description)
	return nil
}

func (a *App) CmdDepRm(fromTaskID, toTaskID string) error {
	if err := a.DB.RemoveDependency(fromTaskID, toTaskID); err != nil {
		return err
	}
	fmt.Printf("Removed dependency %s -> %s\n", fromTaskID, toTaskID)
	return nil
}
```

- [ ] **Step 12: Run full test suite**

Run: `go test ./internal/ -v`
Expected: All tests pass.

- [ ] **Step 13: Commit**

```bash
git add internal/dep_repo.go internal/dep_repo_test.go internal/cmd_dep.go
git commit -m "dependency system with cycle detection"
```
