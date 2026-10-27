package internal

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type dbExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

func (db *DB) WithTx(fn func(tx *sql.Tx) error) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

type DB struct {
	conn *sql.DB
}

func OpenDB(path string) (*DB, error) {
	// Several agents share one DB: WAL lets readers proceed during a write,
	// busy_timeout waits on locks instead of failing, and immediate tx locking
	// avoids SQLITE_BUSY on read->write lock upgrades inside WithTx.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_txlock=immediate"
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := createSchema(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func nextID(ex dbExecer, table, prefix string, width int) (string, error) {
	var maxNum int
	err := ex.QueryRow(
		fmt.Sprintf(`SELECT COALESCE(MAX(CAST(SUBSTR(id, ?) AS INTEGER)), 0) FROM %s WHERE id LIKE ?`, table),
		len(prefix)+2, prefix+"-%",
	).Scan(&maxNum)
	if err != nil {
		return "", fmt.Errorf("computing next %s id: %w", prefix, err)
	}
	return fmt.Sprintf("%s-%0*d", prefix, width, maxNum+1), nil
}

func createSchema(conn *sql.DB) error {
	_, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`)
	if err != nil {
		return err
	}

	var version int
	err = conn.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil {
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
	if version < 3 {
		if err := migrateV3(conn); err != nil {
			return fmt.Errorf("migration v3: %w", err)
		}
	}
	if version < 4 {
		if err := migrateV4(conn); err != nil {
			return fmt.Errorf("migration v4: %w", err)
		}
	}
	if version < 5 {
		if err := migrateV5(conn); err != nil {
			return fmt.Errorf("migration v5: %w", err)
		}
	}
	if version < 6 {
		if err := migrateV6(conn); err != nil {
			return fmt.Errorf("migration v6: %w", err)
		}
	}
	if version < 7 {
		if err := migrateV7(conn); err != nil {
			return fmt.Errorf("migration v7: %w", err)
		}
	}
	if version < 8 {
		if err := migrateV8(conn); err != nil {
			return fmt.Errorf("migration v8: %w", err)
		}
	}
	if version < 9 {
		if err := migrateV9(conn); err != nil {
			return fmt.Errorf("migration v9: %w", err)
		}
	}

	if version < 10 {
		if err := migrateV10(conn); err != nil {
			return fmt.Errorf("migration v10: %w", err)
		}
	}
	if version < 11 {
		if err := migrateV11(conn); err != nil {
			return fmt.Errorf("migration v11: %w", err)
		}
	}
	if version < 12 {
		if err := migrateV12(conn); err != nil {
			return fmt.Errorf("migration v12: %w", err)
		}
	}
	if version < 13 {
		if err := migrateV13(conn); err != nil {
			return fmt.Errorf("migration v13: %w", err)
		}
	}
	if version < 14 {
		if err := migrateV14(conn); err != nil {
			return fmt.Errorf("migration v14: %w", err)
		}
	}

	return nil
}

// notes is free-form text for humans, kept out of reviewer prompts. v3 once
// added tasks.notes and v6 dropped it, so the column is added only if absent.
// subject_edits is the audit log of title/description/notes edits; it is
// separate from the state transition tables so an edit never reads as a state
// change (the dashboard derives "age in state" from transitions).
func migrateV14(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"tasks", "epics"} {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'notes'`, table).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := tx.Exec(`ALTER TABLE ` + table + ` ADD COLUMN notes TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS subject_edits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject_id TEXT NOT NULL,
			fields TEXT NOT NULL,
			edited_by TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_subject_edits_subject ON subject_edits (subject_id, id)`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (14)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// note records why a transition skipped a gate (e.g. a merge without review).
func migrateV13(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`ALTER TABLE task_state_transitions ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE epic_state_transitions ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (13)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Existing tasks and epics keep today's review gate (default) with no
// recorded setter.
func migrateV12(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var stmts []string
	for _, table := range []string{"tasks", "epics"} {
		stmts = append(stmts,
			`ALTER TABLE `+table+` ADD COLUMN review_type TEXT NOT NULL DEFAULT 'default'`,
			`ALTER TABLE `+table+` ADD COLUMN review_type_set_by TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE `+table+` ADD COLUMN review_type_set_at TIMESTAMP NULL`,
		)
	}
	stmts = append(stmts,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (12)`,
	)
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// The dashboard reads the latest run group of every task on each refresh.
func migrateV11(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_check_runs_task_group ON check_runs (task_id, run_group)`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (11)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// resolution tells a verdict apart from a superseded or failed-closed request;
// pre-existing rows keep an empty resolution and reviewer_outcome (no recorded run).
func migrateV10(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`ALTER TABLE review_requests ADD COLUMN resolution TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_pid INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_owner_pid INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_started_unix INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_heartbeat_unix INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_finished_unix INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_exit_code INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE review_requests ADD COLUMN reviewer_outcome TEXT NOT NULL DEFAULT ''`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (10)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Pre-existing queue rows get pid 0 and heartbeat 0, so they read as stale
// and are reclaimed instead of blocking the queue forever.
func migrateV9(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`ALTER TABLE merge_queue ADD COLUMN owner_pid INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE merge_queue ADD COLUMN heartbeat_unix INTEGER NOT NULL DEFAULT 0`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (9)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func setVersion(conn *sql.DB, v int) error {
	_, err := conn.Exec("DELETE FROM schema_version")
	if err != nil {
		return err
	}
	_, err = conn.Exec("INSERT INTO schema_version (version) VALUES (?)", v)
	return err
}

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
	// For existing DBs: silently apply ALTERs (fail if columns exist already)
	conn.Exec(`ALTER TABLE tasks ADD COLUMN base_branch TEXT NOT NULL DEFAULT ''`)
	conn.Exec(`ALTER TABLE tasks ADD COLUMN parent_task_id TEXT DEFAULT NULL`)
	conn.Exec(`ALTER TABLE reviews ADD COLUMN commit_sha TEXT NOT NULL DEFAULT ''`)
	return setVersion(conn, 1)
}

func migrateV2(conn *sql.DB) error {
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

	// Add epic_id to tasks (safe to re-run via ALTER)
	conn.Exec(`ALTER TABLE tasks ADD COLUMN epic_id TEXT DEFAULT NULL`)

	// Rebuild tasks table to make branch/worktree_path nullable
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

func migrateV3(conn *sql.DB) error {
	conn.Exec(`ALTER TABLE tasks ADD COLUMN notes TEXT NOT NULL DEFAULT ''`)
	return setVersion(conn, 3)
}

func migrateV4(conn *sql.DB) error {
	// Add branch column to epics
	conn.Exec(`ALTER TABLE epics ADD COLUMN branch TEXT DEFAULT NULL`)

	// Rebuild reviews table to add epic_id and make task_id nullable
	rebuild := `
	CREATE TABLE IF NOT EXISTS reviews_new (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT DEFAULT NULL,
		epic_id TEXT DEFAULT NULL,
		decision TEXT NOT NULL,
		comment TEXT NOT NULL DEFAULT '',
		commit_sha TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	INSERT OR IGNORE INTO reviews_new (id, task_id, decision, comment, commit_sha, created_at)
		SELECT id, task_id, decision, comment, commit_sha, created_at
		FROM reviews;
	DROP TABLE reviews;
	ALTER TABLE reviews_new RENAME TO reviews;
	`
	if _, err := conn.Exec(rebuild); err != nil {
		return err
	}
	return setVersion(conn, 4)
}

func migrateV5(conn *sql.DB) error {
	tables := `
	CREATE TABLE IF NOT EXISTS task_state_transitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		from_state TEXT NOT NULL DEFAULT '',
		to_state TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS epic_state_transitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		epic_id TEXT NOT NULL REFERENCES epics(id),
		from_state TEXT NOT NULL DEFAULT '',
		to_state TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS project_state_transitions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		project_id TEXT NOT NULL REFERENCES projects(id),
		from_state TEXT NOT NULL DEFAULT '',
		to_state TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := conn.Exec(tables); err != nil {
		return err
	}
	conn.Exec(`ALTER TABLE check_runs ADD COLUMN commit_sha TEXT NOT NULL DEFAULT ''`)
	return setVersion(conn, 5)
}

func migrateV6(conn *sql.DB) error {
	rebuild := `
	CREATE TABLE IF NOT EXISTS tasks_new (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
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
		SELECT id, description, notes, branch, worktree_path, state, base_branch,
		       parent_task_id, epic_id, created_at, updated_at
		FROM tasks;
	DROP TABLE tasks;
	ALTER TABLE tasks_new RENAME TO tasks;
	`
	if _, err := conn.Exec(rebuild); err != nil {
		return err
	}
	return setVersion(conn, 6)
}

func migrateV7(conn *sql.DB) error {
	_, err := conn.Exec(`
	CREATE TABLE IF NOT EXISTS review_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT DEFAULT NULL,
		epic_id TEXT DEFAULT NULL,
		commit_sha TEXT NOT NULL,
		resolved_at TIMESTAMP DEFAULT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`)
	if err != nil {
		return err
	}
	return setVersion(conn, 7)
}

func migrateV8(conn *sql.DB) error {
	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`ALTER TABLE review_requests ADD COLUMN token_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE reviews ADD COLUMN reviewer TEXT NOT NULL DEFAULT ''`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (8)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
