package internal

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Prefixes of review_type_set_by for types a human set in an interactive UI.
const (
	ReviewSetByPreApproved        = "pre-approved by "
	ReviewSetByHuman              = "set by "
	ReviewSetByPreApprovalRemoved = "pre-approval removed by "
)

// IsPreApproval reports whether an approve verdict's reviewer is a
// pre-approval that stopped a review (RecordTerminalStop with StopApprove),
// rather than a human or agent who reviewed the change.
func IsPreApproval(reviewer string) bool {
	return strings.HasSuffix(reviewer, ", pre-approved)")
}

// RecordTerminalRemovePreApproval undoes a pre-approval on a decision a human
// confirmed in an interactive UI, authorized like RecordTerminalVerdict: the
// subject's type goes from none back to default. A task whose APPROVED state
// came from a pre-approval also goes back to IN_PROGRESS in the same
// transaction, so it needs checks and a real review again. Epics have no
// state; their merge ignores a pre-approval verdict unless the type is none.
// QUEUED and MERGING tasks are past the review gate and refused.
func (a *App) RecordTerminalRemovePreApproval(subject, via, reason string) (string, error) {
	isEpic := IsEpicID(subject)
	var current ReviewType
	if isEpic {
		e, err := a.DB.GetEpic(subject)
		if err != nil {
			return "", err
		}
		current = e.ReviewType
	} else {
		task, err := a.DB.GetTask(subject)
		if err != nil {
			return "", err
		}
		current = task.ReviewType
		if task.State == StateQueued || task.State == StateMerging {
			return "", fmt.Errorf("cannot remove the pre-approval of %s: it is %s, already past the review gate", subject, task.State)
		}
	}
	if current != ReviewNone {
		return "", fmt.Errorf("%s is not pre-approved (review type %s)", subject, current)
	}
	who, err := terminalAuth(via)(nil, subject, "remove pre-approval")
	if err != nil {
		return "", err
	}
	setBy := ReviewSetByPreApprovalRemoved + who
	if reason != "" {
		setBy += ": " + reason
	}
	reverted, err := a.DB.removePreApproval(subject, isEpic, setBy)
	if err != nil {
		return "", err
	}
	report := fmt.Sprintf("Pre-approval of %s removed; review type is default again (%s).\n", subject, setBy)
	if reverted {
		report += fmt.Sprintf("Its pre-approval no longer counts: task %s is now IN_PROGRESS; run checks and request review.\n", subject)
	}
	return report, nil
}

// removePreApproval sets the type to default and, for an APPROVED task whose
// latest approval is a pre-approval, moves it to IN_PROGRESS. Both happen in
// one transaction, re-checking the state there, so a merge starting
// concurrently cannot slip between them.
func (db *DB) removePreApproval(subject string, isEpic bool, setBy string) (reverted bool, err error) {
	table := "tasks"
	if isEpic {
		table = "epics"
	}
	err = db.WithTx(func(tx *sql.Tx) error {
		var rt string
		if err := tx.QueryRow(`SELECT review_type FROM `+table+` WHERE id = ?`, subject).Scan(&rt); err != nil {
			return fmt.Errorf("reading %s: %w", subject, err)
		}
		if ReviewType(rt) != ReviewNone {
			return fmt.Errorf("%s is not pre-approved (review type %s)", subject, rt)
		}
		now := time.Now()
		if !isEpic {
			var state string
			if err := tx.QueryRow(`SELECT state FROM tasks WHERE id = ?`, subject).Scan(&state); err != nil {
				return err
			}
			switch TaskState(state) {
			case StateQueued, StateMerging:
				return fmt.Errorf("cannot remove the pre-approval of %s: it is %s, already past the review gate", subject, state)
			case StateApproved:
				var reviewer string
				err := tx.QueryRow(`SELECT reviewer FROM reviews WHERE task_id = ? AND decision = 'approve' ORDER BY id DESC LIMIT 1`, subject).Scan(&reviewer)
				if err != nil && err != sql.ErrNoRows {
					return err
				}
				if IsPreApproval(reviewer) {
					if _, err := tx.Exec(`UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, string(StateInProgress), now, subject); err != nil {
						return err
					}
					if err := recordTransitionNote(tx, "task_state_transitions", "task_id", subject, state, string(StateInProgress), "pre-approval removed"); err != nil {
						return err
					}
					reverted = true
				}
			}
		}
		_, err := tx.Exec(`UPDATE `+table+` SET review_type = ?, review_type_set_by = ?, review_type_set_at = ? WHERE id = ?`,
			string(ReviewDefault), setBy, now, subject)
		return err
	})
	return reverted, err
}

// RecordTerminalReviewType changes a subject's review type on a decision a
// human confirmed in an interactive UI (e.g. the dashboard), authorized like
// RecordTerminalVerdict. It does not need review.allow_none: setting none
// here is a human pre-approval, not a self-declaration. It only records the
// type; stopping an in-flight review is RecordTerminalStop's job.
func (a *App) RecordTerminalReviewType(subject string, r ReviewType, via, reason string) (string, error) {
	if !r.Valid() {
		return "", fmt.Errorf("invalid review type %q: want default, human or none", r)
	}
	who, err := terminalAuth(via)(nil, subject, "review type "+string(r))
	if err != nil {
		return "", err
	}
	setBy := ReviewSetByHuman + who
	if r == ReviewNone {
		setBy = ReviewSetByPreApproved + who
	}
	if reason != "" {
		setBy += ": " + reason
	}
	if err := a.DB.SetReviewType(subject, r, setBy); err != nil {
		return "", err
	}
	return fmt.Sprintf("Review type of %s is now %s (%s).\n", subject, r, setBy), nil
}
