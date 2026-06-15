package internal

import (
	"database/sql"
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
	id, err := db.nextID("projects", "proj", 3)
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
	proj := &Project{ID: id, Title: title, Description: description, Status: "open", CreatedAt: now, UpdatedAt: now}
	db.RecordProjectTransition(id, "", "open")
	return proj, nil
}

func (db *DB) GetProject(id string) (*Project, error) {
	p := &Project{}
	err := db.conn.QueryRow(
		`SELECT id, title, description, status, created_at, updated_at FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Title, &p.Description, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("project %q not found: %w", id, err)
	}
	return p, nil
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.conn.Query(`SELECT id, title, description, status, created_at, updated_at FROM projects ORDER BY created_at ASC`)
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
	return db.WithTx(func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRow(`SELECT status FROM projects WHERE id = ?`, id).Scan(&current); err != nil {
			return fmt.Errorf("reading current status for project %s: %w", id, err)
		}
		_, err := tx.Exec(`UPDATE projects SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now(), id)
		if err != nil {
			return err
		}
		return recordTransition(tx, "project_state_transitions", "project_id", id, current, status)
	})
}

func (db *DB) UpdateProject(id, title, description string) error {
	_, err := db.conn.Exec(
		`UPDATE projects SET title = ?, description = ?, updated_at = ? WHERE id = ?`,
		title, description, time.Now(), id,
	)
	return err
}

func (db *DB) DeleteProject(id string) error {
	// Check for epics
	epics, err := db.ListEpicsByProject(id)
	if err != nil {
		return err
	}
	if len(epics) > 0 {
		return fmt.Errorf("project %s has %d epics; delete them first", id, len(epics))
	}
	_, err = db.conn.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}
