package internal

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
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
	ID         int
	TaskID     string
	EpicID     string
	CommitSHA  string
	TokenHash  string
	CreatedAt  time.Time
	Resolved   bool
	Resolution string

	// Supervision of the review.cmd run; ReviewerOutcome is "" when no
	// reviewer was launched (human review, or rows predating v10).
	ReviewerPID       int
	ReviewerOwnerPID  int
	ReviewerStarted   time.Time
	ReviewerHeartbeat time.Time
	ReviewerFinished  time.Time
	ReviewerExitCode  int
	ReviewerOutcome   string
}

const (
	ReviewerRunning     = "running"
	ReviewerVerdict     = "verdict"
	ReviewerNoVerdict   = "no_verdict"
	ReviewerFailed      = "failed"
	ReviewerTimeout     = "timeout"
	ReviewerInterrupted = "interrupted"
	// ReviewerOrphaned is never stored: it is a running row whose
	// `harness review request` process is gone, computed on read.
	ReviewerOrphaned = "orphaned"
)

const (
	resolutionVerdict    = "verdict"
	resolutionSuperseded = "superseded"
	resolutionReviewer   = "reviewer_failed"
)

// ReviewerHeartbeatTTL must exceed reviewerHeartbeatEvery by a wide margin.
const (
	ReviewerHeartbeatTTL   = 60 * time.Second
	reviewerHeartbeatEvery = 10 * time.Second
)

// RunStatus is the stored outcome, except that a running row whose owner is
// dead or stopped heartbeating reads as orphaned. PIDs are checked on this
// machine, like merge queue leases.
func (r *ReviewRequest) RunStatus(now time.Time) string {
	if r.ReviewerOutcome != ReviewerRunning || r.ReviewerOwnerPID == os.Getpid() {
		return r.ReviewerOutcome
	}
	if !pidAlive(r.ReviewerOwnerPID) || now.Sub(r.ReviewerHeartbeat) > ReviewerHeartbeatTTL {
		return ReviewerOrphaned
	}
	return ReviewerRunning
}

const reviewRequestColumns = `id, COALESCE(task_id, ''), COALESCE(epic_id, ''), commit_sha, token_hash, created_at,
	resolved_at IS NOT NULL, resolution, reviewer_pid, reviewer_owner_pid, reviewer_started_unix,
	reviewer_heartbeat_unix, reviewer_finished_unix, reviewer_exit_code, reviewer_outcome`

func scanReviewRequest(s rowScanner) (*ReviewRequest, error) {
	r := &ReviewRequest{}
	var started, heartbeat, finished int64
	err := s.Scan(&r.ID, &r.TaskID, &r.EpicID, &r.CommitSHA, &r.TokenHash, &r.CreatedAt,
		&r.Resolved, &r.Resolution, &r.ReviewerPID, &r.ReviewerOwnerPID, &started,
		&heartbeat, &finished, &r.ReviewerExitCode, &r.ReviewerOutcome)
	if err != nil {
		return nil, err
	}
	r.ReviewerStarted = unixOrZero(started)
	r.ReviewerHeartbeat = unixOrZero(heartbeat)
	r.ReviewerFinished = unixOrZero(finished)
	return r, nil
}

func unixOrZero(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
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
	token, _, err := db.OpenReviewRequestID(taskID, epicID, commitSHA)
	return token, err
}

// OpenReviewRequestID is OpenReviewRequest that also returns the new request's id.
func (db *DB) OpenReviewRequestID(taskID, epicID, commitSHA string) (string, int, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, err
	}
	token := hex.EncodeToString(buf)

	col, id := reviewSubject(taskID, epicID)
	var reqID int64
	err := db.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE review_requests SET resolved_at = CURRENT_TIMESTAMP, resolution = ? WHERE `+col+` = ? AND resolved_at IS NULL`,
			resolutionSuperseded, id,
		); err != nil {
			return err
		}
		res, err := tx.Exec(
			`INSERT INTO review_requests (`+col+`, commit_sha, token_hash) VALUES (?, ?, ?)`,
			id, commitSHA, hashReviewToken(token),
		)
		if err != nil {
			return err
		}
		reqID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return "", 0, err
	}
	return token, int(reqID), nil
}

// GetOpenReviewRequest returns nil if no unresolved request exists.
func (db *DB) GetOpenReviewRequest(taskID, epicID string) (*ReviewRequest, error) {
	col, id := reviewSubject(taskID, epicID)
	return nilIfNoRows(scanReviewRequest(db.conn.QueryRow(
		`SELECT `+reviewRequestColumns+` FROM review_requests WHERE `+col+` = ? AND resolved_at IS NULL
		 ORDER BY id DESC LIMIT 1`, id,
	)))
}

// GetLatestReviewRequest returns nil if the subject was never requested for review.
func (db *DB) GetLatestReviewRequest(taskID, epicID string) (*ReviewRequest, error) {
	col, id := reviewSubject(taskID, epicID)
	return nilIfNoRows(scanReviewRequest(db.conn.QueryRow(
		`SELECT `+reviewRequestColumns+` FROM review_requests WHERE `+col+` = ? ORDER BY id DESC LIMIT 1`, id,
	)))
}

func (db *DB) GetReviewRequest(reqID int) (*ReviewRequest, error) {
	return scanReviewRequest(db.conn.QueryRow(
		`SELECT `+reviewRequestColumns+` FROM review_requests WHERE id = ?`, reqID,
	))
}

// ListReviewerRuns returns the subject's requests that launched a reviewer, oldest first.
func (db *DB) ListReviewerRuns(taskID, epicID string) ([]ReviewRequest, error) {
	col, id := reviewSubject(taskID, epicID)
	rows, err := db.conn.Query(
		`SELECT `+reviewRequestColumns+` FROM review_requests
		 WHERE `+col+` = ? AND reviewer_outcome != '' ORDER BY id ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewRequest
	for rows.Next() {
		r, err := scanReviewRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func nilIfNoRows(r *ReviewRequest, err error) (*ReviewRequest, error) {
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// StartReviewerRun marks the request's reviewer as running under ownerPID
// (the `harness review request` process), which must keep renewing it with
// HeartbeatReviewerRun.
func (db *DB) StartReviewerRun(reqID, ownerPID, reviewerPID int) error {
	now := time.Now().Unix()
	_, err := db.conn.Exec(
		`UPDATE review_requests SET reviewer_pid = ?, reviewer_owner_pid = ?, reviewer_started_unix = ?,
		 reviewer_heartbeat_unix = ?, reviewer_outcome = ? WHERE id = ?`,
		reviewerPID, ownerPID, now, now, ReviewerRunning, reqID,
	)
	return err
}

func (db *DB) HeartbeatReviewerRun(reqID, ownerPID int) error {
	_, err := db.conn.Exec(
		`UPDATE review_requests SET reviewer_heartbeat_unix = ? WHERE id = ? AND reviewer_owner_pid = ? AND reviewer_outcome = ?`,
		time.Now().Unix(), reqID, ownerPID, ReviewerRunning,
	)
	return err
}

// FinishReviewerRun records how the reviewer ended and returns the stored
// outcome: a verdict recorded on the request wins over exitOutcome. Otherwise
// the request is closed without a verdict in the same transaction, so the
// token dies and a late verdict from a leftover process is refused.
func (db *DB) FinishReviewerRun(reqID int, exitOutcome string, exitCode int) (string, error) {
	outcome := exitOutcome
	err := db.WithTx(func(tx *sql.Tx) error {
		var resolution string
		if err := tx.QueryRow(`SELECT resolution FROM review_requests WHERE id = ?`, reqID).Scan(&resolution); err != nil {
			return err
		}
		if resolution == resolutionVerdict {
			outcome = ReviewerVerdict
		} else if _, err := tx.Exec(
			`UPDATE review_requests SET resolved_at = CURRENT_TIMESTAMP, resolution = ? WHERE id = ? AND resolved_at IS NULL`,
			resolutionReviewer, reqID,
		); err != nil {
			return err
		}
		_, err := tx.Exec(
			`UPDATE review_requests SET reviewer_finished_unix = ?, reviewer_exit_code = ?, reviewer_outcome = ? WHERE id = ?`,
			time.Now().Unix(), exitCode, outcome, reqID,
		)
		return err
	})
	return outcome, err
}

// ResolveReviewRequest records the verdict and closes the request atomically,
// so one request yields at most one verdict.
func (db *DB) ResolveReviewRequest(req *ReviewRequest, decision, comment, reviewer string) error {
	return db.WithTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE review_requests SET resolved_at = CURRENT_TIMESTAMP, resolution = ? WHERE id = ? AND resolved_at IS NULL`,
			resolutionVerdict, req.ID,
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
