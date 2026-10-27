package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseReviewType(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]ReviewType{"": ReviewDefault, "default": ReviewDefault, "human": ReviewHuman, "none": ReviewNone} {
		got, err := ParseReviewType(in)
		if err != nil || got != want {
			t.Errorf("ParseReviewType(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"agent", "NONE", "skip"} {
		if _, err := ParseReviewType(in); err == nil {
			t.Errorf("ParseReviewType(%q) accepted", in)
		}
	}
}

func TestMigrationV12AddsReviewTypeToExistingDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.db")
	db, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`ALTER TABLE tasks DROP COLUMN review_type`,
		`ALTER TABLE tasks DROP COLUMN review_type_set_by`,
		`ALTER TABLE tasks DROP COLUMN review_type_set_at`,
		`ALTER TABLE epics DROP COLUMN review_type`,
		`ALTER TABLE epics DROP COLUMN review_type_set_by`,
		`ALTER TABLE epics DROP COLUMN review_type_set_at`,
		`ALTER TABLE task_state_transitions DROP COLUMN note`,
		`ALTER TABLE epic_state_transitions DROP COLUMN note`,
		`UPDATE schema_version SET version = 11`,
		`INSERT INTO tasks (id, title, state, created_at, updated_at) VALUES ('task-001', 'old', 'PLANNED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO projects (id, title, created_at, updated_at) VALUES ('proj-001', 'p', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		`INSERT INTO epics (id, project_id, title, created_at, updated_at) VALUES ('epic-001', 'proj-001', 'e', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
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
	var version int
	db.conn.QueryRow(`SELECT version FROM schema_version`).Scan(&version)
	if version < 12 {
		t.Errorf("schema version = %d, want >= 12", version)
	}
	task, err := db.GetTask("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if task.ReviewType != ReviewDefault || task.ReviewTypeSetBy != "" || !task.ReviewTypeSetAt.IsZero() {
		t.Errorf("migrated task review = %q/%q/%v", task.ReviewType, task.ReviewTypeSetBy, task.ReviewTypeSetAt)
	}
	epic, err := db.GetEpic("epic-001")
	if err != nil {
		t.Fatal(err)
	}
	if epic.ReviewType != ReviewDefault || epic.ReviewTypeSetBy != "" || !epic.ReviewTypeSetAt.IsZero() {
		t.Errorf("migrated epic review = %q/%q/%v", epic.ReviewType, epic.ReviewTypeSetBy, epic.ReviewTypeSetAt)
	}
}

func TestSetReviewType(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()
	task, err := db.CreatePlannedTask("t", "", "", ReviewSetting{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetReviewType(task.ID, ReviewHuman, "terminal:alice (dashboard)"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetTask(task.ID)
	if got.ReviewType != ReviewHuman || got.ReviewTypeSetBy != "terminal:alice (dashboard)" || got.ReviewTypeSetAt.IsZero() {
		t.Errorf("task review = %q/%q/%v", got.ReviewType, got.ReviewTypeSetBy, got.ReviewTypeSetAt)
	}
	if err := db.SetReviewType(task.ID, "bogus", "x"); err == nil {
		t.Error("invalid review type accepted")
	}
	if err := db.SetReviewType("task-999", ReviewNone, "x"); err == nil {
		t.Error("missing task accepted")
	}
	if err := db.SetReviewType("epic-999", ReviewNone, "x"); err == nil {
		t.Error("missing epic accepted")
	}
}

// commitAllowNone commits review.allow_none: true to main in the app repo.
func commitAllowNone(t *testing.T, dir string) {
	t.Helper()
	writeAllowNone(t, dir)
	gitRun(t, dir, "add", ".harness.yaml")
	gitRun(t, dir, "commit", "-m", "allow none")
}

func writeAllowNone(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, ".harness.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := strings.Replace(string(data), "review:\n", "review:\n  allow_none: true\n", 1)
	if cfg == string(data) {
		t.Fatal("no review: section in scaffold")
	}
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestReviewNoneRefusedWithoutAllowNone(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	for name, create := range map[string]func() error{
		"new":      func() error { return app.CmdNew("nr", "", "", "", "none") },
		"task new": func() error { return app.CmdTaskNew("nr", "", "", "none") },
		"epic new": func() error { return app.CmdEpicNew("nr", "", "proj-001", "", false, "none") },
	} {
		err := create()
		if err == nil || !strings.Contains(err.Error(), "review.allow_none") {
			t.Errorf("%s --review=none: err = %v, want allow_none refusal", name, err)
		}
	}
	if tasks, _ := app.DB.ListTasks(); len(tasks) != 0 {
		t.Errorf("refused creation left %d tasks", len(tasks))
	}
	if epics, _ := app.DB.ListEpics(); len(epics) != 0 {
		t.Errorf("refused creation left %d epics", len(epics))
	}
}

func TestReviewNoneIgnoresWorkingTreeAllowNone(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()
	writeAllowNone(t, dir)
	app2, err := LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer app2.Close()
	if !app2.Config.Review.AllowNone {
		t.Fatal("working-tree config should parse allow_none")
	}
	if err := app2.CmdTaskNew("nr", "", "", "none"); err == nil {
		t.Error("uncommitted allow_none enabled --review=none")
	}
}

func TestReviewNoneAcceptedWithCommittedAllowNone(t *testing.T) {
	t.Parallel()
	app, dir := setupApp(t)
	defer app.Close()
	commitAllowNone(t, dir)
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdNew("active none", "", "", "", "none"); err != nil {
		t.Fatal("new:", err)
	}
	if err := app.CmdTaskNew("planned none", "", "", "none"); err != nil {
		t.Fatal("task new:", err)
	}
	if err := app.CmdEpicNew("epic none", "", "proj-001", "", false, "none"); err != nil {
		t.Fatal("epic new:", err)
	}
	for _, id := range []string{"task-001", "task-002"} {
		task, _ := app.DB.GetTask(id)
		if task.ReviewType != ReviewNone || task.ReviewTypeSetBy != ReviewSetBySelfDeclared || task.ReviewTypeSetAt.IsZero() {
			t.Errorf("%s review = %q/%q/%v", id, task.ReviewType, task.ReviewTypeSetBy, task.ReviewTypeSetAt)
		}
	}
	epic, _ := app.DB.GetEpic("epic-001")
	if epic.ReviewType != ReviewNone || epic.ReviewTypeSetBy != ReviewSetBySelfDeclared || epic.ReviewTypeSetAt.IsZero() {
		t.Errorf("epic review = %q/%q/%v", epic.ReviewType, epic.ReviewTypeSetBy, epic.ReviewTypeSetAt)
	}
}

func TestReviewFlagSetsTypeAndTaskStartKeepsIt(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdNew("active human", "", "", "", "human"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdTaskNew("planned human", "", "", "human"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdTaskNew("planned default", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("epic human", "", "proj-001", "", false, "human"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdTaskNew("bad", "", "", "agent"); err == nil {
		t.Error("--review=agent accepted")
	}
	if err := app.CmdTaskStart("task-002", ""); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]ReviewType{"task-001": ReviewHuman, "task-002": ReviewHuman, "task-003": ReviewDefault} {
		task, _ := app.DB.GetTask(id)
		if task.ReviewType != want || task.ReviewTypeSetBy != ReviewSetByCreator || task.ReviewTypeSetAt.IsZero() {
			t.Errorf("%s review = %q/%q/%v, want %q by creator", id, task.ReviewType, task.ReviewTypeSetBy, task.ReviewTypeSetAt, want)
		}
	}
	started, _ := app.DB.GetTask("task-002")
	if started.State != StateInProgress {
		t.Errorf("task-002 state = %s", started.State)
	}
	epic, _ := app.DB.GetEpic("epic-001")
	if epic.ReviewType != ReviewHuman || epic.ReviewTypeSetBy != ReviewSetByCreator {
		t.Errorf("epic review = %q/%q", epic.ReviewType, epic.ReviewTypeSetBy)
	}
}

func TestReviewTypeShownInCLI(t *testing.T) {
	app, dir := setupApp(t)
	defer app.Close()
	commitAllowNone(t, dir)
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, "human"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdNew("no review", "", "", "", "none"); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdTaskNew("planned", "", "epic-001", "human"); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error { return app.CmdWorkStatus("task-001") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Review type: none · self-declared (allow_none) · ") {
		t.Errorf("work status missing review type:\n%s", out)
	}

	out, err = captureStdout(t, func() error { return app.CmdTaskList("", "", "all", false) })
	if err != nil {
		t.Fatal(err)
	}
	header := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(header, "Review") {
		t.Errorf("task list header has no Review column: %q", header)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "task-001") && !strings.Contains(line, " none ") {
			t.Errorf("task list row missing none: %q", line)
		}
		if strings.HasPrefix(line, "task-002") && !strings.Contains(line, " human ") {
			t.Errorf("task list row missing human: %q", line)
		}
	}

	out, err = captureStdout(t, func() error { return app.CmdEpicView("epic-001") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Review type: human · creator · ") {
		t.Errorf("epic view missing review type:\n%s", out)
	}

	out, err = captureStdout(t, func() error { return app.CmdReviewHistoryDispatch("task-001") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Review type: none · self-declared (allow_none) · ") {
		t.Errorf("review history missing review type:\n%s", out)
	}
	out, err = captureStdout(t, func() error { return app.CmdReviewHistoryDispatch("epic-001") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Review type: human · creator · ") {
		t.Errorf("epic review history missing review type:\n%s", out)
	}
}

func TestFormatReviewTypeCompactDefault(t *testing.T) {
	t.Parallel()
	if got := FormatReviewType(ReviewDefault, "", time.Time{}); got != "default" {
		t.Errorf("legacy default = %q", got)
	}
	if got := FormatReviewType(ReviewDefault, ReviewSetByCreator, time.Now()); got != "default" {
		t.Errorf("creator default = %q", got)
	}
	at := time.Date(2026, 10, 27, 11, 40, 0, 0, time.Local)
	if got := FormatReviewType(ReviewDefault, "terminal:alice (dashboard)", at); got != "default · terminal:alice (dashboard) · 2026-10-27 11:40" {
		t.Errorf("dashboard default = %q", got)
	}
}
