package dashboard

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marqueewinq/harness/internal"
)

type verdictCall struct{ subject, decision, comment, sha string }

type fakeLoader struct {
	snap       Snapshot
	err        error
	snapCalls  int
	details    []string
	diffs      []string
	diffLines  int
	verdicts   []verdictCall
	verdictErr error
	diffErr    error
	expandable bool
	calls      []string
	stopErr    error
	typeErr    error
}

func (f *fakeLoader) loader() Loader {
	return Loader{
		Snapshot: func(time.Time) (Snapshot, error) {
			f.snapCalls++
			return f.snap, f.err
		},
		Detail: func(r Row, _ time.Time) (Detail, error) {
			f.details = append(f.details, r.Key())
			var b doc
			b.add("detail of %s", r.ID)
			if f.expandable {
				b.text("", "Description: ", longText("full text of "+r.ID))
			}
			return b.detail(r.Key(), r.ID), nil
		},
		Diff: func(r Row) (DiffView, error) {
			f.diffs = append(f.diffs, r.Key())
			if f.diffErr != nil {
				return DiffView{}, f.diffErr
			}
			sha := r.RequestSHA()
			var b doc
			b.add("Review of %s at %s", r.ID, sha)
			if f.expandable {
				b.text("  ", "[request_changes at x] ", longText("full comments of "+r.ID))
			}
			v := DiffView{Key: r.Key(), SHA: sha, Range: "base..." + sha, Title: r.ID + " diff", Header: b.lines, HeaderSpans: b.spans}
			for i := 0; i < f.diffLines; i++ {
				v.Diff = append(v.Diff, fmt.Sprintf("+line %d", i))
			}
			return v, nil
		},
		Verdict: func(subject, decision, comment, sha string) (string, error) {
			f.verdicts = append(f.verdicts, verdictCall{subject, decision, comment, sha})
			if f.verdictErr != nil {
				return "", f.verdictErr
			}
			return "Review recorded: " + strings.ToUpper(decision) + " (81742677)\nTask " + subject + " is now APPROVED.\n", nil
		},
		Stop: func(subject string, outcome internal.StopOutcome, sha string) (string, error) {
			f.calls = append(f.calls, fmt.Sprintf("stop %s %s %s", subject, outcome, sha))
			if f.stopErr != nil {
				return "", f.stopErr
			}
			return "Review stopped: " + string(outcome) + "\n", nil
		},
		EditFields: func(subject string) (EditFields, error) {
			return EditFields{Title: "Title of " + subject, Description: "desc\n", Notes: ""}, nil
		},
		Edit: func(subject string, e EditFields) (string, error) {
			f.calls = append(f.calls, fmt.Sprintf("edit %s %q %q %q", subject, e.Title, e.Description, e.Notes))
			return "Edited " + subject + ".\n", nil
		},
		RemovePreApproval: func(subject, reason string) (string, error) {
			f.calls = append(f.calls, fmt.Sprintf("unpreapprove %s %q", subject, reason))
			return "Pre-approval of " + subject + " removed.\n", nil
		},
		SetType: func(subject string, r internal.ReviewType, reason string) (string, error) {
			f.calls = append(f.calls, fmt.Sprintf("type %s %s %q", subject, r, reason))
			if f.typeErr != nil {
				return "", f.typeErr
			}
			return "Review type of " + subject + " is now " + string(r) + ".\n", nil
		},
	}
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// longText has more lines than a collapsed pane shows, ending with last.
func longText(last string) string {
	return "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\n" + last
}

// cmdTimeout bounds one command: a tea.Tick sleeps its whole interval, so a
// model built with a long --interval would otherwise block run.
const cmdTimeout = 10 * time.Second

func execCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(cmdTimeout):
		t.Fatalf("command still running after %s (a tea.Tick with a long interval? use Options{Interval: time.Millisecond})", cmdTimeout)
		return nil
	}
}

// run executes cmd and feeds every message it yields back into the model,
// except ticks, which would recurse.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := execCmd(t, cmd).(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(t, m, c)
		}
	case tickMsg, nil:
	default:
		var next tea.Cmd
		m, next = step(t, m, msg)
		m = run(t, m, next)
	}
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func started(t *testing.T, f *fakeLoader) Model {
	t.Helper()
	return startedWith(t, f, Options{Interval: time.Millisecond, AllowActions: true})
}

func startedWith(t *testing.T, f *fakeLoader, opts Options) Model {
	t.Helper()
	m := NewModel(f.loader(), opts)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = run(t, m, m.Init())
	if f.snapCalls != 1 || m.loading {
		t.Fatalf("after init: snapCalls=%d loading=%v", f.snapCalls, m.loading)
	}
	return m
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = step(t, m, key(k))
		m = run(t, m, cmd)
	}
	return m
}

func TestModelSelectionBounds(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	n := len(Rows(f.snap))
	if m.ui.Selected != 0 {
		t.Fatalf("initial selection = %d", m.ui.Selected)
	}
	m = press(t, m, "up", "k")
	if m.ui.Selected != 0 {
		t.Errorf("selection above top = %d", m.ui.Selected)
	}
	m = press(t, m, "down", "j")
	if m.ui.Selected != 2 {
		t.Errorf("after down,j = %d, want 2", m.ui.Selected)
	}
	for i := 0; i < n+3; i++ {
		m = press(t, m, "j")
	}
	if m.ui.Selected != n-1 {
		t.Errorf("selection past bottom = %d, want %d", m.ui.Selected, n-1)
	}
	if !strings.Contains(m.View(), "›  task-024") {
		t.Errorf("view lacks cursor on last row:\n%s", m.View())
	}
}

func TestModelSelectionFollowsRowAcrossSnapshots(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "j", "j", "j")
	want := m.rows[m.ui.Selected].Key()
	f.snap.Reviews = f.snap.Reviews[1:]
	m = press(t, m, "f")
	if got := m.rows[m.ui.Selected].Key(); got != want {
		t.Errorf("selection moved to %s, want %s", got, want)
	}
	f.snap = Snapshot{Repo: "harness", Now: fixedNow}
	m = press(t, m, "f")
	if m.ui.Selected != 0 || m.selected() != nil {
		t.Errorf("empty snapshot selection = %d", m.ui.Selected)
	}
	m = press(t, m, "j", "enter")
	if !strings.Contains(m.View(), "nothing selected") {
		t.Errorf("empty detail:\n%s", m.View())
	}
}

func TestModelDetailToggle(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "j", "enter")
	if !m.ui.DetailOpen || m.ui.Detail == nil || m.ui.Detail.Key != "review:epic-002" {
		t.Fatalf("detail = %v %+v", m.ui.DetailOpen, m.ui.Detail)
	}
	if !strings.Contains(m.View(), "detail of epic-002") {
		t.Errorf("view lacks detail:\n%s", m.View())
	}
	m = press(t, m, "j", "j")
	if m.ui.Detail.Key != "task:task-025" {
		t.Errorf("detail did not follow selection: %+v", m.ui.Detail)
	}
	m = press(t, m, "tab")
	if m.ui.DetailOpen || strings.Contains(m.View(), "detail of") {
		t.Errorf("tab did not close detail:\n%s", m.View())
	}
	m = press(t, m, "j")
	if got := f.details[len(f.details)-1]; got != "task:task-025" {
		t.Errorf("detail loaded while closed: %v", f.details)
	}
}

func TestEpicHeaderSelectable(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "j", "j", "enter")
	if r := m.selected(); r == nil || r.Key() != "epic:epic-002" || !r.IsEpic {
		t.Fatalf("selected = %+v", r)
	}
	if !strings.Contains(m.View(), "›proj-001 › epic-002") || m.ui.Detail == nil || m.ui.Detail.Key != "epic:epic-002" {
		t.Errorf("epic header not selected with detail:\n%s", m.View())
	}
}

func TestModelRefreshAndTick(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m, cmd := step(t, m, key("f"))
	if cmd == nil || !m.loading || !m.ui.Refreshing {
		t.Fatalf("f did not start a refresh")
	}
	if _, again := step(t, m, key("f")); again != nil {
		t.Error("second refresh started while one is loading")
	}
	m = run(t, m, cmd)
	if f.snapCalls != 2 || m.loading {
		t.Errorf("snapCalls=%d loading=%v", f.snapCalls, m.loading)
	}

	m, cmd = step(t, m, tickMsg(time.Now()))
	if cmd == nil || !m.loading {
		t.Fatal("tick did not start a refresh")
	}
	m = run(t, m, cmd)
	if f.snapCalls != 3 {
		t.Errorf("snapCalls after tick = %d", f.snapCalls)
	}
}

func TestModelStaleSnapshotKeepsLastGood(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	f.err = errors.New("database is locked")
	m = press(t, m, "f")
	view := m.View()
	if !strings.Contains(view, "stale: database is locked") || !strings.Contains(view, "task-025") {
		t.Errorf("stale view:\n%s", view)
	}
	f.err = nil
	m = press(t, m, "f")
	if strings.Contains(m.View(), "stale:") {
		t.Errorf("stale header survived a good snapshot:\n%s", m.View())
	}
}

func TestModelFirstSnapshotFails(t *testing.T) {
	f := &fakeLoader{err: errors.New("no such table")}
	m := NewModel(f.loader(), Options{Interval: time.Millisecond, AllowActions: true})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = run(t, m, m.Init())
	if first := strings.Split(m.View(), "\n")[0]; !strings.Contains(first, "stale: no such table") {
		t.Errorf("header = %q", first)
	}
}

func TestModelQuit(t *testing.T) {
	m := started(t, &fakeLoader{snap: sampleSnapshot()})
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := step(t, m, key(k))
		if cmd == nil {
			t.Fatalf("%s: no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestModelActionsDisabledWhenNotAllowed(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := startedWith(t, f, Options{Interval: time.Millisecond, DenyReason: "HARNESS_REVIEW_TOKEN is set"})
	for _, k := range []string{"a", "r"} {
		next, cmd := step(t, m, key(k))
		if cmd != nil || next.ui.Modal != nil || next.ui.DiffOpen {
			t.Errorf("%s opened something while actions are not allowed", k)
		}
		if !next.ui.StatusErr || !strings.Contains(next.ui.Status, "disabled: HARNESS_REVIEW_TOKEN is set") {
			t.Errorf("%s status = %q", k, next.ui.Status)
		}
		if !strings.Contains(next.View(), "HARNESS_REVIEW_TOKEN is set") {
			t.Errorf("status line not rendered:\n%s", next.View())
		}
	}
	if len(f.verdicts) != 0 {
		t.Errorf("verdict recorded: %v", f.verdicts)
	}
	after := press(t, m, "a", "j")
	if after.ui.Status != "" {
		t.Errorf("status survived the next key: %q", after.ui.Status)
	}
	viewing := press(t, m, "d")
	if !viewing.ui.DiffOpen || len(f.diffs) != 1 || viewing.ui.StatusErr {
		t.Errorf("diff not available when actions are disallowed: open=%v loads=%v status=%q", viewing.ui.DiffOpen, f.diffs, viewing.ui.Status)
	}
}

func TestModelVerdictNeedsOpenRequest(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "j", "j", "j", "a")
	if m.ui.Modal != nil || !strings.Contains(m.ui.Status, "open review request") {
		t.Errorf("approve on a task without a request: modal=%v status=%q", m.ui.Modal, m.ui.Status)
	}
	m = press(t, m, "d")
	if !m.ui.DiffOpen || f.diffs[len(f.diffs)-1] != "task:task-025" {
		t.Errorf("diff on an in-progress task row: open=%v loads=%v", m.ui.DiffOpen, f.diffs)
	}
	m = press(t, m, "esc", "k", "a")
	if m.ui.Modal == nil || m.ui.Modal.Subject != "epic-002" || m.ui.Modal.SHA != "5d1c09aa999" {
		t.Errorf("approve on epic header with an open request: %+v", m.ui.Modal)
	}
}

func TestExpandToggle(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "x")
	if m.ui.Expanded || !strings.Contains(m.ui.Status, "expand applies") {
		t.Errorf("x without an open pane: expanded=%v status=%q", m.ui.Expanded, m.ui.Status)
	}
	f.expandable = true
	m = press(t, m, "enter", "x")
	if !m.ui.Expanded || !strings.Contains(m.View(), "full text of task-026") {
		t.Errorf("x did not expand:\n%s", m.View())
	}
	m = press(t, m, "x")
	if m.ui.Expanded {
		t.Error("second x did not collapse")
	}
	m = press(t, m, "x", "j")
	if m.ui.Expanded {
		t.Error("selection change did not collapse")
	}
	m = press(t, m, "k", "d", "x")
	if !m.ui.Expanded || !strings.Contains(m.View(), "full comments of task-026") {
		t.Errorf("x in diff pane did not expand the header:\n%s", m.View())
	}
}

func TestModelWindowSizeAndTooSmall(t *testing.T) {
	m := started(t, &fakeLoader{snap: sampleSnapshot()})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 40, Height: 10})
	if !strings.Contains(m.View(), "terminal too small") {
		t.Errorf("view = %q", m.View())
	}
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 12})
	m = press(t, m, "G")
	if !strings.Contains(m.View(), "›  task-024") {
		t.Errorf("end key did not scroll to last row:\n%s", m.View())
	}
	if m.ui.Offset == 0 {
		t.Error("offset not stored after scrolling")
	}
}
