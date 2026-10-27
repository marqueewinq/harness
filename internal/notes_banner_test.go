package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const bannerHead = "── New notes from the human on "

func editNotes(t *testing.T, app *App, subject, notes string) {
	t.Helper()
	var cur SubjectText
	if IsEpicID(subject) {
		e, err := app.DB.GetEpic(subject)
		if err != nil {
			t.Fatal(err)
		}
		cur = SubjectText{e.Title, e.Description, e.Notes}
	} else {
		task, err := app.DB.GetTask(subject)
		if err != nil {
			t.Fatal(err)
		}
		cur = SubjectText{task.Title, task.Description, task.Notes}
	}
	next := cur
	next.Notes = notes
	if _, err := app.RecordTerminalEdit(subject, cur, next, "dashboard"); err != nil {
		t.Fatal(err)
	}
}

// Not parallel: captureStdout swaps os.Stdout.
func TestNotesBannerShownOnceAndAfterEachEdit(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()
	diff := func() error { return app.CmdWorkDiff(task.ID) }

	if out, _ := captureStdout(t, diff); strings.Contains(out, bannerHead) {
		t.Fatalf("banner without notes:\n%s", out)
	}
	editNotes(t, app, task.ID, "use sqlite only\nno new deps")
	out, err := captureStdout(t, diff)
	if err != nil {
		t.Fatal(err)
	}
	head := strings.Index(out, bannerHead+task.ID+" (edited ")
	if head != 0 || !strings.Contains(out, "by terminal:") || !strings.Contains(out, "use sqlite only\nno new deps\n──\n") ||
		!strings.Contains(out, "instructions and context from the human") {
		t.Fatalf("banner missing or not first:\n%s", out)
	}
	if !strings.Contains(out[head:], "feature.txt") {
		t.Errorf("command output missing after banner:\n%s", out)
	}
	if out, _ := captureStdout(t, diff); strings.Contains(out, bannerHead) {
		t.Errorf("banner shown twice:\n%s", out)
	}

	editNotes(t, app, task.ID, "actually, postgres")
	if out, _ := captureStdout(t, diff); !strings.Contains(out, bannerHead) || !strings.Contains(out, "actually, postgres") {
		t.Errorf("banner not shown after a second edit:\n%s", out)
	}
	editNotes(t, app, task.ID, "")
	if out, _ := captureStdout(t, diff); !strings.Contains(out, "(the notes were cleared)") {
		t.Errorf("clearing the notes not announced:\n%s", out)
	}
}

func TestStatusAndViewMarkNotesSeen(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()
	for name, show := range map[string]func() error{
		"work status": func() error { return app.CmdWorkStatus(task.ID) },
		"task view":   func() error { return app.CmdTaskView(task.ID) },
	} {
		editNotes(t, app, task.ID, "notes for "+name)
		if out, _ := captureStdout(t, show); !strings.Contains(out, "notes for "+name) {
			t.Fatalf("%s does not print notes:\n%s", name, out)
		}
		if out, _ := captureStdout(t, func() error { return app.CmdWorkDiff(task.ID) }); strings.Contains(out, bannerHead) {
			t.Errorf("banner after %s already showed the notes:\n%s", name, out)
		}
	}
}

func TestNotesBannerOnTaskCommands(t *testing.T) {
	cases := map[string]func(app *App, id string) error{
		"work check":     func(app *App, id string) error { return app.CmdWorkCheck(id) },
		"work commit":    func(app *App, id string) error { return app.CmdWorkCommit(id, "nothing to commit", nil) },
		"work diff":      func(app *App, id string) error { return app.CmdWorkDiff(id) },
		"work sync":      func(app *App, id string) error { return app.CmdWorkSync(id, false, false) },
		"review request": func(app *App, id string) error { return app.CmdReviewRequest(id) },
		"work merge":     func(app *App, id string) error { return app.CmdWorkMerge(id) },
		"work close":     func(app *App, id string) error { return app.CmdWorkClose(id, false) },
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			app, _, task := setupTask(t)
			defer app.Close()
			editNotes(t, app, task.ID, "note for "+name)
			out, _ := captureStdout(t, func() error { return cmd(app, task.ID) })
			if !strings.HasPrefix(out, bannerHead+task.ID) || !strings.Contains(out, "note for "+name) {
				t.Errorf("no banner first:\n%s", out)
			}
		})
	}

	t.Run("task start", func(t *testing.T) {
		app, _ := setupApp(t)
		defer app.Close()
		if err := app.CmdTaskNew("planned", "", "", ""); err != nil {
			t.Fatal(err)
		}
		editNotes(t, app, "task-001", "start here")
		out, err := captureStdout(t, func() error { return app.CmdTaskStart("task-001", "") })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out, bannerHead+"task-001") || !strings.Contains(out, "Started task-001") {
			t.Errorf("task start output:\n%s", out)
		}
	})
}

func TestNotesBannerKeepsCommandError(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()
	editNotes(t, app, task.ID, "careful")
	out, err := captureStdout(t, func() error { return app.CmdWorkMerge(task.ID) })
	if err == nil || !strings.Contains(err.Error(), "cannot merge in state IN_PROGRESS") {
		t.Errorf("merge err = %v, want the usual state refusal", err)
	}
	if !strings.Contains(out, "careful") {
		t.Errorf("banner missing:\n%s", out)
	}
}

func TestEpicNotesBanner(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, ""); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"one", "two"} {
		if err := app.CmdTaskNew(title, "", "epic-001", ""); err != nil {
			t.Fatal(err)
		}
	}
	editNotes(t, app, "epic-001", "epic-wide: keep the CLI flags stable")

	for _, id := range []string{"task-001", "task-002"} {
		out, err := captureStdout(t, func() error { return app.CmdTaskStart(id, "") })
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, bannerHead+"epic-001 (the epic of "+id+")") || !strings.Contains(out, "keep the CLI flags stable") {
			t.Errorf("%s: no epic banner:\n%s", id, out)
		}
		if out, _ := captureStdout(t, func() error { return app.CmdWorkDiff(id) }); strings.Contains(out, bannerHead) {
			t.Errorf("%s: epic banner shown twice:\n%s", id, out)
		}
	}

	out, _ := captureStdout(t, func() error { return app.CmdEpicMerge("epic-001") })
	if !strings.HasPrefix(out, bannerHead+"epic-001 (edited") {
		t.Errorf("epic command: no epic banner:\n%s", out)
	}
	if out, _ := captureStdout(t, func() error { return app.CmdEpicMerge("epic-001") }); strings.Contains(out, bannerHead) {
		t.Errorf("epic banner shown twice on epic commands:\n%s", out)
	}
	if out, _ := captureStdout(t, func() error { return app.CmdEpicView("epic-001") }); !strings.Contains(out, "keep the CLI flags stable") {
		t.Errorf("epic view lacks notes:\n%s", out)
	}
}

func TestMigrationV15BackfillsNotesChange(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "harness.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO tasks (id, title, state, notes, created_at, updated_at) VALUES ('task-001', 't', 'PLANNED', 'old notes', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO subject_edits (subject_id, fields, edited_by, created_at) VALUES ('task-001', 'title, notes', 'terminal:alice (dashboard)', '2026-10-27 10:12:00')`,
		`INSERT INTO subject_edits (subject_id, fields, edited_by, created_at) VALUES ('task-001', 'title', 'terminal:bob (dashboard)', '2026-10-27 11:00:00')`,
		`ALTER TABLE tasks DROP COLUMN notes_changed_at`,
		`ALTER TABLE tasks DROP COLUMN notes_changed_by`,
		`ALTER TABLE tasks DROP COLUMN notes_seen_at`,
		`ALTER TABLE tasks DROP COLUMN epic_notes_seen_at`,
		`ALTER TABLE epics DROP COLUMN notes_changed_at`,
		`ALTER TABLE epics DROP COLUMN notes_changed_by`,
		`ALTER TABLE epics DROP COLUMN notes_seen_at`,
		`UPDATE schema_version SET version = 14`,
	} {
		if _, err := db.conn.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	db.Close()
	db, err = OpenDB(path)
	if err != nil {
		t.Fatal("reopen:", err)
	}
	defer db.Close()
	var by string
	var changed, seen interface{}
	if err := db.conn.QueryRow(`SELECT notes_changed_by, notes_changed_at, notes_seen_at FROM tasks WHERE id = 'task-001'`).Scan(&by, &changed, &seen); err != nil {
		t.Fatal(err)
	}
	if by != "terminal:alice (dashboard)" || changed == nil || seen != nil {
		t.Errorf("backfill = %q / %v / %v", by, changed, seen)
	}
}

func TestReviewerPromptCarriesNotes(t *testing.T) {
	t.Parallel()
	promptCopy := filepath.Join(t.TempDir(), "prompt.txt")
	app, _ := setupAppWithReviewCmd(t, "cp {{prompt_file}} "+promptCopy)
	defer app.Close()
	if err := app.CmdNew("noted", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	editNotes(t, app, task.ID, "do not touch the public API")
	app.CmdReviewRequest(task.ID) // the reviewer records no verdict
	data, err := os.ReadFile(promptCopy)
	if err != nil {
		t.Fatal("reviewer did not run:", err)
	}
	if !strings.Contains(string(data), "Notes from the human (context and constraints for this task):\ndo not touch the public API") {
		t.Errorf("prompt lacks the notes:\n%s", data)
	}
}
