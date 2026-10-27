package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marqueewinq/harness/internal"
)

// historyDetail is a task detail whose history has n transitions; every
// third one carries a note.
func historyDetail(n int) *Detail {
	var b doc
	b.add("State: MERGE_FAILED for 1h · branch sync-epic · base epic/planning")
	start := time.Date(2026, 10, 26, 9, 0, 0, 0, time.Local)
	states := []string{"CHECKS_RUNNING", "CHECKS_FAILED", "IN_PROGRESS"}
	var entries []historyEntry
	from := ""
	for i := 0; i < n; i++ {
		to := states[i%len(states)]
		e := historyEntry{at: start.Add(time.Duration(i) * time.Minute), from: from, to: to}
		if i%3 == 1 {
			e.note = fmt.Sprintf("note %d: merge failed: rebase conflict in internal/dashboard/view.go and two more files that need a hand merge", i)
		}
		entries = append(entries, e)
		from = to
	}
	b.history(entries)
	b.add("Review: not requested")
	d := b.detail("task:task-027", "task-027 Sync epic branch")
	return &d
}

func TestHistoryCollapsedAndExpanded(t *testing.T) {
	st := NewStyles(false)
	d := historyDetail(20)
	collapsed, hidden := paneLines(st, d.Lines, d.Spans, 99, false)
	text := strings.Join(collapsed, "\n")
	if hidden != 12 || !strings.Contains(text, "  … 12 earlier — x to expand") {
		t.Errorf("hidden=%d collapsed:\n%s", hidden, text)
	}
	if strings.Contains(text, "09:00:00") || !strings.Contains(text, "09:19:00") || !strings.Contains(text, "09:12:00") {
		t.Errorf("collapsed does not keep the last 8 transitions:\n%s", text)
	}

	expanded, hidden := paneLines(st, d.Lines, d.Spans, 99, true)
	text = strings.Join(expanded, "\n")
	if hidden != 0 || !strings.Contains(text, "(created) → CHECKS_RUNNING") || strings.Contains(text, "earlier") {
		t.Errorf("expanded:\n%s", text)
	}
	if first, last := strings.Index(text, "09:00:00"), strings.Index(text, "09:19:00"); first < 0 || last < first {
		t.Errorf("history not oldest first:\n%s", text)
	}
	for i := 1; i < 20; i += 3 {
		if !strings.Contains(text, fmt.Sprintf("· note %d: merge failed", i)) {
			t.Errorf("note %d missing:\n%s", i, text)
		}
	}
	for _, l := range expanded {
		if w := len([]rune(l)); w > 99 {
			t.Errorf("history line not wrapped (%d): %q", w, l)
		}
	}
	if _, hidden := paneLines(st, historyDetail(8).Lines, historyDetail(8).Spans, 99, false); hidden != 0 {
		t.Errorf("8 transitions reported hidden lines: %d", hidden)
	}
}

func TestExpandEnabledOnlyWhenHistoryTruncated(t *testing.T) {
	s := sampleSnapshot()
	for n, want := range map[int]bool{8: false, 9: true, 20: true} {
		ui := UIState{Selected: rowTask027, DetailOpen: true, Detail: historyDetail(n)}
		if got := Expandable(s, ui, 100); got != want {
			t.Errorf("%d transitions: expandable=%v, want %v", n, got, want)
		}
	}
}

// rowTask027 is task-027 in sampleSnapshot's rows.
const rowTask027 = 4

func TestHistoryGolden(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		ui := UIState{Selected: rowTask027, DetailOpen: true, Detail: historyDetail(12), Expanded: expanded, Actions: DefaultActions(true)}
		out := Render(sampleSnapshot(), ui, 100, 44)
		renderLines(t, out, 44)
		checkGolden(t, fmt.Sprintf("tui_history_expanded_%v.golden", expanded), out+"\n")
	}
}

// The real detail loader builds the history block for tasks and epics.
func TestHistoryFromRealRepo(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	task := seedTask(t, app, "flaky", "", internal.StateChecksFailed)
	for i := 0; i < 6; i++ {
		for _, s := range []internal.TaskState{internal.StateChecksRunning, internal.StateChecksFailed} {
			if err := app.DB.UpdateTaskState(task.ID, s); err != nil {
				t.Fatal(err)
			}
		}
	}
	m := NewModel(AppLoader(app, false), Options{Interval: time.Millisecond, AllowActions: true})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 60})
	m = run(t, m, m.Init())
	for m.selected() != nil && m.selected().ID != task.ID {
		m = press(t, m, "j")
	}
	m = press(t, m, "enter")
	if v := m.View(); !strings.Contains(v, "… 7 earlier — x to expand") || strings.Contains(v, "(created) → IN_PROGRESS") {
		t.Fatalf("collapsed history:\n%s", v)
	}
	m = press(t, m, "x")
	if v := m.View(); !m.ui.Expanded || !strings.Contains(v, "(created) → IN_PROGRESS") || strings.Contains(v, "earlier") {
		t.Errorf("x did not expand the history:\n%s", v)
	}

	proj, _ := app.DB.CreateProject("P", "")
	epic, _ := app.DB.CreateEpic("E", "", proj.ID, "", internal.ReviewSetting{})
	app.DB.UpdateEpicStatus(epic.ID, internal.EpicStatusClosed)
	d, err := BuildDetail(app, Row{Kind: RowEpic, ID: epic.ID, IsEpic: true}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if text := strings.Join(d.Lines, "\n"); !strings.Contains(text, "History:") || !strings.Contains(text, "open → closed") {
		t.Errorf("epic detail lacks history:\n%s", text)
	}
}
