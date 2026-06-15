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
	Status      ProjectStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const projectColumns = `id, title, description, status, created_at, updated_at`

func scanProject(s rowScanner) (*Project, error) {
	p := &Project{}
	var status string
	err := s.Scan(&p.ID, &p.Title, &p.Description, &status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Status = ProjectStatus(status)
	return p, nil
}

func (db *DB) CreateProject(title, description string) (*Project, error) {
	var proj *Project
	err := db.WithTx(func(tx *sql.Tx) error {
		id, err := nextID(tx, "projects", "proj", 3)
		if err != nil {
			return err
		}
		now := time.Now()
		_, err = tx.Exec(
			`INSERT INTO projects (id, title, description, status, created_at, updated_at)
			 VALUES (?, ?, ?, 'open', ?, ?)`,
			id, title, description, now, now,
		)
		if err != nil {
			return fmt.Errorf("creating project: %w", err)
		}
		if err := recordTransition(tx, "project_state_transitions", "project_id", id, "", string(ProjectStatusOpen)); err != nil {
			return err
		}
		proj = &Project{ID: id, Title: title, Description: description, Status: ProjectStatusOpen, CreatedAt: now, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return proj, nil
}

func (db *DB) GetProject(id string) (*Project, error) {
	p, err := scanProject(db.conn.QueryRow(
		`SELECT `+projectColumns+` FROM projects WHERE id = ?`, id,
	))
	if err != nil {
		return nil, fmt.Errorf("project %q not found: %w", id, err)
	}
	return p, nil
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.conn.Query(`SELECT ` + projectColumns + ` FROM projects ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, *p)
	}
	return projects, nil
}

func (db *DB) UpdateProjectStatus(id string, status ProjectStatus) error {
	return db.WithTx(func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRow(`SELECT status FROM projects WHERE id = ?`, id).Scan(&current); err != nil {
			return fmt.Errorf("reading current status for project %s: %w", id, err)
		}
		_, err := tx.Exec(`UPDATE projects SET status = ?, updated_at = ? WHERE id = ?`, string(status), time.Now(), id)
		if err != nil {
			return err
		}
		return recordTransition(tx, "project_state_transitions", "project_id", id, current, string(status))
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
