package internal

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
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
	Reviewer  string
	CreatedAt time.Time
}

const reviewColumns = `id, COALESCE(task_id, ''), COALESCE(epic_id, ''), decision, comment, commit_sha, reviewer, created_at`

func scanReview(s rowScanner) (*Review, error) {
	r := &Review{}
	err := s.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.Decision, &r.Comment, &r.CommitSHA, &r.Reviewer, &r.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (db *DB) listReviews(taskID, epicID string) ([]Review, error) {
	col, id := reviewSubject(taskID, epicID)
	rows, err := db.conn.Query(
		`SELECT `+reviewColumns+` FROM reviews WHERE `+col+` = ? ORDER BY id ASC`, id,
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

func (db *DB) latestApproval(taskID, epicID string) (*Review, error) {
	col, id := reviewSubject(taskID, epicID)
	r, err := scanReview(db.conn.QueryRow(
		`SELECT `+reviewColumns+` FROM reviews WHERE `+col+` = ? AND decision = 'approve'
		 ORDER BY id DESC LIMIT 1`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (db *DB) GetReviews(taskID string) ([]Review, error) { return db.listReviews(taskID, "") }

func (db *DB) GetLatestApproval(taskID string) (*Review, error) { return db.latestApproval(taskID, "") }

func (db *DB) GetEpicReviews(epicID string) ([]Review, error) { return db.listReviews("", epicID) }

func (db *DB) GetLatestEpicApproval(epicID string) (*Review, error) {
	return db.latestApproval("", epicID)
}

type ReviewRequest struct {
	ID        int
	TaskID    string
	EpicID    string
	CommitSHA string
	TokenHash string
	CreatedAt time.Time
}

// TokenMatches compares in constant time; a request without a token matches nothing.
func (r *ReviewRequest) TokenMatches(token string) bool {
	if r.TokenHash == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashReviewToken(token)), []byte(r.TokenHash)) == 1
}

func hashReviewToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// OpenReviewRequest supersedes any open request for the same task or epic and
// returns a one-time token authorizing a single verdict on the new request.
// Only the token's hash is stored. Exactly one of taskID, epicID must be non-empty.
func (db *DB) OpenReviewRequest(taskID, epicID, commitSHA string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)

	col, id := reviewSubject(taskID, epicID)
	err := db.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE review_requests SET resolved_at = CURRENT_TIMESTAMP WHERE `+col+` = ? AND resolved_at IS NULL`, id,
		); err != nil {
			return err
		}
		_, err := tx.Exec(
			`INSERT INTO review_requests (`+col+`, commit_sha, token_hash) VALUES (?, ?, ?)`,
			id, commitSHA, hashReviewToken(token),
		)
		return err
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// GetOpenReviewRequest returns nil if no unresolved request exists.
func (db *DB) GetOpenReviewRequest(taskID, epicID string) (*ReviewRequest, error) {
	col, id := reviewSubject(taskID, epicID)
	r := &ReviewRequest{}
	err := db.conn.QueryRow(
		`SELECT id, COALESCE(task_id, ''), COALESCE(epic_id, ''), commit_sha, token_hash, created_at
		 FROM review_requests WHERE `+col+` = ? AND resolved_at IS NULL
		 ORDER BY id DESC LIMIT 1`, id,
	).Scan(&r.ID, &r.TaskID, &r.EpicID, &r.CommitSHA, &r.TokenHash, &r.CreatedAt)
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
func (db *DB) ResolveReviewRequest(req *ReviewRequest, decision, comment, reviewer string) error {
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
			`INSERT INTO reviews (task_id, epic_id, decision, comment, commit_sha, reviewer) VALUES (?, ?, ?, ?, ?, ?)`,
			taskVal, epicVal, decision, comment, req.CommitSHA, reviewer,
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
