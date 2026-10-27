package internal

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordTerminalEditTask(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, err := app.DB.CreateTask("old title", "old desc", "b", "/tmp/x", "main", "", "", ReviewSetting{})
	if err != nil {
		t.Fatal(err)
	}
	report, err := app.RecordTerminalEdit(task.ID, SubjectText{"old title", "old desc", ""}, SubjectText{"new title", "old desc", "line 1\nline 2"}, "dashboard")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := app.DB.GetTask(task.ID)
	if got.Title != "new title" || got.Description != "old desc" || got.Notes != "line 1\nline 2" {
		t.Errorf("task = %q %q %q", got.Title, got.Description, got.Notes)
	}
	if !strings.Contains(report, "title, notes") {
		t.Errorf("report = %q", report)
	}
	edits, err := app.DB.ListEdits(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantBy := "terminal:" + terminalUser() + " (dashboard)"
	if len(edits) != 1 || edits[0].Fields != "title, notes" || edits[0].EditedBy != wantBy || edits[0].CreatedAt.IsZero() {
		t.Errorf("edits = %+v, want title, notes by %s", edits, wantBy)
	}
	trs, _ := app.DB.GetTaskTransitions(task.ID)
	if len(trs) != 1 {
		t.Errorf("an edit wrote a state transition: %+v", trs)
	}
	out, err := captureStdout(t, func() error { return app.CmdWorkStatus(task.ID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Notes:\n  line 1\n  line 2\n") {
		t.Errorf("work status lacks notes:\n%s", out)
	}

	if _, err := app.RecordTerminalEdit(task.ID, SubjectText{"new title", "old desc", "line 1\nline 2"}, SubjectText{"  ", "x", ""}, "dashboard"); err == nil || !strings.Contains(err.Error(), "title") {
		t.Errorf("empty title: err = %v", err)
	}
	if _, err := app.RecordTerminalEdit(task.ID, SubjectText{"new title", "old desc", "line 1\nline 2"}, SubjectText{"new title", "old desc", "line 1\nline 2"}, "dashboard"); err == nil || !strings.Contains(err.Error(), "no changes") {
		t.Errorf("unchanged edit: err = %v", err)
	}
}

func TestRecordTerminalEditRefusesStaleEdit(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, _ := app.DB.CreateTask("title", "desc", "b", "/tmp/x", "main", "", "", ReviewSetting{})
	opened := SubjectText{"title", "desc", ""}
	// Someone else edits the notes while the editor is open.
	if _, err := app.RecordTerminalEdit(task.ID, opened, SubjectText{"title", "desc", "their notes"}, "dashboard"); err != nil {
		t.Fatal(err)
	}
	_, err := app.RecordTerminalEdit(task.ID, opened, SubjectText{"my title", "desc", ""}, "dashboard")
	if err == nil || !strings.Contains(err.Error(), "changed since the editor was opened (notes)") || !strings.Contains(err.Error(), "re-open") {
		t.Errorf("stale edit: err = %v", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.Title != "title" || got.Notes != "their notes" {
		t.Errorf("stale edit applied: %q %q", got.Title, got.Notes)
	}
}

func TestRecordTerminalEditMergedTaskAndEpic(t *testing.T) {
	app, task := setupTaskInState(t, StateMerged)
	app.Git = NewGitClient(t.TempDir())
	if _, err := app.RecordTerminalEdit(task.ID, SubjectText{task.Title, "", ""}, SubjectText{task.Title, "post-merge notes on why", "follow-up in task-099"}, "dashboard"); err != nil {
		t.Fatalf("merged task not editable: %v", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateMerged || got.Notes != "follow-up in task-099" {
		t.Errorf("merged task after edit: %s %q", got.State, got.Notes)
	}

	proj, _ := app.DB.CreateProject("P", "")
	epic, _ := app.DB.CreateEpic("E", "", proj.ID, "", ReviewSetting{})
	app.DB.UpdateEpicStatus(epic.ID, EpicStatusClosed)
	if _, err := app.RecordTerminalEdit(epic.ID, SubjectText{"E", "", ""}, SubjectText{"Epic renamed", "desc", "epic notes"}, "dashboard"); err != nil {
		t.Fatal(err)
	}
	e, _ := app.DB.GetEpic(epic.ID)
	if e.Title != "Epic renamed" || e.Description != "desc" || e.Notes != "epic notes" {
		t.Errorf("epic = %+v", e)
	}
	if edits, _ := app.DB.ListEdits(epic.ID); len(edits) != 1 || edits[0].Fields != "title, description, notes" {
		t.Errorf("epic edits = %+v", edits)
	}
}

func TestNotesInReviewPrompt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, prompt, notes, want string
	}{
		{"placeholder", "{{task_id}}|{{task_notes}}|{{diff}}", "keep the API stable", "task-001|keep the API stable|+ {{task_notes}}"},
		{"placeholder, empty notes", "{{task_id}}|{{task_notes}}|", "", "task-001||"},
		{"auto-append", "{{task_id}}", "keep the API stable\n",
			"task-001\n\nNotes from the human (context and constraints for this task):\nkeep the API stable\n"},
		{"no placeholder, empty notes", "{{task_id}}", "  ", "task-001"},
	} {
		diff := "+ {{task_notes}}"
		if got := composeReviewPrompt(ReviewConfig{Prompt: tc.prompt}, "task-001", "T", tc.notes, diff, "none"); got != tc.want {
			t.Errorf("%s: prompt = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Not parallel: swaps package-level hooks and the process environment.
func TestRecordTerminalEditRefusals(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, _ := app.DB.CreateTask("t", "", "b", "/tmp/x", "main", "", "", ReviewSetting{})

	t.Run("token env set", func(t *testing.T) {
		t.Setenv(ReviewTokenEnv, "anything")
		if _, err := app.RecordTerminalEdit(task.ID, SubjectText{"t", "", ""}, SubjectText{"x", "", ""}, "dashboard"); err == nil || !strings.Contains(err.Error(), "reviewer's environment") {
			t.Errorf("err = %v", err)
		}
	})
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()
	if _, err := app.RecordTerminalEdit(task.ID, SubjectText{"t", "", ""}, SubjectText{"x", "", ""}, "dashboard"); err == nil || !strings.Contains(err.Error(), "not an interactive terminal") {
		t.Errorf("without tty: err = %v", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.Title != "t" {
		t.Errorf("title changed after refusals: %q", got.Title)
	}
}

// A database migrated through v3 once had tasks.notes; v6 dropped it. v14
// must add the column only where it is missing.
func TestMigrateV14ToleratesExistingNotesColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`UPDATE schema_version SET version = 13`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`DROP TABLE subject_edits`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`ALTER TABLE epics DROP COLUMN notes`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = OpenDB(path)
	if err != nil {
		t.Fatalf("reopen with tasks.notes already present: %v", err)
	}
	defer db.Close()
	var v int
	db.conn.QueryRow(`SELECT version FROM schema_version`).Scan(&v)
	if v < 14 {
		t.Errorf("version = %d, want >= 14", v)
	}
	for _, table := range []string{"tasks", "epics"} {
		var n int
		db.conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'notes'`, table).Scan(&n)
		if n != 1 {
			t.Errorf("%s.notes columns = %d", table, n)
		}
	}
	var name sql.NullString
	db.conn.QueryRow(`SELECT name FROM sqlite_master WHERE name = 'subject_edits'`).Scan(&name)
	if name.String != "subject_edits" {
		t.Error("subject_edits missing")
	}
}
