package internal

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Edit is one audited change of a task's or epic's title, description or notes.
type Edit struct {
	SubjectID string
	Fields    string
	EditedBy  string
	CreatedAt time.Time
}

// SubjectText is the human-written text of a task or epic.
type SubjectText struct {
	Title       string
	Description string
	Notes       string
}

func diffFields(a, b SubjectText) []string {
	var out []string
	for _, f := range []struct{ name, a, b string }{
		{"title", a.Title, b.Title}, {"description", a.Description, b.Description}, {"notes", a.Notes, b.Notes},
	} {
		if f.a != f.b {
			out = append(out, f.name)
		}
	}
	return out
}

// RecordTerminalEdit replaces a task's or epic's title, description and notes
// (old, as the human's editor was given them) with new, on an edit the human
// confirmed in an interactive UI (e.g. the dashboard), authorized like
// RecordTerminalVerdict. It is refused if the stored text no longer equals
// old, so a concurrent edit is never overwritten unseen. Any task or epic is
// editable, including merged and closed ones. The change and its audit entry
// are written in one transaction. A description edited during an open review
// only reaches later review requests, whose prompt is composed at request
// time, and so do notes; changed notes are also shown once to the agent by
// its next harness command on the task (showNewNotes).
func (a *App) RecordTerminalEdit(subject string, old, new SubjectText, via string) (string, error) {
	new.Title = strings.TrimSpace(new.Title)
	if new.Title == "" {
		return "", fmt.Errorf("refusing to edit %s: the title must not be empty", subject)
	}
	who, err := terminalAuth(via)(nil, subject, "edit")
	if err != nil {
		return "", err
	}
	fields, err := a.DB.editSubject(subject, old, new, who)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s: %s (by %s).\n", subject, fields, who), nil
}

func (db *DB) editSubject(subject string, old, new SubjectText, who string) (string, error) {
	table := "tasks"
	if IsEpicID(subject) {
		table = "epics"
	}
	var fields []string
	err := db.WithTx(func(tx *sql.Tx) error {
		var stored SubjectText
		err := tx.QueryRow(`SELECT title, description, notes FROM `+table+` WHERE id = ?`, subject).Scan(&stored.Title, &stored.Description, &stored.Notes)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s not found", subject)
		}
		if err != nil {
			return err
		}
		if moved := diffFields(old, stored); len(moved) > 0 {
			return fmt.Errorf("%s changed since the editor was opened (%s); nothing saved, re-open it to edit the current text",
				subject, strings.Join(moved, ", "))
		}
		fields = diffFields(stored, new)
		if len(fields) == 0 {
			return fmt.Errorf("no changes to %s", subject)
		}
		now := time.Now()
		if _, err := tx.Exec(`UPDATE `+table+` SET title = ?, description = ?, notes = ?, updated_at = ? WHERE id = ?`,
			new.Title, new.Description, new.Notes, now, subject); err != nil {
			return err
		}
		if stored.Notes != new.Notes {
			if _, err := tx.Exec(`UPDATE `+table+` SET notes_changed_at = ?, notes_changed_by = ? WHERE id = ?`, now, who, subject); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`INSERT INTO subject_edits (subject_id, fields, edited_by, created_at) VALUES (?, ?, ?, ?)`,
			subject, strings.Join(fields, ", "), who, now)
		return err
	})
	return strings.Join(fields, ", "), err
}

func printNotes(notes string) {
	if strings.TrimSpace(notes) == "" {
		return
	}
	fmt.Println("Notes:")
	for _, l := range strings.Split(strings.TrimRight(notes, "\n"), "\n") {
		fmt.Printf("  %s\n", l)
	}
}

// ListEdits returns subject's edits, oldest first.
func (db *DB) ListEdits(subject string) ([]Edit, error) {
	rows, err := db.conn.Query(`SELECT subject_id, fields, edited_by, created_at FROM subject_edits WHERE subject_id = ? ORDER BY id ASC`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edit
	for rows.Next() {
		var e Edit
		if err := rows.Scan(&e.SubjectID, &e.Fields, &e.EditedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
