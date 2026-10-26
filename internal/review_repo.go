package internal

import (
	"database/sql"
	"fmt"
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

func scanReview(s rowScanner) (*Review, error) {
	r := &Review{}
	err := s.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.Decision, &r.Comment, &r.CommitSHA, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
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
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, *r)
	}
	return reviews, nil
}

func (db *DB) GetLatestApproval(taskID string) (*Review, error) {
	r, err := scanReview(db.conn.QueryRow(
		`SELECT id, task_id, COALESCE(epic_id, ''), decision, comment, commit_sha, created_at
		 FROM reviews WHERE task_id = ? AND decision = 'approve'
		 ORDER BY id DESC LIMIT 1`, taskID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

type ReviewRequest struct {
	ID        int
	TaskID    string
	EpicID    string
	CommitSHA string
	Resolved  bool
	CreatedAt time.Time
}

// OpenReviewRequest supersedes any open request for the same task or epic.
// Exactly one of taskID, epicID must be non-empty.
func (db *DB) OpenReviewRequest(taskID, epicID, commitSHA string) error {
	col, id := reviewSubject(taskID, epicID)
	return db.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE review_requests SET resolved_at = CURRENT_TIMESTAMP WHERE `+col+` = ? AND resolved_at IS NULL`, id,
		); err != nil {
			return err
		}
		_, err := tx.Exec(
			`INSERT INTO review_requests (`+col+`, commit_sha) VALUES (?, ?)`, id, commitSHA,
		)
		return err
	})
}

// GetOpenReviewRequest returns nil if no unresolved request exists.
func (db *DB) GetOpenReviewRequest(taskID, epicID string) (*ReviewRequest, error) {
	col, id := reviewSubject(taskID, epicID)
	r := &ReviewRequest{}
	err := db.conn.QueryRow(
		`SELECT id, COALESCE(task_id, ''), COALESCE(epic_id, ''), commit_sha, created_at
		 FROM review_requests WHERE `+col+` = ? AND resolved_at IS NULL
		 ORDER BY id DESC LIMIT 1`, id,
	).Scan(&r.ID, &r.TaskID, &r.EpicID, &r.CommitSHA, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ResolveReviewRequest records the verdict and closes the request atomically,
// so one request yields at most one verdict.
func (db *DB) ResolveReviewRequest(req *ReviewRequest, decision, comment string) error {
	return db.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE review_requests SET resolved_at = CURRENT_TIMESTAMP WHERE id = ? AND resolved_at IS NULL`, req.ID,
		)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("review request #%d is already resolved", req.ID)
		}
		var taskVal, epicVal interface{}
		if req.TaskID != "" {
			taskVal = req.TaskID
		}
		if req.EpicID != "" {
			epicVal = req.EpicID
		}
		_, err = tx.Exec(
			`INSERT INTO reviews (task_id, epic_id, decision, comment, commit_sha) VALUES (?, ?, ?, ?, ?)`,
			taskVal, epicVal, decision, comment, req.CommitSHA,
		)
		return err
	})
}

func reviewSubject(taskID, epicID string) (string, string) {
	if epicID != "" {
		return "epic_id", epicID
	}
	return "task_id", taskID
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
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, *r)
	}
	return reviews, nil
}

func (db *DB) GetLatestEpicApproval(epicID string) (*Review, error) {
	r, err := scanReview(db.conn.QueryRow(
		`SELECT id, COALESCE(task_id, ''), epic_id, decision, comment, commit_sha, created_at
		 FROM reviews WHERE epic_id = ? AND decision = 'approve'
		 ORDER BY id DESC LIMIT 1`, epicID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}
