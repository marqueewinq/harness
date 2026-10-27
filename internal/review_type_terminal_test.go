package internal

import (
	"strings"
	"testing"
)

func TestRecordTerminalReviewType(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, err := app.DB.CreateTask("t", "", "b", "/tmp/x", "main", "", "", ReviewSetting{})
	if err != nil {
		t.Fatal(err)
	}
	user := terminalUser()

	report, err := app.RecordTerminalReviewType(task.ID, ReviewNone, "dashboard", "trivial docs change")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := app.DB.GetTask(task.ID)
	wantBy := "pre-approved by terminal:" + user + " (dashboard): trivial docs change"
	if got.ReviewType != ReviewNone || got.ReviewTypeSetBy != wantBy || got.ReviewTypeSetAt.IsZero() {
		t.Errorf("after pre-approve: %s %q %v, want none %q", got.ReviewType, got.ReviewTypeSetBy, got.ReviewTypeSetAt, wantBy)
	}
	if !strings.Contains(report, "none") || !strings.Contains(report, task.ID) {
		t.Errorf("report = %q", report)
	}

	if _, err := app.RecordTerminalReviewType(task.ID, ReviewHuman, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = app.DB.GetTask(task.ID)
	if got.ReviewType != ReviewHuman || got.ReviewTypeSetBy != "set by terminal:"+user+" (dashboard)" {
		t.Errorf("after human: %s %q", got.ReviewType, got.ReviewTypeSetBy)
	}

	proj, _ := app.DB.CreateProject("P", "")
	epic, _ := app.DB.CreateEpic("E", "", proj.ID, "", ReviewSetting{})
	if _, err := app.RecordTerminalReviewType(epic.ID, ReviewNone, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if e, _ := app.DB.GetEpic(epic.ID); e.ReviewType != ReviewNone || e.ReviewTypeSetBy != "pre-approved by terminal:"+user+" (dashboard)" {
		t.Errorf("epic: %s %q", e.ReviewType, e.ReviewTypeSetBy)
	}

	if _, err := app.RecordTerminalReviewType(task.ID, "sometimes", "dashboard", ""); err == nil {
		t.Error("invalid type accepted")
	}
}

// Not parallel: swaps package-level hooks and the process environment.
func TestRecordTerminalReviewTypeRefusals(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, _ := app.DB.CreateTask("t", "", "b", "/tmp/x", "main", "", "", ReviewSetting{})

	t.Run("token env set", func(t *testing.T) {
		t.Setenv(ReviewTokenEnv, "anything")
		_, err := app.RecordTerminalReviewType(task.ID, ReviewNone, "dashboard", "")
		if err == nil || !strings.Contains(err.Error(), "reviewer's environment") {
			t.Errorf("err = %v", err)
		}
	})

	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()
	_, err := app.RecordTerminalReviewType(task.ID, ReviewNone, "dashboard", "")
	if err == nil || !strings.Contains(err.Error(), "not an interactive terminal") {
		t.Errorf("without tty: err = %v", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.ReviewType != ReviewDefault {
		t.Errorf("type changed after refusals: %s", got.ReviewType)
	}
}
