package internal

import (
	"io"
	"os"
	"strings"
	"testing"
)

// Swaps the global os.Stdout, so callers must not run in parallel.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = orig
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

func TestCmdProjectViewPrintsTaskTitle(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", true); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdTaskNew("the title", "the description", "epic-001"); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error { return app.CmdProjectView("proj-001") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "the title") || strings.Contains(out, "the description") {
		t.Errorf("project view should list task title, not description; got:\n%s", out)
	}
}

func TestCmdEpicViewMissingProject(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.conn.Exec(`UPDATE epics SET project_id = 'proj-999' WHERE id = 'epic-001'`); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicView("epic-001"); err == nil {
		t.Error("expected error viewing epic whose project is missing")
	}
}

func TestCmdEpicEditProject(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	for _, title := range []string{"P1", "P2"} {
		if err := app.CmdProjectNew(title, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", true); err != nil {
		t.Fatal(err)
	}

	if err := app.CmdEpicEdit("epic-001", "", "", "", false, false, true); err == nil {
		t.Error("expected error for empty --project")
	}
	if err := app.CmdEpicEdit("epic-001", "", "", "proj-999", false, false, true); err == nil {
		t.Error("expected error for nonexistent --project")
	}
	e, _ := app.DB.GetEpic("epic-001")
	if e.ProjectID != "proj-001" {
		t.Fatalf("project changed by failed edit: %q", e.ProjectID)
	}

	if err := app.CmdEpicEdit("epic-001", "", "", "proj-002", false, false, true); err != nil {
		t.Fatal(err)
	}
	e, _ = app.DB.GetEpic("epic-001")
	if e.ProjectID != "proj-002" {
		t.Errorf("project = %q, want proj-002", e.ProjectID)
	}
}

func TestCmdCheckFromApproved(t *testing.T) {
	t.Parallel()
	app, _, task := setupTask(t)
	defer app.Close()
	if _, err := app.DB.conn.Exec(`UPDATE tasks SET state = ? WHERE id = ?`, string(StateApproved), task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal("check from APPROVED:", err)
	}
	got, _ := app.DB.GetTask(task.ID)
	if got.State != StateChecksPassed {
		t.Errorf("state = %s, want CHECKS_PASSED", got.State)
	}
}
