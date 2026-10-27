package internal

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Notes pinning: a review request snapshots the subject's own notes (a
// task's, or an epic's for an epic review; a task's epic notes are not part of
// the task's gate). An approval covers that snapshot, and a merge needs the
// notes to be unchanged since. A change resets the gate: re-run checks and
// request review again. A review-type-none task instead needs its notes
// unchanged since its latest check run; a none epic has no gate to reset.

// requestNotes is the notes snapshot the request pinned, so the reviewer
// prompt carries exactly what an approval will be checked against.
func (a *App) requestNotes(reqID int, fallback string) string {
	req, err := a.DB.GetReviewRequest(reqID)
	if err != nil || !req.NotesSnapshot.Valid {
		return fallback
	}
	return req.NotesSnapshot.String
}

type subjectNotes struct {
	Notes     string
	ChangedBy string
	ChangedAt sql.NullTime
}

// readSubjectNotes reads inside the caller's transaction. A missing subject
// reads as empty notes; callers that need the subject check it themselves.
func readSubjectNotes(q dbExecer, taskID, epicID string) (subjectNotes, error) {
	table, id := "tasks", taskID
	if epicID != "" {
		table, id = "epics", epicID
	}
	var n subjectNotes
	err := q.QueryRow(`SELECT notes, notes_changed_by, notes_changed_at FROM `+table+` WHERE id = ?`, id).
		Scan(&n.Notes, &n.ChangedBy, &n.ChangedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return subjectNotes{}, nil
	}
	return n, err
}

func (n subjectNotes) edited() string {
	if !n.ChangedAt.Valid {
		return ""
	}
	s := "edited " + n.ChangedAt.Time.Local().Format("2006-01-02 15:04:05")
	if n.ChangedBy != "" {
		s += " by " + n.ChangedBy
	}
	return " (" + s + ")"
}

func (n subjectNotes) quoted() string {
	if strings.TrimSpace(n.Notes) == "" {
		return "(the notes were cleared)"
	}
	return strings.TrimRight(n.Notes, "\n")
}

func rerunHint(subject string) string {
	if IsEpicID(subject) {
		return "harness review request " + subject
	}
	return fmt.Sprintf("harness work check %s, then harness review request %s", subject, subject)
}

func (n subjectNotes) changedDuringReview(subject string) error {
	return fmt.Errorf("refusing to approve %s: notes changed during review%s; the reviewer did not see them — run %s\nNotes now:\n%s",
		subject, n.edited(), rerunHint(subject), n.quoted())
}

// requireNotesCoveredByApproval refuses a merge whose approval did not cover
// the subject's current notes. Approvals predating notes pinning are not checked.
func (a *App) requireNotesCoveredByApproval(taskID, epicID string, approval *Review) error {
	if !approval.NotesSnapshot.Valid {
		return nil
	}
	n, err := readSubjectNotes(a.DB.conn, taskID, epicID)
	if err != nil {
		return err
	}
	if n.Notes == approval.NotesSnapshot.String {
		return nil
	}
	subject := taskID + epicID
	return fmt.Errorf("new notes from the human on %s since approval%s: address them, re-run checks and request review again (%s)\nNotes:\n%s",
		subject, n.edited(), rerunHint(subject), n.quoted())
}

// requireNotesBeforeChecks refuses a review-type-none merge whose notes
// changed after its latest check run started: nothing since has shown that
// the work addresses them.
func (a *App) requireNotesBeforeChecks(taskID string, checksStarted time.Time) error {
	n, err := readSubjectNotes(a.DB.conn, taskID, "")
	if err != nil {
		return err
	}
	if !n.ChangedAt.Valid || !n.ChangedAt.Time.After(checksStarted) {
		return nil
	}
	return fmt.Errorf("new notes from the human on %s since the last check run%s: address them and re-run checks (harness work check %s)\nNotes:\n%s",
		taskID, n.edited(), taskID, n.quoted())
}
