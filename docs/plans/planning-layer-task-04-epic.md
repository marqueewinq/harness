### Task 4: Epic Model + CRUD + CLI

**Files:**
- Create: `internal/epic_repo.go`
- Create: `internal/cmd_epic.go`

---

- [ ] **Step 1: Write test for epic CRUD**

Add to `internal/db_test.go`:

```go
func TestEpicCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create project first
	proj, err := db.CreateProject("Test project", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create epic
	e, err := db.CreateEpic("OAuth2 support", "", proj.ID)
	if err != nil {
		t.Fatal("create:", err)
	}
	if e.ID != "epic-001" {
		t.Errorf("id = %q, want epic-001", e.ID)
	}
	if e.ProjectID != proj.ID {
		t.Errorf("project_id = %q, want %q", e.ProjectID, proj.ID)
	}

	// Get
	e2, err := db.GetEpic("epic-001")
	if err != nil {
		t.Fatal("get:", err)
	}
	if e2.Title != "OAuth2 support" {
		t.Errorf("title = %q", e2.Title)
	}

	// List by project
	db.CreateEpic("Rate limiting", "", proj.ID)
	epics, err := db.ListEpicsByProject(proj.ID)
	if err != nil {
		t.Fatal("list:", err)
	}
	if len(epics) != 2 {
		t.Fatalf("len = %d, want 2", len(epics))
	}

	// List all
	all, err := db.ListEpics()
	if err != nil {
		t.Fatal("list all:", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}

	// Update status
	err = db.UpdateEpicStatus("epic-001", "closed")
	if err != nil {
		t.Fatal("update:", err)
	}
	e3, _ := db.GetEpic("epic-001")
	if e3.Status != "closed" {
		t.Errorf("status = %q, want closed", e3.Status)
	}
}

func TestEpicRequiresProject(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.CreateEpic("Orphan epic", "", "proj-999")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ -run TestEpicCRUD -v`
Expected: FAIL — `CreateEpic` is undefined.

- [ ] **Step 3: Implement epic_repo.go**

Create `internal/epic_repo.go`:

```go
package internal

import (
	"fmt"
	"time"
)

type Epic struct {
	ID          string
	ProjectID   string
	Title       string
	Description string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (db *DB) CreateEpic(title, description, projectID string) (*Epic, error) {
	// Verify project exists
	_, err := db.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project %q not found", projectID)
	}

	id, err := db.nextEpicID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_, err = db.conn.Exec(
		`INSERT INTO epics (id, project_id, title, description, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'open', ?, ?)`,
		id, projectID, title, description, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating epic: %w", err)
	}
	return &Epic{
		ID: id, ProjectID: projectID, Title: title, Description: description,
		Status: "open", CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) nextEpicID() (string, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM epics`).Scan(&count)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("epic-%03d", count+1), nil
}

func (db *DB) GetEpic(id string) (*Epic, error) {
	e := &Epic{}
	err := db.conn.QueryRow(
		`SELECT id, project_id, title, description, status, created_at, updated_at
		 FROM epics WHERE id = ?`, id,
	).Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("epic %q not found: %w", id, err)
	}
	return e, nil
}

func (db *DB) ListEpics() ([]Epic, error) {
	rows, err := db.conn.Query(
		`SELECT id, project_id, title, description, status, created_at, updated_at
		 FROM epics ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var epics []Epic
	for rows.Next() {
		var e Epic
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		epics = append(epics, e)
	}
	return epics, nil
}

func (db *DB) ListEpicsByProject(projectID string) ([]Epic, error) {
	rows, err := db.conn.Query(
		`SELECT id, project_id, title, description, status, created_at, updated_at
		 FROM epics WHERE project_id = ? ORDER BY created_at ASC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var epics []Epic
	for rows.Next() {
		var e Epic
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		epics = append(epics, e)
	}
	return epics, nil
}

func (db *DB) UpdateEpicStatus(id, status string) error {
	_, err := db.conn.Exec(
		`UPDATE epics SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), id,
	)
	return err
}
```

- [ ] **Step 4: Add ListTasksByEpic to task_repo.go**

Add to `internal/task_repo.go`:

```go
func (db *DB) ListTasksByEpic(epicID string) ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT id, description, branch, worktree_path, state, base_branch,
		        parent_task_id, epic_id, created_at, updated_at
		 FROM tasks WHERE epic_id = ? ORDER BY created_at ASC`, epicID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var parentID, epicID, branch, worktreePath sql.NullString
		if err := rows.Scan(&t.ID, &t.Description, &branch, &worktreePath,
			&t.State, &t.BaseBranch, &parentID, &epicID,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Branch = branch.String
		t.WorktreePath = worktreePath.String
		t.ParentTaskID = parentID.String
		t.EpicID = epicID.String
		tasks = append(tasks, t)
	}
	return tasks, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ -run "TestEpicCRUD|TestEpicRequiresProject" -v`
Expected: PASS

- [ ] **Step 6: Implement cmd_epic.go**

Create `internal/cmd_epic.go`:

```go
package internal

import "fmt"

func (a *App) CmdEpicNew(title, description, projectID string) error {
	if projectID == "" {
		return fmt.Errorf("--project is required")
	}

	// Warn if project is closed
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	if p.Status == "closed" {
		fmt.Printf("Warning: project %s is closed\n", projectID)
	}

	e, err := a.DB.CreateEpic(title, description, projectID)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s: %q (%s)\n", e.ID, e.Title, projectID)
	return nil
}

func (a *App) CmdEpicList(projectID string) error {
	var epics []Epic
	var err error
	if projectID != "" {
		epics, err = a.DB.ListEpicsByProject(projectID)
	} else {
		epics, err = a.DB.ListEpics()
	}
	if err != nil {
		return err
	}
	if len(epics) == 0 {
		fmt.Println("No epics. Run: harness epic new \"title\" --project <id>")
		return nil
	}
	fmt.Printf("%-10s %-10s %-8s %s\n", "ID", "Project", "Status", "Title")
	for _, e := range epics {
		tasks, _ := a.DB.ListTasksByEpic(e.ID)
		var done int
		for _, t := range tasks {
			if t.State == StateMerged || t.State == StateClosed {
				done++
			}
		}
		progress := ""
		if len(tasks) > 0 {
			progress = fmt.Sprintf(" (%d/%d)", done, len(tasks))
		}
		fmt.Printf("%-10s %-10s %-8s %s%s\n", e.ID, e.ProjectID, e.Status, e.Title, progress)
	}
	return nil
}

func (a *App) CmdEpicStatus(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	p, _ := a.DB.GetProject(e.ProjectID)

	fmt.Printf("Epic: %s %q\n", e.ID, e.Title)
	fmt.Printf("Project: %s %q\n", p.ID, p.Title)
	fmt.Printf("Status: %s\n", e.Status)
	if e.Description != "" {
		fmt.Printf("Description: %s\n", e.Description)
	}

	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	if len(tasks) == 0 {
		fmt.Printf("\nNo tasks. Run: harness plan \"description\" --epic %s\n", e.ID)
		return nil
	}

	fmt.Println("\nTasks:")
	for _, t := range tasks {
		fmt.Printf("  %-10s %-20s %s\n", t.ID, t.State, t.Description)
	}
	return nil
}

func (a *App) CmdEpicClose(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}

	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	var openCount int
	for _, t := range tasks {
		if t.State != StateMerged && t.State != StateClosed {
			openCount++
		}
	}
	if openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this epic\n", openCount)
	}

	if err := a.DB.UpdateEpicStatus(epicID, "closed"); err != nil {
		return err
	}
	fmt.Printf("Closed %s: %q\n", e.ID, e.Title)
	return nil
}
```

- [ ] **Step 7: Verify full test suite compiles and passes**

Run: `go test ./internal/ -v`
Expected: All tests pass. Task 3's cmd_project.go should now compile since
`ListEpicsByProject` and `ListTasksByEpic` are defined.

- [ ] **Step 8: Commit**

```bash
git add internal/epic_repo.go internal/cmd_epic.go internal/task_repo.go internal/db_test.go
git commit -m "epic model + CRUD + CLI commands"
```
