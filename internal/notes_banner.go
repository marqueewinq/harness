package internal

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// notesUnseen: notes changed after they were last shown, or were never shown
// and are non-empty (e.g. edited before seen-tracking existed).
func notesUnseen(notes string, changed, seen sql.NullTime) bool {
	if !seen.Valid {
		return strings.TrimSpace(notes) != ""
	}
	return changed.Valid && changed.Time.After(seen.Time)
}

func printNotesBanner(subject, context, notes, by string, changed sql.NullTime) {
	when := ""
	if changed.Valid {
		when = "edited " + changed.Time.Local().Format("2006-01-02 15:04")
		if by != "" {
			when += " by " + by
		}
		when = " (" + when + ")"
	}
	fmt.Printf("── New notes from the human on %s%s%s ──\n", subject, context, when)
	fmt.Println("These are instructions and context from the human. Read them and follow them.")
	if strings.TrimSpace(notes) == "" {
		fmt.Println("(the notes were cleared)")
	} else {
		fmt.Println(strings.TrimRight(notes, "\n"))
	}
	fmt.Println("──")
	fmt.Println()
}

// showNewNotes prints the task's notes, and its epic's, if a human changed
// them since this task last showed them, then marks them seen. It runs before
// a command's own output and never fails the command.
//
// Epic notes are tracked per task (tasks.epic_notes_seen_at), not per epic:
// several agents may work on tasks of one epic, and each must see a change.
func (a *App) showNewNotes(taskID string) {
	var notes, by, epicID string
	var changed, seen, epicSeen sql.NullTime
	err := a.DB.conn.QueryRow(
		`SELECT notes, notes_changed_by, notes_changed_at, notes_seen_at, COALESCE(epic_id, ''), epic_notes_seen_at
		 FROM tasks WHERE id = ?`, taskID,
	).Scan(&notes, &by, &changed, &seen, &epicID, &epicSeen)
	if err != nil {
		return
	}
	now := time.Now()
	if notesUnseen(notes, changed, seen) {
		printNotesBanner(taskID, "", notes, by, changed)
		a.DB.conn.Exec(`UPDATE tasks SET notes_seen_at = ? WHERE id = ?`, now, taskID)
	}
	if epicID == "" {
		return
	}
	var epicNotes, epicBy string
	var epicChanged sql.NullTime
	if err := a.DB.conn.QueryRow(`SELECT notes, notes_changed_by, notes_changed_at FROM epics WHERE id = ?`, epicID).
		Scan(&epicNotes, &epicBy, &epicChanged); err != nil {
		return
	}
	if notesUnseen(epicNotes, epicChanged, epicSeen) {
		printNotesBanner(epicID, " (the epic of "+taskID+")", epicNotes, epicBy, epicChanged)
		a.DB.conn.Exec(`UPDATE tasks SET epic_notes_seen_at = ? WHERE id = ?`, now, taskID)
	}
}

// showNewEpicNotes is showNewNotes for commands acting on the epic itself.
func (a *App) showNewEpicNotes(epicID string) {
	var notes, by string
	var changed, seen sql.NullTime
	if err := a.DB.conn.QueryRow(`SELECT notes, notes_changed_by, notes_changed_at, notes_seen_at FROM epics WHERE id = ?`, epicID).
		Scan(&notes, &by, &changed, &seen); err != nil {
		return
	}
	if notesUnseen(notes, changed, seen) {
		printNotesBanner(epicID, "", notes, by, changed)
		a.markNotesSeen(epicID)
	}
}

// markNotesSeen is for commands that already print the subject's notes.
func (a *App) markNotesSeen(subject string) {
	table := "tasks"
	if IsEpicID(subject) {
		table = "epics"
	}
	a.DB.conn.Exec(`UPDATE `+table+` SET notes_seen_at = ? WHERE id = ?`, time.Now(), subject)
}
