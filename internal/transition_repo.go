package internal

import (
	"fmt"
	"time"
)

type TaskStateTransition struct {
	ID        int
	TaskID    string
	FromState string
	ToState   string
	CreatedAt time.Time
}

type EpicStateTransition struct {
	ID        int
	EpicID    string
	FromState string
	ToState   string
	CreatedAt time.Time
}

type ProjectStateTransition struct {
	ID        int
	ProjectID string
	FromState string
	ToState   string
	CreatedAt time.Time
}

func recordTransition(ex dbExecer, table, idCol, id, from, to string) error {
	query := fmt.Sprintf(
		`INSERT INTO %s (%s, from_state, to_state, created_at) VALUES (?, ?, ?, ?)`,
		table, idCol,
	)
	_, err := ex.Exec(query, id, from, to, time.Now())
	return err
}

func (db *DB) RecordTaskTransition(taskID, fromState, toState string) error {
	return recordTransition(db.conn, "task_state_transitions", "task_id", taskID, fromState, toState)
}

func (db *DB) GetTaskTransitions(taskID string) ([]TaskStateTransition, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, from_state, to_state, created_at
		 FROM task_state_transitions WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transitions []TaskStateTransition
	for rows.Next() {
		var t TaskStateTransition
		if err := rows.Scan(&t.ID, &t.TaskID, &t.FromState, &t.ToState, &t.CreatedAt); err != nil {
			return nil, err
		}
		transitions = append(transitions, t)
	}
	return transitions, nil
}

func (db *DB) RecordEpicTransition(epicID, fromState, toState string) error {
	return recordTransition(db.conn, "epic_state_transitions", "epic_id", epicID, fromState, toState)
}

func (db *DB) GetEpicTransitions(epicID string) ([]EpicStateTransition, error) {
	rows, err := db.conn.Query(
		`SELECT id, epic_id, from_state, to_state, created_at
		 FROM epic_state_transitions WHERE epic_id = ? ORDER BY created_at ASC`, epicID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transitions []EpicStateTransition
	for rows.Next() {
		var t EpicStateTransition
		if err := rows.Scan(&t.ID, &t.EpicID, &t.FromState, &t.ToState, &t.CreatedAt); err != nil {
			return nil, err
		}
		transitions = append(transitions, t)
	}
	return transitions, nil
}

func (db *DB) RecordProjectTransition(projectID, fromState, toState string) error {
	return recordTransition(db.conn, "project_state_transitions", "project_id", projectID, fromState, toState)
}

func (db *DB) GetProjectTransitions(projectID string) ([]ProjectStateTransition, error) {
	rows, err := db.conn.Query(
		`SELECT id, project_id, from_state, to_state, created_at
		 FROM project_state_transitions WHERE project_id = ? ORDER BY created_at ASC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transitions []ProjectStateTransition
	for rows.Next() {
		var t ProjectStateTransition
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.FromState, &t.ToState, &t.CreatedAt); err != nil {
			return nil, err
		}
		transitions = append(transitions, t)
	}
	return transitions, nil
}
