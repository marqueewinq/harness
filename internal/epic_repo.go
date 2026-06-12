package internal

import (
	"database/sql"
	"fmt"
	"time"
)

type Epic struct {
	ID          string
	ProjectID   string
	Title       string
	Description string
	Status      string
	Branch      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (db *DB) CreateEpic(title, description, projectID, branch string) (*Epic, error) {
	if _, err := db.GetProject(projectID); err != nil {
		return nil, fmt.Errorf("project %q not found", projectID)
	}
	id, err := db.nextEpicID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var branchVal interface{}
	if branch != "" {
		branchVal = branch
	}
	_, err = db.conn.Exec(
		`INSERT INTO epics (id, project_id, title, description, status, branch, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'open', ?, ?, ?)`,
		id, projectID, title, description, branchVal, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating epic: %w", err)
	}
	epic := &Epic{ID: id, ProjectID: projectID, Title: title, Description: description, Status: "open", Branch: branch, CreatedAt: now, UpdatedAt: now}
	db.RecordEpicTransition(id, "", "open")
	return epic, nil
}

func (db *DB) nextEpicID() (string, error) {
	var maxID sql.NullString
	err := db.conn.QueryRow(`SELECT MAX(id) FROM epics`).Scan(&maxID)
	if err != nil || !maxID.Valid {
		return "epic-001", nil
	}
	var num int
	fmt.Sscanf(maxID.String, "epic-%d", &num)
	return fmt.Sprintf("epic-%03d", num+1), nil
}

func (db *DB) GetEpic(id string) (*Epic, error) {
	e := &Epic{}
	var branch sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, project_id, title, description, status, branch, created_at, updated_at FROM epics WHERE id = ?`, id,
	).Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &e.Status, &branch, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("epic %q not found: %w", id, err)
	}
	e.Branch = branch.String
	return e, nil
}

func (db *DB) ListEpics() ([]Epic, error) {
	rows, err := db.conn.Query(`SELECT id, project_id, title, description, status, branch, created_at, updated_at FROM epics ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var epics []Epic
	for rows.Next() {
		var e Epic
		var branch sql.NullString
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &e.Status, &branch, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Branch = branch.String
		epics = append(epics, e)
	}
	return epics, nil
}

func (db *DB) ListEpicsByProject(projectID string) ([]Epic, error) {
	rows, err := db.conn.Query(
		`SELECT id, project_id, title, description, status, branch, created_at, updated_at FROM epics WHERE project_id = ? ORDER BY created_at ASC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var epics []Epic
	for rows.Next() {
		var e Epic
		var branch sql.NullString
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &e.Status, &branch, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Branch = branch.String
		epics = append(epics, e)
	}
	return epics, nil
}

func (db *DB) UpdateEpicStatus(id, status string) error {
	var current string
	if err := db.conn.QueryRow(`SELECT status FROM epics WHERE id = ?`, id).Scan(&current); err != nil {
		return fmt.Errorf("reading current status for epic %s: %w", id, err)
	}
	_, err := db.conn.Exec(`UPDATE epics SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now(), id)
	if err != nil {
		return err
	}
	return db.RecordEpicTransition(id, current, status)
}

func (db *DB) UpdateEpic(id, title, description, projectID string) error {
	_, err := db.conn.Exec(
		`UPDATE epics SET title = ?, description = ?, project_id = ?, updated_at = ? WHERE id = ?`,
		title, description, projectID, time.Now(), id,
	)
	return err
}

func (db *DB) UpdateEpicBranch(id, branch string) error {
	var branchVal interface{}
	if branch != "" {
		branchVal = branch
	}
	_, err := db.conn.Exec(
		`UPDATE epics SET branch = ?, updated_at = ? WHERE id = ?`,
		branchVal, time.Now(), id,
	)
	return err
}
