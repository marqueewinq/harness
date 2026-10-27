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
	Status      EpicStatus
	Branch      string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	ReviewType      ReviewType
	ReviewTypeSetBy string
	ReviewTypeSetAt time.Time // zero when never set (pre-review-types rows)
}

const epicColumns = `id, project_id, title, description, status, branch, created_at, updated_at, review_type, review_type_set_by, review_type_set_at`

func scanEpic(s rowScanner) (*Epic, error) {
	e := &Epic{}
	var branch sql.NullString
	var status string
	var reviewSetAt sql.NullTime
	err := s.Scan(&e.ID, &e.ProjectID, &e.Title, &e.Description, &status, &branch, &e.CreatedAt, &e.UpdatedAt,
		&e.ReviewType, &e.ReviewTypeSetBy, &reviewSetAt)
	if err != nil {
		return nil, err
	}
	e.ReviewTypeSetAt = reviewSetAt.Time
	e.Status = EpicStatus(status)
	e.Branch = branch.String
	return e, nil
}

func (db *DB) CreateEpic(title, description, projectID, branch string, review ReviewSetting) (*Epic, error) {
	if _, err := db.GetProject(projectID); err != nil {
		return nil, fmt.Errorf("project %q not found", projectID)
	}
	var epic *Epic
	err := db.WithTx(func(tx *sql.Tx) error {
		id, err := nextID(tx, "epics", "epic", 3)
		if err != nil {
			return err
		}
		now := time.Now()
		var branchVal interface{}
		if branch != "" {
			branchVal = branch
		}
		reviewType, reviewSetBy, reviewSetAt := review.columnValues(now)
		_, err = tx.Exec(
			`INSERT INTO epics (id, project_id, title, description, status, branch, created_at, updated_at,
			                    review_type, review_type_set_by, review_type_set_at)
			 VALUES (?, ?, ?, ?, 'open', ?, ?, ?, ?, ?, ?)`,
			id, projectID, title, description, branchVal, now, now,
			reviewType, reviewSetBy, reviewSetAt,
		)
		if err != nil {
			return fmt.Errorf("creating epic: %w", err)
		}
		if err := recordTransition(tx, "epic_state_transitions", "epic_id", id, "", string(EpicStatusOpen)); err != nil {
			return err
		}
		epic = &Epic{ID: id, ProjectID: projectID, Title: title, Description: description, Status: EpicStatusOpen, Branch: branch, CreatedAt: now, UpdatedAt: now}
		epic.setReview(reviewType, reviewSetBy, reviewSetAt)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return epic, nil
}

func (db *DB) GetEpic(id string) (*Epic, error) {
	e, err := scanEpic(db.conn.QueryRow(
		`SELECT `+epicColumns+` FROM epics WHERE id = ?`, id,
	))
	if err != nil {
		return nil, fmt.Errorf("epic %q not found: %w", id, err)
	}
	return e, nil
}

func (db *DB) ListEpics() ([]Epic, error) {
	rows, err := db.conn.Query(`SELECT ` + epicColumns + ` FROM epics ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var epics []Epic
	for rows.Next() {
		e, err := scanEpic(rows)
		if err != nil {
			return nil, err
		}
		epics = append(epics, *e)
	}
	return epics, nil
}

func (db *DB) ListEpicsByProject(projectID string) ([]Epic, error) {
	rows, err := db.conn.Query(
		`SELECT `+epicColumns+` FROM epics WHERE project_id = ? ORDER BY created_at ASC`, projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var epics []Epic
	for rows.Next() {
		e, err := scanEpic(rows)
		if err != nil {
			return nil, err
		}
		epics = append(epics, *e)
	}
	return epics, nil
}

func (db *DB) UpdateEpicStatus(id string, status EpicStatus) error {
	return db.UpdateEpicStatusNote(id, status, "")
}

func (db *DB) UpdateEpicStatusNote(id string, status EpicStatus, note string) error {
	return db.WithTx(func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRow(`SELECT status FROM epics WHERE id = ?`, id).Scan(&current); err != nil {
			return fmt.Errorf("reading current status for epic %s: %w", id, err)
		}
		_, err := tx.Exec(`UPDATE epics SET status = ?, updated_at = ? WHERE id = ?`, string(status), time.Now(), id)
		if err != nil {
			return err
		}
		return recordTransitionNote(tx, "epic_state_transitions", "epic_id", id, current, string(status), note)
	})
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
