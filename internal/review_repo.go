package internal

import (
	"database/sql"
	"time"
)

type Review struct {
	ID        int
	TaskID    string
	EpicID    string
	Decision  string
	Comment   string
	CommitSHA string
	CreatedAt time.Time
}

func (db *DB) RecordReview(taskID, decision, comment, commitSHA string) error {
	_, err := db.conn.Exec(
		`INSERT INTO reviews (task_id, decision, comment, commit_sha) VALUES (?, ?, ?, ?)`,
		taskID, decision, comment, commitSHA,
	)
	return err
}

func (db *DB) GetReviews(taskID string) ([]Review, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, COALESCE(epic_id, ''), decision, comment, commit_sha, created_at
		 FROM reviews WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.Decision, &r.Comment, &r.CommitSHA, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}

func (db *DB) GetLatestApproval(taskID string) (*Review, error) {
	row := db.conn.QueryRow(
		`SELECT id, task_id, COALESCE(epic_id, ''), decision, comment, commit_sha, created_at
		 FROM reviews WHERE task_id = ? AND decision = 'approve'
		 ORDER BY id DESC LIMIT 1`, taskID,
	)
	var r Review
	err := row.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.Decision, &r.Comment, &r.CommitSHA, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// --- Epic review methods ---

func (db *DB) RecordEpicReview(epicID, decision, comment, commitSHA string) error {
	_, err := db.conn.Exec(
		`INSERT INTO reviews (epic_id, decision, comment, commit_sha) VALUES (?, ?, ?, ?)`,
		epicID, decision, comment, commitSHA,
	)
	return err
}

func (db *DB) GetEpicReviews(epicID string) ([]Review, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(task_id, ''), epic_id, decision, comment, commit_sha, created_at
		 FROM reviews WHERE epic_id = ? ORDER BY created_at ASC`, epicID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.Decision, &r.Comment, &r.CommitSHA, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}

func (db *DB) GetLatestEpicApproval(epicID string) (*Review, error) {
	row := db.conn.QueryRow(
		`SELECT id, COALESCE(task_id, ''), epic_id, decision, comment, commit_sha, created_at
		 FROM reviews WHERE epic_id = ? AND decision = 'approve'
		 ORDER BY id DESC LIMIT 1`, epicID,
	)
	var r Review
	err := row.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.Decision, &r.Comment, &r.CommitSHA, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
