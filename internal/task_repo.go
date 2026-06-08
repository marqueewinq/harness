package internal

import (
	"fmt"
	"time"
)

type Task struct {
	ID           string
	Description  string
	Branch       string
	WorktreePath string
	State        TaskState
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (db *DB) CreateTask(description, branch, worktreePath string) (*Task, error) {
	id, err := db.nextTaskID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, description, branch, worktree_path, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, description, branch, worktreePath, string(StateInProgress), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	return &Task{
		ID: id, Description: description, Branch: branch,
		WorktreePath: worktreePath, State: StateInProgress,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) nextTaskID() (string, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task-%03d", count+1), nil
}

func (db *DB) GetTask(id string) (*Task, error) {
	t := &Task{}
	err := db.conn.QueryRow(
		`SELECT id, description, branch, worktree_path, state, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Description, &t.Branch, &t.WorktreePath, &t.State, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("task %q not found: %w", id, err)
	}
	return t, nil
}

func (db *DB) GetTaskByBranch(branch string) (*Task, error) {
	t := &Task{}
	err := db.conn.QueryRow(
		`SELECT id, description, branch, worktree_path, state, created_at, updated_at
		 FROM tasks WHERE branch = ?`, branch,
	).Scan(&t.ID, &t.Description, &t.Branch, &t.WorktreePath, &t.State, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("task with branch %q not found: %w", branch, err)
	}
	return t, nil
}

func (db *DB) ListTasks() ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT id, description, branch, worktree_path, state, created_at, updated_at
		 FROM tasks ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Description, &t.Branch, &t.WorktreePath, &t.State, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (db *DB) UpdateTaskState(id string, state TaskState) error {
	_, err := db.conn.Exec(
		`UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), time.Now(), id,
	)
	return err
}
