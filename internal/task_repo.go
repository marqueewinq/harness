package internal

import (
	"database/sql"
	"fmt"
	"time"
)

type Task struct {
	ID           string
	Title        string
	Description  string
	Branch       string
	WorktreePath string
	State        TaskState
	BaseBranch   string
	ParentTaskID string
	EpicID       string
	CreatedAt    time.Time
	UpdatedAt    time.Time

	ReviewType      ReviewType
	ReviewTypeSetBy string
	ReviewTypeSetAt time.Time // zero when never set (pre-review-types rows)
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

const taskColumns = `id, title, description, branch, worktree_path, state, base_branch, parent_task_id, epic_id, created_at, updated_at, review_type, review_type_set_by, review_type_set_at`

func scanTask(s rowScanner) (*Task, error) {
	t := &Task{}
	var parentID, epicID, branch, worktreePath sql.NullString
	var reviewSetAt sql.NullTime
	err := s.Scan(&t.ID, &t.Title, &t.Description, &branch, &worktreePath,
		&t.State, &t.BaseBranch, &parentID, &epicID, &t.CreatedAt, &t.UpdatedAt,
		&t.ReviewType, &t.ReviewTypeSetBy, &reviewSetAt)
	if err != nil {
		return nil, err
	}
	t.ReviewTypeSetAt = reviewSetAt.Time
	t.Branch = branch.String
	t.WorktreePath = worktreePath.String
	t.ParentTaskID = parentID.String
	t.EpicID = epicID.String
	return t, nil
}

func (db *DB) CreateTask(title, description, branch, worktreePath, baseBranch, parentTaskID, epicID string) (*Task, error) {
	var task *Task
	err := db.WithTx(func(tx *sql.Tx) error {
		id, err := nextID(tx, "tasks", "task", 3)
		if err != nil {
			return err
		}
		now := time.Now()
		var parentVal interface{}
		if parentTaskID != "" {
			parentVal = parentTaskID
		}
		var epicVal interface{}
		if epicID != "" {
			epicVal = epicID
		}
		_, err = tx.Exec(
			`INSERT INTO tasks (id, title, description, branch, worktree_path, state, base_branch, parent_task_id, epic_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, title, description, branch, worktreePath, string(StateInProgress), baseBranch, parentVal, epicVal, now, now,
		)
		if err != nil {
			return fmt.Errorf("creating task: %w", err)
		}
		if err := recordTransition(tx, "task_state_transitions", "task_id", id, "", string(StateInProgress)); err != nil {
			return err
		}
		task = &Task{
			ID: id, Title: title, Description: description, Branch: branch,
			WorktreePath: worktreePath, State: StateInProgress,
			BaseBranch: baseBranch, ParentTaskID: parentTaskID, EpicID: epicID,
			CreatedAt: now, UpdatedAt: now, ReviewType: ReviewDefault,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (db *DB) GetTask(id string) (*Task, error) {
	t, err := scanTask(db.conn.QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id,
	))
	if err != nil {
		return nil, fmt.Errorf("task %q not found: %w", id, err)
	}
	return t, nil
}

func (db *DB) GetTaskByBranch(branch string) (*Task, error) {
	t, err := scanTask(db.conn.QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE branch = ?`, branch,
	))
	if err != nil {
		return nil, fmt.Errorf("task with branch %q not found: %w", branch, err)
	}
	return t, nil
}

func (db *DB) ListTasks() ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT ` + taskColumns + ` FROM tasks ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

func (db *DB) GetSubtasks(parentTaskID string) ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT `+taskColumns+` FROM tasks WHERE parent_task_id = ? ORDER BY created_at ASC`, parentTaskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

func (db *DB) ListTasksByEpic(epicID string) ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT `+taskColumns+` FROM tasks WHERE epic_id = ? ORDER BY created_at ASC`, epicID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

func (db *DB) UpdateTaskState(id string, state TaskState) error {
	return db.WithTx(func(tx *sql.Tx) error { return updateTaskStateTx(tx, id, state) })
}

func updateTaskStateTx(tx *sql.Tx, id string, state TaskState) error {
	var current string
	if err := tx.QueryRow(`SELECT state FROM tasks WHERE id = ?`, id).Scan(&current); err != nil {
		return fmt.Errorf("reading current state for %s: %w", id, err)
	}
	if !ValidTransition(TaskState(current), state) {
		return fmt.Errorf("invalid transition %s -> %s for task %s", current, state, id)
	}
	_, err := tx.Exec(
		`UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), time.Now(), id,
	)
	if err != nil {
		return err
	}
	return recordTransition(tx, "task_state_transitions", "task_id", id, current, string(state))
}

func (db *DB) CreatePlannedTask(title, description, epicID string) (*Task, error) {
	var task *Task
	err := db.WithTx(func(tx *sql.Tx) error {
		id, err := nextID(tx, "tasks", "task", 3)
		if err != nil {
			return err
		}
		now := time.Now()
		var epicVal interface{}
		if epicID != "" {
			epicVal = epicID
		}
		_, err = tx.Exec(
			`INSERT INTO tasks (id, title, description, branch, worktree_path, state, epic_id, created_at, updated_at)
			 VALUES (?, ?, ?, NULL, NULL, ?, ?, ?, ?)`,
			id, title, description, string(StatePlanned), epicVal, now, now,
		)
		if err != nil {
			return fmt.Errorf("creating planned task: %w", err)
		}
		if err := recordTransition(tx, "task_state_transitions", "task_id", id, "", string(StatePlanned)); err != nil {
			return err
		}
		task = &Task{
			ID: id, Title: title, Description: description,
			State: StatePlanned, EpicID: epicID,
			CreatedAt: now, UpdatedAt: now, ReviewType: ReviewDefault,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (db *DB) UpdatePlannedTask(id, title, description, epicID string) error {
	// Only allow editing PLANNED tasks
	task, err := db.GetTask(id)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("can only edit PLANNED tasks (task %s is %s)", id, task.State)
	}

	var epicVal interface{}
	if epicID != "" {
		epicVal = epicID
	}
	_, err = db.conn.Exec(
		`UPDATE tasks SET title = ?, description = ?, epic_id = ?, updated_at = ? WHERE id = ?`,
		title, description, epicVal, time.Now(), id,
	)
	return err
}

func (db *DB) DeletePlannedTask(id string) error {
	task, err := db.GetTask(id)
	if err != nil {
		return err
	}
	if task.State != StatePlanned {
		return fmt.Errorf("can only delete PLANNED tasks (task %s is %s)", id, task.State)
	}

	return db.WithTx(func(tx *sql.Tx) error {
		// Remove dependencies first
		_, err := tx.Exec(`DELETE FROM task_dependencies WHERE from_task_id = ? OR to_task_id = ?`, id, id)
		if err != nil {
			return fmt.Errorf("removing dependencies: %w", err)
		}
		_, err = tx.Exec(`DELETE FROM tasks WHERE id = ?`, id)
		if err != nil {
			return fmt.Errorf("deleting task: %w", err)
		}
		return nil
	})
}

func (db *DB) StartTask(id, branch, worktreePath, baseBranch, parentTaskID string) error {
	return db.WithTx(func(tx *sql.Tx) error {
		var current TaskState
		if err := tx.QueryRow(`SELECT state FROM tasks WHERE id = ?`, id).Scan(&current); err != nil {
			return fmt.Errorf("reading current state for %s: %w", id, err)
		}
		if !ValidTransition(current, StateInProgress) {
			return fmt.Errorf("invalid transition %s -> %s for task %s", current, StateInProgress, id)
		}
		var parentVal interface{}
		if parentTaskID != "" {
			parentVal = parentTaskID
		}
		_, err := tx.Exec(
			`UPDATE tasks SET branch = ?, worktree_path = ?, state = ?, base_branch = ?,
			        parent_task_id = ?, updated_at = ?
			 WHERE id = ?`,
			branch, worktreePath, string(StateInProgress), baseBranch,
			parentVal, time.Now(), id,
		)
		if err != nil {
			return err
		}
		return recordTransition(tx, "task_state_transitions", "task_id", id, string(current), string(StateInProgress))
	})
}
