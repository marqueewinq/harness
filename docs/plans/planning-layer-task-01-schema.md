### Task 1: Schema Versioning + DB Migrations

**Files:**
- Modify: `internal/db.go`

This task introduces schema versioning and creates the new tables. The task
table rebuild (making branch/worktree_path nullable) is also here.

---

- [ ] **Step 1: Write the schema version test**

Add to `internal/db_test.go`:

```go
func TestSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var version int
	err = db.conn.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatal("schema_version table missing:", err)
	}
	if version < 2 {
		t.Errorf("schema version = %d, want >= 2", version)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ -run TestSchemaVersion -v`
Expected: FAIL — `schema_version` table does not exist.

- [ ] **Step 3: Implement schema versioning in db.go**

Replace the current `createSchema` function. The new approach:

1. Create `schema_version` table if not exists
2. Read current version (0 if table is empty)
3. Run migrations sequentially based on version

```go
func createSchema(conn *sql.DB) error {
	// Bootstrap: version tracking
	_, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	if err != nil {
		return err
	}

	var version int
	err = conn.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil {
		// No row yet — fresh DB
		version = 0
	}

	if version < 1 {
		if err := migrateV1(conn); err != nil {
			return fmt.Errorf("migration v1: %w", err)
		}
	}
	if version < 2 {
		if err := migrateV2(conn); err != nil {
			return fmt.Errorf("migration v2: %w", err)
		}
	}

	return nil
}

func setVersion(conn *sql.DB, v int) error {
	_, err := conn.Exec("DELETE FROM schema_version")
	if err != nil {
		return err
	}
	_, err = conn.Exec("INSERT INTO schema_version (version) VALUES (?)", v)
	return err
}
```

Migration v1 captures the existing schema (the original tables + previous
ALTER TABLE migrations). This ensures both fresh DBs and existing DBs converge
to the same state:

```go
func migrateV1(conn *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		branch TEXT UNIQUE,
		worktree_path TEXT,
		state TEXT NOT NULL,
		base_branch TEXT NOT NULL DEFAULT '',
		parent_task_id TEXT DEFAULT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS check_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		check_name TEXT NOT NULL,
		exit_code INTEGER NOT NULL,
		stdout TEXT NOT NULL DEFAULT '',
		stderr TEXT NOT NULL DEFAULT '',
		started_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NOT NULL,
		run_group INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS reviews (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		decision TEXT NOT NULL,
		comment TEXT NOT NULL DEFAULT '',
		commit_sha TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS merge_queue (
		position INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
		queued_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := conn.Exec(schema); err != nil {
		return err
	}

	// For existing DBs that ran the old migration path: apply ALTERs silently
	conn.Exec(`ALTER TABLE tasks ADD COLUMN base_branch TEXT NOT NULL DEFAULT ''`)
	conn.Exec(`ALTER TABLE tasks ADD COLUMN parent_task_id TEXT DEFAULT NULL`)
	conn.Exec(`ALTER TABLE reviews ADD COLUMN commit_sha TEXT NOT NULL DEFAULT ''`)

	return setVersion(conn, 1)
}
```

Migration v2 adds planning layer tables and rebuilds the tasks table to make
`branch` and `worktree_path` nullable:

```go
func migrateV2(conn *sql.DB) error {
	// New tables
	newTables := `
	CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS epics (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL REFERENCES projects(id),
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'open',
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);

	CREATE TABLE IF NOT EXISTS task_dependencies (
		from_task_id TEXT NOT NULL REFERENCES tasks(id),
		to_task_id TEXT NOT NULL REFERENCES tasks(id),
		dep_type TEXT NOT NULL DEFAULT 'blocks',
		PRIMARY KEY (from_task_id, to_task_id)
	);
	`
	if _, err := conn.Exec(newTables); err != nil {
		return err
	}

	// Add epic_id to tasks (safe to re-run)
	conn.Exec(`ALTER TABLE tasks ADD COLUMN epic_id TEXT DEFAULT NULL`)

	// Rebuild tasks table to make branch/worktree_path nullable
	// (SQLite cannot ALTER column constraints)
	rebuild := `
	CREATE TABLE IF NOT EXISTS tasks_new (
		id TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		branch TEXT UNIQUE,
		worktree_path TEXT,
		state TEXT NOT NULL,
		base_branch TEXT NOT NULL DEFAULT '',
		parent_task_id TEXT DEFAULT NULL,
		epic_id TEXT DEFAULT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	INSERT OR IGNORE INTO tasks_new
		SELECT id, description, branch, worktree_path, state, base_branch,
		       parent_task_id, epic_id, created_at, updated_at
		FROM tasks;

	DROP TABLE tasks;

	ALTER TABLE tasks_new RENAME TO tasks;
	`
	if _, err := conn.Exec(rebuild); err != nil {
		return err
	}

	return setVersion(conn, 2)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ -run TestSchemaVersion -v`
Expected: PASS

- [ ] **Step 5: Verify existing tests still pass**

Run: `go test ./internal/ -v`
Expected: All existing tests pass (the v1 migration captures the old schema).

- [ ] **Step 6: Write test for new tables existence**

Add to `internal/db_test.go`:

```go
func TestPlanningTablesExist(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tables := []string{"projects", "epics", "task_dependencies"}
	for _, table := range tables {
		var name string
		err := db.conn.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/ -run TestPlanningTablesExist -v`
Expected: PASS (tables created by migrateV2)

- [ ] **Step 8: Write test for nullable branch on tasks**

```go
func TestTaskNullableBranch(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert a task with NULL branch (simulating PLANNED state)
	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, description, branch, worktree_path, state, created_at, updated_at)
		 VALUES ('task-001', 'test', NULL, NULL, 'PLANNED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	)
	if err != nil {
		t.Fatal("inserting task with NULL branch:", err)
	}

	// Insert a second task with NULL branch — should not violate UNIQUE
	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, description, branch, worktree_path, state, created_at, updated_at)
		 VALUES ('task-002', 'test2', NULL, NULL, 'PLANNED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	)
	if err != nil {
		t.Fatal("second NULL branch should be allowed:", err)
	}
}
```

- [ ] **Step 9: Run test to verify it passes**

Run: `go test ./internal/ -run TestTaskNullableBranch -v`
Expected: PASS

- [ ] **Step 10: Run full test suite**

Run: `go test ./internal/ -v`
Expected: All tests pass.

- [ ] **Step 11: Commit**

```bash
git add internal/db.go internal/db_test.go
git commit -m "schema versioning + planning layer tables"
```
