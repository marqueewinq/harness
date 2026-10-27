package dashboard

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marqueewinq/harness/internal"
)

// Row indexes in sampleSnapshot: 2 is the epic-002 header (open request at
// 5d1c09aa999), 3 task-025, 6 task-030, 8 the merged task-024.
const (
	rowEpicHeader = 2
	rowTask025    = 3
	rowTask030    = 6
	rowMerged     = 8
)

// inReviewSnapshot puts task-025 in REVIEW_REQUESTED with an open request.
func inReviewSnapshot() Snapshot {
	s := sampleSnapshot()
	t := &s.Groups[0].Tasks[0]
	t.State = internal.StateReviewRequested
	t.Review = &internal.ReviewRequest{CommitSHA: "abc12345ff"}
	return s
}

func selectRow(t *testing.T, m Model, i int) Model {
	t.Helper()
	for m.ui.Selected < i {
		m = press(t, m, "j")
	}
	if m.ui.Selected != i {
		t.Fatalf("selected %d, want %d", m.ui.Selected, i)
	}
	return m
}

// confirm answers the y/N chooser for subject id with yes.
func confirm(t *testing.T, m Model, id string) Model {
	t.Helper()
	if m.ui.Modal == nil || m.ui.Modal.Step != StepConfirm || m.ui.Modal.Subject != id {
		t.Fatalf("not at the confirm step for %s: %+v", id, m.ui.Modal)
	}
	return press(t, m, "y")
}

func TestPreApproveWithoutReviewOnlySetsType(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := selectRow(t, started(t, f), rowTask030)
	m = press(t, m, "p")
	if m.ui.Modal == nil || m.ui.Modal.Decision != DecisionPreApprove || m.ui.Modal.SHA != "" {
		t.Fatalf("modal = %+v", m.ui.Modal)
	}
	if v := m.View(); !strings.Contains(v, "Pre-approve task-030 (review type none)?") || !strings.Contains(v, "Reason (optional") {
		t.Errorf("pre-approve modal:\n%s", v)
	}
	m = press(t, m, "enter")
	if !strings.Contains(m.View(), "yes, don't ask again") {
		t.Errorf("no y/N chooser:\n%s", m.View())
	}
	m = confirm(t, m, "task-030")
	if want := []string{`type task-030 none ""`}; !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if m.ui.StatusErr || !strings.Contains(m.ui.Status, "now none") {
		t.Errorf("status = %q", m.ui.Status)
	}
}

func TestPreApproveInFlightStopsReviewFirst(t *testing.T) {
	f := &fakeLoader{snap: inReviewSnapshot()}
	m := selectRow(t, started(t, f), rowTask025)
	m = press(t, m, "p")
	if !strings.Contains(m.View(), "Approves the requested commit abc12345") {
		t.Errorf("in-flight note missing:\n%s", m.View())
	}
	m = typeText(t, m, "docs only")
	m = press(t, m, "enter")
	m = confirm(t, m, "task-025")
	want := []string{"stop task-025 approve abc12345ff", `type task-025 none "docs only"`}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if m.ui.StatusErr || !strings.HasPrefix(m.ui.Status, "Review stopped: approve") {
		t.Errorf("status = %q", m.ui.Status)
	}
}

func TestPreApproveRefusedStopLeavesType(t *testing.T) {
	f := &fakeLoader{snap: inReviewSnapshot(), stopErr: errors.New("review request for task-025 changed since you opened it")}
	m := selectRow(t, started(t, f), rowTask025)
	m = press(t, m, "p", "enter")
	m = confirm(t, m, "task-025")
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "stop ") {
		t.Errorf("type set after a refused stop: %v", f.calls)
	}
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "changed since you opened it") {
		t.Errorf("status = %q", m.ui.Status)
	}
}

func TestTypeSetFailsAfterStop(t *testing.T) {
	f := &fakeLoader{snap: inReviewSnapshot(), typeErr: errors.New("database is locked")}
	m := selectRow(t, started(t, f), rowTask025)
	m = press(t, m, "p", "enter")
	m = confirm(t, m, "task-025")
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "review stopped") || !strings.Contains(m.ui.Status, "database is locked") {
		t.Errorf("status = %q", m.ui.Status)
	}
}

func TestTypePicker(t *testing.T) {
	cases := []struct {
		name  string
		snap  Snapshot
		row   int
		setup func(*Snapshot)
		keys  []string
		id    string
		want  []string
	}{
		{"mid-review to human revokes", inReviewSnapshot(), rowTask025, nil, []string{"j"}, "task-025",
			[]string{"stop task-025 revoke abc12345ff", `type task-025 human ""`}},
		{"mid-review to none approves", inReviewSnapshot(), rowTask025, nil, []string{"j", "j"}, "task-025",
			[]string{"stop task-025 approve abc12345ff", `type task-025 none ""`}},
		{"no review to human", sampleSnapshot(), rowTask030, nil, []string{"j"}, "task-030",
			[]string{`type task-030 human ""`}},
		{"human to default mid-review only sets type", inReviewSnapshot(), rowTask025,
			func(s *Snapshot) { s.Groups[0].Tasks[0].ReviewType = internal.ReviewHuman }, []string{"k"}, "task-025",
			[]string{`type task-025 default ""`}},
		{"none to default keeps state", sampleSnapshot(), 5,
			func(s *Snapshot) { s.Groups[0].Tasks[2].ReviewType = internal.ReviewNone }, []string{"k", "k"}, "task-028",
			[]string{`type task-028 default ""`}},
		{"epic header mid-review to none", sampleSnapshot(), rowEpicHeader,
			func(s *Snapshot) { s.Groups[0].Epic.ReviewType = internal.ReviewDefault }, []string{"j", "j"}, "epic-002",
			[]string{"stop epic-002 approve 5d1c09aa999", `type epic-002 none ""`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.snap
			s.Groups = append([]EpicGroup{}, s.Groups...)
			g0 := s.Groups[0]
			epic := *g0.Epic
			g0.Epic = &epic
			g0.Tasks = append([]TaskRow{}, g0.Tasks...)
			s.Groups[0] = g0
			if tc.setup != nil {
				tc.setup(&s)
			}
			f := &fakeLoader{snap: s}
			m := selectRow(t, started(t, f), tc.row)
			m = press(t, m, "t")
			if m.ui.Modal == nil || m.ui.Modal.Step != StepPick {
				t.Fatalf("picker not open: %+v status=%q", m.ui.Modal, m.ui.Status)
			}
			m = press(t, m, tc.keys...)
			m = press(t, m, "enter")
			if m.ui.Modal == nil || m.ui.Modal.Step != StepConfirm {
				t.Fatalf("pick not accepted: %+v", m.ui.Modal)
			}
			m = confirm(t, m, tc.id)
			if !reflect.DeepEqual(f.calls, tc.want) {
				t.Errorf("calls = %v, want %v", f.calls, tc.want)
			}
		})
	}
}

func TestTypePickerRejectsCurrentTypeAndEsc(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := selectRow(t, started(t, f), rowTask030)
	m = press(t, m, "t")
	if v := m.View(); !strings.Contains(v, "Review type of task-030 (now ") || !strings.Contains(v, "(current)") || !strings.Contains(v, "enter select") {
		t.Errorf("picker:\n%s", v)
	}
	m = press(t, m, "enter")
	if m.ui.Modal == nil || m.ui.Modal.Step != StepPick || !strings.Contains(m.ui.Modal.Error, "already") {
		t.Errorf("current type accepted: %+v", m.ui.Modal)
	}
	m = press(t, m, "j", "enter", "esc")
	if m.ui.Modal != nil || len(f.calls) != 0 {
		t.Errorf("esc did not cancel: %+v %v", m.ui.Modal, f.calls)
	}
}

func TestSnapshotApprovalColumn(t *testing.T) {
	t.Parallel()
	app := newRepo(t, agentConfig)
	setType := func(id string, r internal.ReviewType, by string) {
		t.Helper()
		if err := app.DB.SetReviewType(id, r, by); err != nil {
			t.Fatal(err)
		}
	}
	request := func(taskID, epicID string) {
		t.Helper()
		if _, err := app.DB.OpenReviewRequest(taskID, epicID, "abc12345ff"); err != nil {
			t.Fatal(err)
		}
	}
	notStarted := seedTask(t, app, "not started", "", internal.StateInProgress)
	reviewing := seedTask(t, app, "reviewing", "", internal.StateReviewRequested)
	request(reviewing.ID, "")
	awaiting := seedTask(t, app, "awaiting", "", internal.StateReviewRequested)
	setType(awaiting.ID, internal.ReviewHuman, "creator")
	request(awaiting.ID, "")
	humanApproved := seedTask(t, app, "human approved", "", internal.StateApproved)
	setType(humanApproved.ID, internal.ReviewHuman, "creator")
	defaultApproved := seedTask(t, app, "default approved", "", internal.StateQueued)
	changes := seedTask(t, app, "changes", "", internal.StateRequestChanges)
	pre := seedTask(t, app, "pre", "", internal.StateChecksPassed)
	setType(pre.ID, internal.ReviewNone, "pre-approved by terminal:alice (dashboard): docs")
	selfDeclared, err := app.DB.CreateTask("self", "", "self", "/nonexistent/self", "main", "", "",
		internal.ReviewSetting{Type: internal.ReviewNone, SetBy: internal.ReviewSetBySelfDeclared})
	if err != nil {
		t.Fatal(err)
	}
	// A none task with a leftover open request still never shows for review.
	request(selfDeclared.ID, "")
	proj, _ := app.DB.CreateProject("P", "")
	epic, _ := app.DB.CreateEpic("E", "", proj.ID, "epic/e", internal.ReviewSetting{Type: internal.ReviewHuman, SetBy: "creator"})
	seedTask(t, app, "in epic", epic.ID, internal.StateInProgress)
	request("", epic.ID)

	s := build(t, app, time.Now(), false)
	for id, want := range map[string]string{
		notStarted.ID:      "default · not started yet",
		reviewing.ID:       "default · reviewing",
		awaiting.ID:        "human · awaiting",
		humanApproved.ID:   "human · approved",
		defaultApproved.ID: "default · approved",
		changes.ID:         "default · changes requested",
		pre.ID:             "none · pre-approved by alice",
		selfDeclared.ID:    "none · self-declared",
	} {
		if got := findRow(t, s.Groups, id).Approval; got != want {
			t.Errorf("%s approval = %q, want %q", id, got, want)
		}
	}
	for _, g := range s.Groups {
		if g.Epic != nil && g.Approval != "human · awaiting" {
			t.Errorf("epic approval = %q", g.Approval)
		}
	}

	reviewers := map[string]string{}
	for _, r := range s.Reviews {
		reviewers[r.SubjectID] = r.Reviewer
	}
	if _, ok := reviewers[selfDeclared.ID]; ok {
		t.Errorf("none task listed under AWAITING REVIEW: %+v", s.Reviews)
	}
	// agentConfig makes default subjects agent-reviewed; human-typed ones
	// still wait for a human.
	if reviewers[awaiting.ID] != ReviewerHuman || reviewers[epic.ID] != ReviewerHuman || reviewers[reviewing.ID] != ReviewerPending {
		t.Errorf("per-subject reviewer status = %v", reviewers)
	}
	if got := findRow(t, s.Groups, awaiting.ID).Attention; got != "awaiting human review" {
		t.Errorf("human task attention = %q", got)
	}
}

func approvalSnapshot() Snapshot {
	s := inReviewSnapshot()
	s.Groups[0].Approval = "default · awaiting"
	tasks := s.Groups[0].Tasks
	tasks[0].Approval, tasks[1].Approval, tasks[2].Approval = "default · reviewing", "human · approved", "none · pre-approved by alice"
	s.Groups[1].Tasks[0].Approval = "none · self-declared"
	s.Groups[1].Tasks[1].Approval = "default · not started yet"
	return s
}

func TestRenderTypePickerGolden(t *testing.T) {
	for _, step := range []ModalStep{StepPick, StepConfirm} {
		ui := UIState{Selected: rowTask025, Actions: DefaultActions(true), Modal: &Modal{
			Decision: DecisionType, Subject: "task-025", SHA: "abc12345ff", Step: step,
			Current: internal.ReviewDefault, Choice: internal.ReviewHuman, Option: ConfirmNo,
		}}
		out := Render(approvalSnapshot(), ui, 120, 30)
		renderLines(t, out, 30)
		checkGolden(t, fmt.Sprintf("tui_type_picker_%d.golden", step), out+"\n")
	}
}

func TestRenderPreApproveGolden(t *testing.T) {
	ui := UIState{Selected: rowTask025, Actions: DefaultActions(true), Modal: &Modal{
		Decision: DecisionPreApprove, Subject: "task-025", SHA: "abc12345ff", Step: StepComment, Input: "docs only",
	}}
	out := Render(approvalSnapshot(), ui, 120, 30)
	renderLines(t, out, 30)
	checkGolden(t, "tui_pre_approve.golden", out+"\n")
}

func TestReviewTypeActionsRefused(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := startedWith(t, f, Options{Interval: time.Millisecond, DenyReason: "HARNESS_REVIEW_TOKEN is set"})
	m = selectRow(t, m, rowTask030)
	for _, k := range []string{"p", "t"} {
		next := press(t, m, k)
		if next.ui.Modal != nil || !next.ui.StatusErr || !strings.Contains(next.ui.Status, "disabled: HARNESS_REVIEW_TOKEN is set") {
			t.Errorf("%s while not allowed: modal=%v status=%q", k, next.ui.Modal, next.ui.Status)
		}
	}

	m = selectRow(t, started(t, &fakeLoader{snap: sampleSnapshot()}), rowMerged)
	for _, k := range []string{"p", "t"} {
		if next := press(t, m, k); next.ui.Modal != nil || !strings.Contains(next.ui.Status, "not merged or closed") {
			t.Errorf("%s on a merged task: modal=%v status=%q", k, next.ui.Modal, next.ui.Status)
		}
	}

	// A none task already queued is past the gate: its pre-approval stays.
	s := sampleSnapshot()
	s.Groups[0].Tasks[2].ReviewType = internal.ReviewNone
	m = selectRow(t, started(t, &fakeLoader{snap: s}), 5)
	if next := press(t, m, "p"); next.ui.Modal != nil || !strings.Contains(next.ui.Status, "already past the review gate") {
		t.Errorf("p on a queued none task: modal=%v status=%q", next.ui.Modal, next.ui.Status)
	}
	if next := press(t, m, "t"); next.ui.Modal == nil {
		t.Error("t refused on a none task")
	}
	if len(f.calls) != 0 {
		t.Errorf("loader called: %v", f.calls)
	}
}

func TestPreApproveToggle(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := selectRow(t, started(t, f), rowTask030)
	m = press(t, m, "p", "enter", "y")
	if want := []string{`type task-030 none ""`}; !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("pre-approve calls = %v", f.calls)
	}

	// The next snapshot shows the task as none: p now removes the pre-approval.
	f.snap = sampleSnapshot()
	f.snap.Groups[1].Tasks[0].ReviewType = internal.ReviewNone
	m = press(t, m, "f")
	if bar := strings.Split(m.View(), "\n"); !strings.Contains(bar[len(bar)-1], "remove pre-approval") {
		t.Errorf("key bar label did not switch: %q", bar[len(bar)-1])
	}
	m = press(t, m, "p")
	if m.ui.Modal == nil || m.ui.Modal.Decision != DecisionRemovePreApproval {
		t.Fatalf("modal = %+v status=%q", m.ui.Modal, m.ui.Status)
	}
	if v := m.View(); !strings.Contains(v, "Remove the pre-approval of task-030") || !strings.Contains(v, "goes back to IN_PROGRESS") {
		t.Errorf("remove modal:\n%s", v)
	}
	m = typeText(t, m, "wants review")
	m = press(t, m, "enter", "a")
	if want := `unpreapprove task-030 "wants review"`; f.calls[len(f.calls)-1] != want {
		t.Errorf("calls = %v, want last %s", f.calls, want)
	}
	if !strings.Contains(strings.Split(m.View(), "\n")[0], "auto-confirm: remove pre-approval") {
		t.Errorf("header lacks the remove kind: %q", strings.Split(m.View(), "\n")[0])
	}
	// Its own kind: pre-approving still asks.
	f.snap = sampleSnapshot()
	m = press(t, m, "f", "p", "enter")
	if m.ui.Modal == nil || m.ui.Modal.Step != StepConfirm {
		t.Errorf("pre-approve skipped its confirmation: %+v", m.ui.Modal)
	}
}

func TestRemovePreApprovalKeyBar(t *testing.T) {
	st := NewStyles(true)
	s := sampleSnapshot()
	s.Groups[1].Tasks[0].ReviewType = internal.ReviewNone
	row := Rows(s)[rowTask030]
	for _, allowed := range []bool{true, false} {
		bar := keyBar(st, DefaultActions(allowed), Context{Row: &row})
		want := disabledKey(st, "remove pre-approval", "p")
		if allowed {
			want = enabledKey(st, "remove pre-approval", "p")
		}
		if !strings.Contains(bar, want) {
			t.Errorf("allowed=%v: bar = %q", allowed, bar)
		}
	}
	if plain := keyBar(NewStyles(false), DefaultActions(true), Context{Row: &row}); !strings.Contains(plain, "  remove pre-approval  type  ") {
		t.Errorf("plain bar = %q", plain)
	}
}

func TestRenderRemovePreApprovalGolden(t *testing.T) {
	s := sampleSnapshot()
	s.Groups[1].Tasks[0].ReviewType = internal.ReviewNone
	ui := UIState{Selected: rowTask030, Actions: DefaultActions(true), Modal: &Modal{
		Decision: DecisionRemovePreApproval, Subject: "task-030", Step: StepConfirm, Comment: "wants review", Option: ConfirmNo,
	}}
	out := Render(s, ui, 120, 30)
	renderLines(t, out, 30)
	checkGolden(t, "tui_remove_pre_approval.golden", out+"\n")
}
