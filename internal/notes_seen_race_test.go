package internal

import (
	"strings"
	"testing"
	"time"
)

// An edit landing between reading the notes and marking them seen must stay
// unseen: marking only applies to the text that was actually shown.
// Not parallel: captureStdout swaps os.Stdout.
func TestMarkNotesSeenIgnoresNotesChangedSinceShown(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()
	if _, err := app.DB.conn.Exec(`UPDATE tasks SET notes = ?, notes_changed_at = ? WHERE id = ?`,
		"newer instructions", time.Now(), task.ID); err != nil {
		t.Fatal(err)
	}

	app.markNotesSeen(task.ID, "older text that was shown")

	show := func() string {
		out, _ := captureStdout(t, func() error { app.showNewNotes(task.ID); return nil })
		return out
	}
	if out := show(); !strings.Contains(out, "newer instructions") {
		t.Fatalf("newer notes were marked seen without being shown:\n%s", out)
	}
	if again := show(); again != "" {
		t.Errorf("banner repeated after being shown:\n%s", again)
	}
}
