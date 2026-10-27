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

// RecordTerminalEdit sets a task's or epic's title, description and notes on
// an edit a human confirmed in an interactive UI (e.g. the dashboard),
// authorized like RecordTerminalVerdict. Any task or epic is editable,
// including merged and closed ones. The change and its audit entry are
// written in one transaction. A description edited during an open review
// only reaches later review requests, whose prompt is composed at request
// time; notes never reach a reviewer.
func (a *App) RecordTerminalEdit(subject, title, description, notes, via string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", fmt.Errorf("refusing to edit %s: the title must not be empty", subject)
	}
	who, err := terminalAuth(via)(nil, subject, "edit")
	if err != nil {
		return "", err
	}
	fields, err := a.DB.editSubject(subject, title, description, notes, who)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Edited %s: %s (by %s).\n", subject, fields, who), nil
}

func (db *DB) editSubject(subject, title, description, notes, who string) (string, error) {
	table := "tasks"
	if isEpicID(subject) {
		table = "epics"
	}
	var fields []string
	err := db.WithTx(func(tx *sql.Tx) error {
		var oldTitle, oldDesc, oldNotes string
		err := tx.QueryRow(`SELECT title, description, notes FROM `+table+` WHERE id = ?`, subject).Scan(&oldTitle, &oldDesc, &oldNotes)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%s not found", subject)
		}
		if err != nil {
			return err
		}
		for _, f := range []struct{ name, old, new string }{
			{"title", oldTitle, title}, {"description", oldDesc, description}, {"notes", oldNotes, notes},
		} {
			if f.old != f.new {
				fields = append(fields, f.name)
			}
		}
		if len(fields) == 0 {
			return fmt.Errorf("no changes to %s", subject)
		}
		now := time.Now()
		if _, err := tx.Exec(`UPDATE `+table+` SET title = ?, description = ?, notes = ?, updated_at = ? WHERE id = ?`,
			title, description, notes, now, subject); err != nil {
			return err
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
