package internal

import (
	"strings"
	"testing"
)

func TestRemovePreApprovalTogglesType(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, err := app.DB.CreateTask("t", "", "b", "/tmp/x", "main", "", "", ReviewSetting{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", ""); err == nil || !strings.Contains(err.Error(), "not pre-approved") {
		t.Errorf("remove on a default task: err = %v", err)
	}
	if _, err := app.RecordTerminalReviewType(task.ID, ReviewNone, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	report, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", "needs a look after all")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := app.DB.GetTask(task.ID)
	want := ReviewSetByPreApprovalRemoved + "terminal:" + terminalUser() + " (dashboard): needs a look after all"
	if got.ReviewType != ReviewDefault || got.ReviewTypeSetBy != want || got.State != StateInProgress {
		t.Errorf("after remove: %s %q %s, want default %q", got.ReviewType, got.ReviewTypeSetBy, got.State, want)
	}
	if !strings.Contains(report, "default") {
		t.Errorf("report = %q", report)
	}
}

func TestRemovePreApprovalDropsDashboardApproval(t *testing.T) {
	app, task := requestedTask(t)
	if _, err := app.RecordTerminalStop(task.ID, StopApprove, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalReviewType(task.ID, ReviewNone, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateApproved {
		t.Fatalf("setup: state %s", got.State)
	}

	report, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := app.DB.GetTask(task.ID)
	if got.State != StateInProgress || got.ReviewType != ReviewDefault {
		t.Errorf("after remove: %s %s, want IN_PROGRESS default", got.State, got.ReviewType)
	}
	if !strings.Contains(report, "IN_PROGRESS") {
		t.Errorf("report = %q", report)
	}
	if err := app.CmdWorkMerge(task.ID); err == nil {
		t.Error("merge accepted after the pre-approval was removed")
	}
	trs, _ := app.DB.GetTaskTransitions(task.ID)
	if last := trs[len(trs)-1]; last.FromState != string(StateApproved) || last.ToState != string(StateInProgress) {
		t.Errorf("last transition = %+v", last)
	}
}

func TestRemovePreApprovalKeepsHumanApproval(t *testing.T) {
	app, task := requestedTask(t)
	if _, err := app.RecordTerminalVerdict(task.ID, "approve", "", "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalReviewType(task.ID, ReviewNone, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateApproved {
		t.Errorf("a real review's approval was dropped: %s", got.State)
	}
}

func TestRemovePreApprovalRefusedPastGate(t *testing.T) {
	for _, state := range []TaskState{StateQueued, StateMerging} {
		app, task := setupTaskInState(t, state)
		app.DB.SetReviewType(task.ID, ReviewNone, ReviewSetByPreApproved+"terminal:x (dashboard)")
		_, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", "")
		if err == nil || !strings.Contains(err.Error(), "past the review gate") {
			t.Errorf("%s: err = %v", state, err)
		}
		if got, _ := app.DB.GetTask(task.ID); got.ReviewType != ReviewNone {
			t.Errorf("%s: type changed to %s", state, got.ReviewType)
		}
	}
}

func TestRemovePreApprovalEpic(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false, ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalStop("epic-001", StopApprove, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalReviewType("epic-001", ReviewNone, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalRemovePreApproval("epic-001", "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if e, _ := app.DB.GetEpic("epic-001"); e.ReviewType != ReviewDefault {
		t.Errorf("epic type = %s", e.ReviewType)
	}
	err := app.CmdEpicMerge("epic-001")
	if err == nil || !strings.Contains(err.Error(), "pre-approval") {
		t.Errorf("epic merge after removed pre-approval: err = %v", err)
	}
}

// Not parallel: swaps package-level hooks and the process environment.
func TestRemovePreApprovalRefusals(t *testing.T) {
	app, _ := setupApp(t)
	defer app.Close()
	task, _ := app.DB.CreateTask("t", "", "b", "/tmp/x", "main", "", "", ReviewSetting{})
	app.DB.SetReviewType(task.ID, ReviewNone, ReviewSetByPreApproved+"terminal:x (dashboard)")

	t.Run("token env set", func(t *testing.T) {
		t.Setenv(ReviewTokenEnv, "anything")
		if _, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", ""); err == nil || !strings.Contains(err.Error(), "reviewer's environment") {
			t.Errorf("err = %v", err)
		}
	})
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()
	if _, err := app.RecordTerminalRemovePreApproval(task.ID, "dashboard", ""); err == nil || !strings.Contains(err.Error(), "not an interactive terminal") {
		t.Errorf("without tty: err = %v", err)
	}
	if got, _ := app.DB.GetTask(task.ID); got.ReviewType != ReviewNone {
		t.Errorf("type changed after refusals: %s", got.ReviewType)
	}
}
