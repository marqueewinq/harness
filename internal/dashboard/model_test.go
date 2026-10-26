package dashboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeLoader struct {
	snap      Snapshot
	err       error
	snapCalls int
	details   []string
}

func (f *fakeLoader) loader() Loader {
	return Loader{
		Snapshot: func(time.Time) (Snapshot, error) {
			f.snapCalls++
			return f.snap, f.err
		},
		Detail: func(r Row, _ time.Time) (Detail, error) {
			f.details = append(f.details, r.Key())
			return Detail{Key: r.Key(), Title: r.ID, Lines: []string{"detail of " + r.ID}}, nil
		},
	}
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// run executes cmd and feeds every message it yields back into the model,
// except ticks, which would recurse.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
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
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func started(t *testing.T, f *fakeLoader) Model {
	t.Helper()
	m := NewModel(f.loader(), time.Millisecond, false)
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
	m = press(t, m, "j")
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
	m := NewModel(f.loader(), time.Millisecond, false)
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

func TestModelDisabledActionsDoNothing(t *testing.T) {
	m := started(t, &fakeLoader{snap: sampleSnapshot()})
	for _, k := range []string{"a", "r", "d"} {
		next, cmd := step(t, m, key(k))
		if cmd != nil || next.ui.DetailOpen != m.ui.DetailOpen || next.ui.Selected != m.ui.Selected {
			t.Errorf("%s changed state or ran a command", k)
		}
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
