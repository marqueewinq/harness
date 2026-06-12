### Task 3: Project Model + CRUD + CLI

**Files:**
- Create: `internal/project_repo.go`
- Create: `internal/cmd_project.go`
- Modify: `main.go`

---

- [ ] **Step 1: Write test for project CRUD**

Add to `internal/db_test.go`:

```go
func TestProjectCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create
	p, err := db.CreateProject("Auth redesign", "Redesign the auth system")
	if err != nil {
		t.Fatal("create:", err)
	}
	if p.ID != "proj-001" {
		t.Errorf("id = %q, want proj-001", p.ID)
	}
	if p.Status != "open" {
		t.Errorf("status = %q, want open", p.Status)
	}

	// Get
	p2, err := db.GetProject("proj-001")
	if err != nil {
		t.Fatal("get:", err)
	}
	if p2.Title != "Auth redesign" {
		t.Errorf("title = %q, want Auth redesign", p2.Title)
	}

	// List
	db.CreateProject("Second project", "")
	projects, err := db.ListProjects()
	if err != nil {
		t.Fatal("list:", err)
	}
	if len(projects) != 2 {
		t.Fatalf("len = %d, want 2", len(projects))
	}

	// Update status
	err = db.UpdateProjectStatus("proj-001", "closed")
	if err != nil {
		t.Fatal("update:", err)
	}
	p3, _ := db.GetProject("proj-001")
	if p3.Status != "closed" {
		t.Errorf("status = %q, want closed", p3.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ -run TestProjectCRUD -v`
Expected: FAIL — `CreateProject` is undefined.

- [ ] **Step 3: Implement project_repo.go**

Create `internal/project_repo.go`:

```go
package internal

import (
	"fmt"
	"time"
)

type Project struct {
	ID          string
	Title       string
	Description string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (db *DB) CreateProject(title, description string) (*Project, error) {
	id, err := db.nextProjectID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	_, err = db.conn.Exec(
		`INSERT INTO projects (id, title, description, status, created_at, updated_at)
		 VALUES (?, ?, ?, 'open', ?, ?)`,
		id, title, description, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating project: %w", err)
	}
	return &Project{
		ID: id, Title: title, Description: description,
		Status: "open", CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) nextProjectID() (string, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM projects`).Scan(&count)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("proj-%03d", count+1), nil
}

func (db *DB) GetProject(id string) (*Project, error) {
	p := &Project{}
	err := db.conn.QueryRow(
		`SELECT id, title, description, status, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Title, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("project %q not found: %w", id, err)
	}
	return p, nil
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, description, status, created_at, updated_at
		 FROM projects ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Title, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (db *DB) UpdateProjectStatus(id, status string) error {
	_, err := db.conn.Exec(
		`UPDATE projects SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), id,
	)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ -run TestProjectCRUD -v`
Expected: PASS

- [ ] **Step 5: Implement cmd_project.go**

Create `internal/cmd_project.go`:

```go
package internal

import "fmt"

func (a *App) CmdProjectNew(title, description string) error {
	p, err := a.DB.CreateProject(title, description)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s: %q\n", p.ID, p.Title)
	return nil
}

func (a *App) CmdProjectList() error {
	projects, err := a.DB.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		fmt.Println("No projects. Run: harness project new \"title\"")
		return nil
	}
	fmt.Printf("%-10s %-8s %s\n", "ID", "Status", "Title")
	for _, p := range projects {
		// Count epics and tasks for progress
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
			progress = fmt.Sprintf(" (%d/%d tasks done)", doneTasks, totalTasks)
		}
		fmt.Printf("%-10s %-8s %s%s\n", p.ID, p.Status, p.Title, progress)
	}
	return nil
}

func (a *App) CmdProjectStatus(projectID string) error {
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	fmt.Printf("Project: %s %q\n", p.ID, p.Title)
	fmt.Printf("Status: %s\n", p.Status)
	if p.Description != "" {
		fmt.Printf("Description: %s\n", p.Description)
	}

	epics, _ := a.DB.ListEpicsByProject(p.ID)
	if len(epics) == 0 {
		fmt.Println("\nNo epics. Run: harness epic new \"title\" --project " + p.ID)
		return nil
	}

	fmt.Println("\nEpics:")
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
		fmt.Printf("  %-10s %-8s %s%s\n", e.ID, e.Status, e.Title, progress)
		for _, t := range tasks {
			fmt.Printf("    %-10s %-20s %s\n", t.ID, t.State, t.Description)
		}
	}
	return nil
}

func (a *App) CmdProjectClose(projectID string) error {
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}

	// Warn about open tasks
	epics, _ := a.DB.ListEpicsByProject(p.ID)
	var openCount int
	for _, e := range epics {
		tasks, _ := a.DB.ListTasksByEpic(e.ID)
		for _, t := range tasks {
			if t.State != StateMerged && t.State != StateClosed {
				openCount++
			}
		}
	}
	if openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this project\n", openCount)
	}

	if err := a.DB.UpdateProjectStatus(projectID, "closed"); err != nil {
		return err
	}
	fmt.Printf("Closed %s: %q\n", p.ID, p.Title)
	return nil
}
```

Note: `CmdProjectList` and `CmdProjectStatus` reference `ListEpicsByProject`
and `ListTasksByEpic` which will be created in Task 4 (Epic CRUD). This file
won't compile until Task 4 is done. If you prefer, implement the project
commands without the epic/task progress counts first, then add them in Task 4.

Alternative: stub the functions now and fill them in Task 4. The simpler path:
implement `CmdProjectNew` and `CmdProjectClose` now (they don't need epics),
defer `CmdProjectList` and `CmdProjectStatus` to Task 4.

Recommended approach: implement all four functions now. They'll compile after
Task 4 stubs are in place. Skip running the full suite until Task 4 is done.

- [ ] **Step 6: Run project CRUD test**

Run: `go test ./internal/ -run TestProjectCRUD -v`
Expected: PASS (repo layer works; cmd layer not tested yet)

- [ ] **Step 7: Commit**

```bash
git add internal/project_repo.go internal/cmd_project.go internal/db_test.go
git commit -m "project model + CRUD + CLI commands"
```
