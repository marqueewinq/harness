package internal

import (
	"database/sql"
	"fmt"
	"time"
)

type Task struct {
	ID           string
	Description  string
	Notes        string
	Branch       string
	WorktreePath string
	State        TaskState
	BaseBranch   string
	ParentTaskID string
	EpicID       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (db *DB) CreateTask(description, notes, branch, worktreePath, baseBranch, parentTaskID, epicID string) (*Task, error) {
	id, err := db.nextTaskID()
	if err != nil {
		return nil, err
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
	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, description, notes, branch, worktree_path, state, base_branch, parent_task_id, epic_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, description, notes, branch, worktreePath, string(StateInProgress), baseBranch, parentVal, epicVal, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	return &Task{
		ID: id, Description: description, Notes: notes, Branch: branch,
		WorktreePath: worktreePath, State: StateInProgress,
		BaseBranch: baseBranch, ParentTaskID: parentTaskID, EpicID: epicID,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) nextTaskID() (string, error) {
	var maxID sql.NullString
	err := db.conn.QueryRow(`SELECT MAX(id) FROM tasks`).Scan(&maxID)
	if err != nil || !maxID.Valid {
		return "task-001", nil
	}
	var num int
	fmt.Sscanf(maxID.String, "task-%d", &num)
	return fmt.Sprintf("task-%03d", num+1), nil
}

func (db *DB) GetTask(id string) (*Task, error) {
	t := &Task{}
	var parentID, epicID, branch, worktreePath sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, description, notes, branch, worktree_path, state, base_branch,
		        parent_task_id, epic_id, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Description, &t.Notes, &branch, &worktreePath, &t.State,
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

func (db *DB) GetTaskByBranch(branch string) (*Task, error) {
	t := &Task{}
	var parentID, epicID, branchVal, worktreePath sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, description, notes, branch, worktree_path, state, base_branch,
		        parent_task_id, epic_id, created_at, updated_at
		 FROM tasks WHERE branch = ?`, branch,
	).Scan(&t.ID, &t.Description, &t.Notes, &branchVal, &worktreePath, &t.State,
		&t.BaseBranch, &parentID, &epicID, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("task with branch %q not found: %w", branch, err)
	}
	t.Branch = branchVal.String
	t.WorktreePath = worktreePath.String
	t.ParentTaskID = parentID.String
	t.EpicID = epicID.String
	return t, nil
}

func (db *DB) ListTasks() ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT id, description, notes, branch, worktree_path, state, base_branch,
		        parent_task_id, epic_id, created_at, updated_at
		 FROM tasks ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var parentID, epicID, branch, worktreePath sql.NullString
		if err := rows.Scan(&t.ID, &t.Description, &t.Notes, &branch, &worktreePath, &t.State,
			&t.BaseBranch, &parentID, &epicID, &t.CreatedAt, &t.UpdatedAt); err != nil {
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

func (db *DB) GetSubtasks(parentTaskID string) ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT id, description, notes, branch, worktree_path, state, base_branch,
		        parent_task_id, epic_id, created_at, updated_at
		 FROM tasks WHERE parent_task_id = ? ORDER BY created_at ASC`, parentTaskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		var parentID, epicID, branch, worktreePath sql.NullString
		if err := rows.Scan(&t.ID, &t.Description, &t.Notes, &branch, &worktreePath, &t.State,
			&t.BaseBranch, &parentID, &epicID, &t.CreatedAt, &t.UpdatedAt); err != nil {
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

func (db *DB) ListTasksByEpic(epicID string) ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT id, description, notes, branch, worktree_path, state, base_branch,
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
		var parentID, epicIDN, branch, worktreePath sql.NullString
		if err := rows.Scan(&t.ID, &t.Description, &t.Notes, &branch, &worktreePath,
			&t.State, &t.BaseBranch, &parentID, &epicIDN,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Branch = branch.String
		t.WorktreePath = worktreePath.String
		t.ParentTaskID = parentID.String
		t.EpicID = epicIDN.String
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

func (db *DB) CreatePlannedTask(description, notes, epicID string) (*Task, error) {
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
		`INSERT INTO tasks (id, description, notes, branch, worktree_path, state, epic_id, created_at, updated_at)
		 VALUES (?, ?, ?, NULL, NULL, ?, ?, ?, ?)`,
		id, description, notes, string(StatePlanned), epicVal, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating planned task: %w", err)
	}
	return &Task{
		ID: id, Description: description, Notes: notes,
		State: StatePlanned, EpicID: epicID,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) UpdatePlannedTask(id, description, notes, epicID string) error {
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
		`UPDATE tasks SET description = ?, notes = ?, epic_id = ?, updated_at = ? WHERE id = ?`,
		description, notes, epicVal, time.Now(), id,
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

	// Remove dependencies first
	_, err = db.conn.Exec(`DELETE FROM task_dependencies WHERE from_task_id = ? OR to_task_id = ?`, id, id)
	if err != nil {
		return fmt.Errorf("removing dependencies: %w", err)
	}
	_, err = db.conn.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}
	return nil
}

func (db *DB) StartTask(id, branch, worktreePath, baseBranch, parentTaskID string) error {
	var parentVal interface{}
	if parentTaskID != "" {
		parentVal = parentTaskID
	}
	_, err := db.conn.Exec(
		`UPDATE tasks SET branch = ?, worktree_path = ?, state = ?, base_branch = ?,
		        parent_task_id = ?, updated_at = ?
		 WHERE id = ? AND state = ?`,
		branch, worktreePath, string(StateInProgress), baseBranch,
		parentVal, time.Now(), id, string(StatePlanned),
	)
	return err
}
