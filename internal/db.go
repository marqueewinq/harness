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
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
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

func (db *DB) nextID(table, prefix string, width int) (string, error) {
	var maxID sql.NullString
	err := db.conn.QueryRow(fmt.Sprintf(`SELECT MAX(id) FROM %s`, table)).Scan(&maxID)
	if err != nil || !maxID.Valid {
		return fmt.Sprintf("%s-%0*d", prefix, width, 1), nil
	}
	var num int
	fmt.Sscanf(maxID.String, prefix+"-%d", &num)
	return fmt.Sprintf("%s-%0*d", prefix, width, num+1), nil
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
