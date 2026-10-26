package dashboard

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	next, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return run(t, next, cmd)
}

func TestApproveFlow(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "a")
	if m.ui.Modal == nil || m.ui.Modal.Decision != "approve" || m.ui.Modal.Subject != "task-026" || m.ui.Modal.Step != StepComment {
		t.Fatalf("modal = %+v", m.ui.Modal)
	}
	if v := m.View(); !strings.Contains(v, "Approve task-026 at 81742677?") || !strings.Contains(v, "enter confirm") {
		t.Errorf("approve modal not rendered:\n%s", v)
	}
	m = typeText(t, m, "looks good")
	m = press(t, m, "enter")
	if m.ui.Modal.Step != StepConfirm || m.ui.Modal.Comment != "looks good" {
		t.Fatalf("after comment: %+v", m.ui.Modal)
	}
	m = typeText(t, m, "task-062")
	m = press(t, m, "enter")
	if m.ui.Modal == nil || !strings.Contains(m.ui.Modal.Error, "type task-026 exactly") || len(f.verdicts) != 0 {
		t.Fatalf("wrong ID accepted: modal=%+v verdicts=%v", m.ui.Modal, f.verdicts)
	}
	if !strings.Contains(m.View(), "type task-026 exactly") {
		t.Errorf("inline error not rendered:\n%s", m.View())
	}
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	m = typeText(t, m, "task-026")
	calls := f.snapCalls
	m = press(t, m, "enter")
	want := []verdictCall{{"task-026", "approve", "looks good", "8174267712345"}}
	if len(f.verdicts) != 1 || f.verdicts[0] != want[0] {
		t.Fatalf("verdicts = %+v, want %+v", f.verdicts, want)
	}
	if m.ui.Modal != nil || m.ui.StatusErr || !strings.Contains(m.ui.Status, "Review recorded: APPROVE") {
		t.Errorf("after verdict: modal=%v status=%q err=%v", m.ui.Modal, m.ui.Status, m.ui.StatusErr)
	}
	if f.snapCalls != calls+1 {
		t.Errorf("verdict did not refresh the snapshot")
	}
}

func TestRejectRequiresComment(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m = press(t, m, "j", "r")
	if m.ui.Modal == nil || m.ui.Modal.Decision != "request_changes" || m.ui.Modal.Subject != "epic-002" {
		t.Fatalf("modal = %+v", m.ui.Modal)
	}
	m = press(t, m, "enter")
	if m.ui.Modal.Step != StepComment || !strings.Contains(m.ui.Modal.Error, "comment is required") {
		t.Fatalf("empty reject comment accepted: %+v", m.ui.Modal)
	}
	m = typeText(t, m, "split the migration")
	m = press(t, m, "enter")
	m = typeText(t, m, "epic-002")
	m = press(t, m, "enter")
	if len(f.verdicts) != 1 || f.verdicts[0] != (verdictCall{"epic-002", "request_changes", "split the migration", "5d1c09aa999"}) {
		t.Errorf("verdicts = %+v", f.verdicts)
	}
}

func TestEscCancelsAtEachStep(t *testing.T) {
	for _, keys := range [][]string{{"a"}, {"a", "enter"}, {"r"}, {"r", "x", "enter"}} {
		f := &fakeLoader{snap: sampleSnapshot()}
		m := started(t, f)
		for _, k := range keys {
			if k == "x" {
				m = typeText(t, m, "some comment")
				continue
			}
			m = press(t, m, k)
		}
		if m.ui.Modal == nil {
			t.Fatalf("%v: no modal before esc", keys)
		}
		m = press(t, m, "esc")
		if m.ui.Modal != nil || len(f.verdicts) != 0 || m.ui.Status != "cancelled" {
			t.Errorf("%v: esc left modal=%+v verdicts=%v status=%q", keys, m.ui.Modal, f.verdicts, m.ui.Status)
		}
		if strings.Contains(m.View(), "to confirm") {
			t.Errorf("%v: modal still drawn", keys)
		}
	}
}

func TestVerdictErrorShown(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot(), verdictErr: errors.New("task-026 moved since review was requested (81742677 -> 0badc0de)")}
	m := started(t, f)
	m = press(t, m, "a", "enter")
	m = typeText(t, m, "task-026")
	m = press(t, m, "enter")
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "moved since review was requested") {
		t.Errorf("status = %q err=%v", m.ui.Status, m.ui.StatusErr)
	}
	if !strings.Contains(m.View(), "moved since review was requested") {
		t.Errorf("error not rendered:\n%s", m.View())
	}
}

func TestQuitInsideModal(t *testing.T) {
	m := started(t, &fakeLoader{snap: sampleSnapshot()})
	m = press(t, m, "a")
	_, cmd := step(t, m, key("ctrl+c"))
	if cmd == nil {
		t.Fatal("ctrl+c in modal returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("ctrl+c in modal did not quit")
	}
	m = typeText(t, m, "q")
	if m.ui.Modal == nil || !strings.Contains(m.ui.Modal.Input, "q") {
		t.Errorf("q in modal was not typed: %+v", m.ui.Modal)
	}
}

func TestDiffPane(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot(), diffLines: 100}
	m := started(t, f)
	m = press(t, m, "d")
	if !m.ui.DiffOpen || len(f.diffs) != 1 || f.diffs[0] != "review:task-026" {
		t.Fatalf("diff open=%v loads=%v", m.ui.DiffOpen, f.diffs)
	}
	v := m.View()
	if !strings.Contains(v, "task-026 diff") || !strings.Contains(v, "+line 0") || !strings.Contains(v, "esc close") {
		t.Errorf("diff pane:\n%s", v)
	}
	m = press(t, m, "j", "j")
	if m.ui.DiffOffset != 2 {
		t.Errorf("offset after j,j = %d", m.ui.DiffOffset)
	}
	m = press(t, m, "pgdown")
	page := DiffPage(m.snap, m.ui, m.height)
	if m.ui.DiffOffset != 2+page {
		t.Errorf("offset after pgdown = %d, want %d", m.ui.DiffOffset, 2+page)
	}
	m = press(t, m, "G")
	last := ClampDiffOffset(m.snap, m.ui, m.height)
	if m.ui.DiffOffset != last || !strings.Contains(m.View(), "+line 99") {
		t.Errorf("G: offset=%d last=%d\n%s", m.ui.DiffOffset, last, m.View())
	}
	m = press(t, m, "pgup", "k", "g")
	if m.ui.DiffOffset != 0 {
		t.Errorf("g: offset=%d", m.ui.DiffOffset)
	}
	sel := m.ui.Selected
	m = press(t, m, "esc")
	if m.ui.DiffOpen || m.ui.Selected != sel {
		t.Errorf("esc: open=%v selected=%d", m.ui.DiffOpen, m.ui.Selected)
	}
	m = press(t, m, "d")
	if len(f.diffs) != 1 {
		t.Errorf("reopening the same diff reloaded it: %v", f.diffs)
	}
	m = press(t, m, "a")
	if m.ui.Modal == nil || m.ui.Modal.Subject != "task-026" {
		t.Errorf("approve from diff pane: %+v", m.ui.Modal)
	}
}

// The snapshot moves on to a new request while the human reads the old
// diff: the verdict must carry the SHA of the diff on screen, and reopening
// the diff must load the new commit.
func TestVerdictPinnedToShownDiff(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot(), diffLines: 3}
	m := started(t, f)
	m = press(t, m, "d")
	f.snap = sampleSnapshot()
	f.snap.Reviews[0].CommitSHA = "feedface0001"
	m = press(t, m, "f")
	if got := m.rows[0].Review.CommitSHA; got != "feedface0001" {
		t.Fatalf("snapshot not refreshed: %s", got)
	}
	m = press(t, m, "a")
	if m.ui.Modal == nil || m.ui.Modal.SHA != "8174267712345" {
		t.Fatalf("modal SHA = %+v, want the shown diff's 8174267712345", m.ui.Modal)
	}
	m = press(t, m, "enter")
	m = typeText(t, m, "task-026")
	m = press(t, m, "enter")
	if len(f.verdicts) != 1 || f.verdicts[0].sha != "8174267712345" {
		t.Errorf("verdicts = %+v, want SHA of the shown diff", f.verdicts)
	}
	m = press(t, m, "a")
	if m.ui.Modal == nil || m.ui.Modal.SHA != "feedface0001" {
		t.Errorf("modal without open diff: %+v, want the row's request SHA", m.ui.Modal)
	}
}

func TestDiffCacheKeyedByRequestSHA(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot(), diffLines: 3}
	m := started(t, f)
	m = press(t, m, "d", "esc", "f", "d")
	if len(f.diffs) != 1 {
		t.Errorf("unchanged request reloaded: %v", f.diffs)
	}
	m = press(t, m, "esc")
	f.snap = sampleSnapshot()
	f.snap.Reviews[0].CommitSHA = "feedface0001"
	m = press(t, m, "f", "d")
	if len(f.diffs) != 2 || m.ui.Diff == nil || m.ui.Diff.SHA != "feedface0001" {
		t.Errorf("stale diff reused: loads=%v diff=%+v", f.diffs, m.ui.Diff)
	}
	if !strings.Contains(m.View(), "at feedface0001") {
		t.Errorf("view shows the old diff:\n%s", m.View())
	}
}

func TestFailedDiffNotCached(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot(), diffErr: errors.New("git diff: bad object")}
	m := started(t, f)
	m = press(t, m, "d")
	if !strings.Contains(m.View(), "bad object") {
		t.Errorf("diff error not shown:\n%s", m.View())
	}
	m = press(t, m, "a")
	if m.ui.Modal == nil || m.ui.Modal.SHA != "8174267712345" {
		t.Errorf("modal after failed diff: %+v", m.ui.Modal)
	}
	m = press(t, m, "esc", "esc")
	f.diffErr = nil
	m = press(t, m, "d")
	if len(f.diffs) != 2 || m.ui.Diff == nil || m.ui.Diff.Failed {
		t.Errorf("failed diff not retried: loads=%v diff=%+v", f.diffs, m.ui.Diff)
	}
}

func TestDetailLoadsDoNotOverlap(t *testing.T) {
	f := &fakeLoader{snap: sampleSnapshot()}
	m := started(t, f)
	m, cmd := step(t, m, key("enter"))
	if cmd == nil || !m.detailLoading {
		t.Fatal("enter did not start a detail load")
	}
	m, again := step(t, m, key("j"))
	if again != nil {
		t.Fatal("second detail load started while one is running")
	}
	m = run(t, m, cmd)
	if len(f.details) != 2 || f.details[1] != "review:epic-002" {
		t.Errorf("queued detail load not served: %v", f.details)
	}
	if m.ui.Detail == nil || m.ui.Detail.Key != "review:epic-002" {
		t.Errorf("detail = %+v", m.ui.Detail)
	}
}
