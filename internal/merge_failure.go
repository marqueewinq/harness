package internal

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MergeLogPath is the full output of the latest merge attempt of a task or
// epic; each attempt overwrites it.
func MergeLogPath(repoRoot, subjectID string) string {
	return filepath.Join(CheckLogDir(repoRoot, subjectID), "merge.log")
}

// startMergeLog begins a new attempt's log. Logging is best effort: a merge
// never fails because its log could not be written.
func (a *App) startMergeLog(subjectID string) {
	path := MergeLogPath(a.RepoRoot, subjectID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	os.WriteFile(path, []byte(fmt.Sprintf("merge of %s started %s\n", subjectID, time.Now().Format(time.RFC3339))), 0644)
}

func (a *App) mergeLogf(subjectID, format string, args ...interface{}) {
	path := MergeLogPath(a.RepoRoot, subjectID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	fmt.Fprintf(f, "[%s] %s", time.Now().Format("15:04:05"), msg)
}

func (a *App) mergeLogChecks(subjectID string, results []CheckResult) {
	for _, r := range results {
		status := "PASS"
		switch {
		case r.Skipped:
			status = "SKIP"
		case r.ExitCode != 0:
			status = fmt.Sprintf("FAIL (exit %d)", r.ExitCode)
		}
		a.mergeLogf(subjectID, "check %s: %s", r.Name, status)
		if r.Stdout != "" {
			a.mergeLogf(subjectID, "--- %s stdout\n%s", r.Name, r.Stdout)
		}
		if r.Stderr != "" {
			a.mergeLogf(subjectID, "--- %s stderr\n%s", r.Name, r.Stderr)
		}
	}
}

func failedCheckNames(results []CheckResult) string {
	var names []string
	for _, r := range results {
		if !r.Skipped && r.ExitCode != 0 {
			names = append(names, r.Name)
		}
	}
	return strings.Join(names, ", ")
}

// mergeFailureReason is the one-line reason stored with the failure; the
// full error, including git's output, goes to the merge log.
func mergeFailureReason(err error) string {
	msg := strings.TrimSpace(err.Error())
	first, _, _ := strings.Cut(msg, "\n")
	var conflicts []string
	for _, l := range strings.Split(msg, "\n") {
		if _, file, ok := strings.Cut(l, "Merge conflict in "); ok {
			conflicts = append(conflicts, strings.TrimSpace(file))
		}
	}
	if len(conflicts) > 0 {
		first += " (conflict in " + strings.Join(conflicts, ", ") + ")"
	}
	return first
}

type MergeFailure struct {
	Reason  string
	At      time.Time
	LogPath string // "" when no merge log exists
}

func (m MergeFailure) String() string {
	reason := m.Reason
	if reason == "" {
		reason = "no reason recorded"
	}
	s := fmt.Sprintf("%s (%s)", reason, m.At.Local().Format("2006-01-02 15:04:05"))
	if m.LogPath != "" {
		s += " · log: " + m.LogPath
	}
	return s
}

func (a *App) withMergeLog(subjectID string, at time.Time, reason string) *MergeFailure {
	m := &MergeFailure{Reason: reason, At: at}
	if _, err := os.Stat(MergeLogPath(a.RepoRoot, subjectID)); err == nil {
		m.LogPath = MergeLogPath(a.RepoRoot, subjectID)
	}
	return m
}

// LastMergeFailure is the task's latest transition into MERGE_FAILED, or nil.
func (a *App) LastMergeFailure(taskID string) (*MergeFailure, error) {
	var note string
	var at time.Time
	err := a.DB.conn.QueryRow(
		`SELECT note, created_at FROM task_state_transitions
		 WHERE task_id = ? AND to_state = ? ORDER BY id DESC LIMIT 1`,
		taskID, string(StateMergeFailed),
	).Scan(&note, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a.withMergeLog(taskID, at, note), nil
}

// Epics have no MERGE_FAILED status: a failed epic merge leaves the epic open.
// It is recorded as an open -> open transition carrying the reason, so it
// sits in the epic's history without a new status that every epic reader
// would have to handle.
func (db *DB) RecordEpicMergeFailure(epicID, reason string) error {
	return db.WithTx(func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRow(`SELECT status FROM epics WHERE id = ?`, epicID).Scan(&status); err != nil {
			return fmt.Errorf("reading epic %s: %w", epicID, err)
		}
		return recordTransitionNote(tx, "epic_state_transitions", "epic_id", epicID, status, status, "merge failed: "+reason)
	})
}

func (a *App) failEpicMerge(epicID string, cause error) error {
	reason := mergeFailureReason(cause)
	a.mergeLogf(epicID, "MERGE FAILED: %v", cause)
	if err := a.DB.RecordEpicMergeFailure(epicID, reason); err != nil {
		return fmt.Errorf("%w (also failed to record the failure: %v)", cause, err)
	}
	fmt.Printf("\nEpic %s merge failed: %s\n", epicID, reason)
	fmt.Printf("Merge log: %s\n", MergeLogPath(a.RepoRoot, epicID))
	return cause
}

// LastEpicMergeFailure is the epic's latest merge failure if no transition
// (e.g. the close on a successful merge) has happened since, or nil.
func (a *App) LastEpicMergeFailure(epicID string) (*MergeFailure, error) {
	var from, to, note string
	var at time.Time
	err := a.DB.conn.QueryRow(
		`SELECT from_state, to_state, note, created_at FROM epic_state_transitions
		 WHERE epic_id = ? ORDER BY id DESC LIMIT 1`, epicID,
	).Scan(&from, &to, &note, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	reason, ok := strings.CutPrefix(note, "merge failed: ")
	if from != to || !ok {
		return nil, nil
	}
	return a.withMergeLog(epicID, at, reason), nil
}

func (a *App) printMergeFailure(task *Task) {
	m, err := a.LastMergeFailure(task.ID)
	if err != nil || m == nil {
		return
	}
	if task.State == StateMergeFailed {
		fmt.Printf("Merge failed: %s\n", m)
	} else {
		fmt.Printf("Last merge failure: %s\n", m)
	}
}

func (db *DB) UpdateTaskStateNote(id string, state TaskState, note string) error {
	return db.WithTx(func(tx *sql.Tx) error { return updateTaskStateNoteTx(tx, id, state, note) })
}
