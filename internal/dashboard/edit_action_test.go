package dashboard

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeEditor plays the human in the editor: it runs edit on the file's text
// instead of starting a process, and remembers the file it was given.
type fakeEditor struct {
	edit    func(string) string
	fail    error
	path    string
	content string
}

func (e *fakeEditor) options(allowed bool) Options {
	return Options{
		Interval:     time.Millisecond,
		AllowActions: allowed,
		DenyReason:   "HARNESS_REVIEW_TOKEN is set",
		EditorCommand: func(path string) (*exec.Cmd, error) {
			return exec.Command("fake-editor", path), nil
		},
		ExecProcess: func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
			return func() tea.Msg {
				e.path = c.Args[len(c.Args)-1]
				data, err := os.ReadFile(e.path)
				if err != nil {
					return cb(err)
				}
				e.content = string(data)
				if e.fail != nil {
					return cb(e.fail)
				}
				return cb(os.WriteFile(e.path, []byte(e.edit(string(data))), 0600))
			}
		},
	}
}

func replaceLine(old, new string) func(string) string {
	return func(s string) string { return strings.Replace(s, old, new, 1) }
}

func editModel(t *testing.T, ed *fakeEditor, f *fakeLoader, row int) Model {
	t.Helper()
	m := startedWith(t, f, ed.options(true))
	return selectRow(t, m, row)
}

func TestEditAppliesAfterConfirm(t *testing.T) {
	ed := &fakeEditor{edit: func(s string) string {
		s = replaceLine("Title: Title of task-030", "Title: Better title")(s)
		return s + "a note\nsecond note line\n"
	}}
	f := &fakeLoader{snap: sampleSnapshot()}
	m := editModel(t, ed, f, rowTask030)
	m = press(t, m, "e")
	if !strings.Contains(ed.content, "Title: Title of task-030\n--- description ---\ndesc\n--- notes ---\n") {
		t.Errorf("editor got:\n%s", ed.content)
	}
	if m.ui.Modal == nil || m.ui.Modal.Decision != DecisionEdit || m.ui.Modal.Step != StepConfirm {
		t.Fatalf("no confirm after editing: %+v status=%q", m.ui.Modal, m.ui.Status)
	}
	if v := m.View(); !strings.Contains(v, "Save the edit of task-030?") || !strings.Contains(v, "Changes: title, notes") {
		t.Errorf("edit confirm:\n%s", v)
	}
	m = press(t, m, "y")
	// The unchanged description keeps its stored text, trailing newline included.
	want := []string{`edit task-030 "Better title" "desc\n" "a note\nsecond note line"`}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %v, want %v", f.calls, want)
	}
	if m.ui.StatusErr || !strings.HasPrefix(m.ui.Status, "Edited task-030") {
		t.Errorf("status = %q", m.ui.Status)
	}
	if _, err := os.Stat(ed.path); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %s", ed.path)
	}
}

func TestEditNoChangesAndCancels(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(string) string
		status string
	}{
		{"unchanged", func(s string) string { return s }, "no changes to task-030"},
		{"trailing blank lines only", func(s string) string { return s + "\n\n" }, "no changes to task-030"},
		{"empty title", replaceLine("Title: Title of task-030", "Title:"), "title empty: edit of task-030 cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ed := &fakeEditor{edit: tc.edit}
			f := &fakeLoader{snap: sampleSnapshot()}
			m := press(t, editModel(t, ed, f, rowTask030), "e")
			if m.ui.Modal != nil || m.ui.Status != tc.status || len(f.calls) != 0 {
				t.Errorf("modal=%+v status=%q calls=%v", m.ui.Modal, m.ui.Status, f.calls)
			}
			if _, err := os.Stat(ed.path); !os.IsNotExist(err) {
				t.Errorf("temp file left behind")
			}
		})
	}
}

func TestEditParseErrorKeepsFile(t *testing.T) {
	ed := &fakeEditor{edit: replaceLine("--- notes ---\n", "")}
	f := &fakeLoader{snap: sampleSnapshot()}
	m := press(t, editModel(t, ed, f, rowTask030), "e")
	defer os.Remove(ed.path)
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "not applied") || !strings.Contains(m.ui.Status, ed.path) {
		t.Errorf("status = %q", m.ui.Status)
	}
	if _, err := os.Stat(ed.path); err != nil {
		t.Errorf("edited text lost: %v", err)
	}
}

func TestEditorFailure(t *testing.T) {
	ed := &fakeEditor{fail: errors.New("exit status 1")}
	f := &fakeLoader{snap: sampleSnapshot()}
	m := press(t, editModel(t, ed, f, rowTask030), "e")
	defer os.Remove(ed.path)
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "editor failed") || len(f.calls) != 0 {
		t.Errorf("status = %q calls=%v", m.ui.Status, f.calls)
	}

	noEditor := ed.options(true)
	noEditor.EditorCommand = func(string) (*exec.Cmd, error) { return nil, errors.New("no editor found: set VISUAL or EDITOR") }
	m = selectRow(t, startedWith(t, &fakeLoader{snap: sampleSnapshot()}, noEditor), rowTask030)
	m = press(t, m, "e")
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "no editor found") {
		t.Errorf("status without editor = %q", m.ui.Status)
	}
}

func TestEditRowsAndPermission(t *testing.T) {
	ed := &fakeEditor{edit: replaceLine("Title: Title of task-024", "Title: Merged, renamed")}
	f := &fakeLoader{snap: sampleSnapshot()}
	m := press(t, editModel(t, ed, f, rowMerged), "e", "a")
	if want := `edit task-024 "Merged, renamed" "desc\n" ""`; len(f.calls) != 1 || f.calls[0] != want {
		t.Errorf("merged task edit calls = %v", f.calls)
	}
	// "don't ask again" applies to later edits.
	ed.edit = replaceLine("Title: Title of epic-002", "Title: Epic renamed")
	m = selectRowUp(t, m, rowEpicHeader)
	m = press(t, m, "e")
	if m.ui.Modal != nil || len(f.calls) != 2 || !strings.Contains(m.ui.Status, "(confirmation skipped for edit this session)") {
		t.Errorf("second edit: modal=%+v calls=%v status=%q", m.ui.Modal, f.calls, m.ui.Status)
	}

	m = selectRow(t, startedWith(t, &fakeLoader{snap: sampleSnapshot()}, ed.options(false)), rowTask030)
	m = press(t, m, "e")
	if m.ui.Modal != nil || !strings.Contains(m.ui.Status, "edit is disabled: HARNESS_REVIEW_TOKEN is set") {
		t.Errorf("edit while not allowed: %q", m.ui.Status)
	}
	m = press(t, startedWith(t, &fakeLoader{snap: sampleSnapshot()}, ed.options(true)), "e")
	if !strings.Contains(m.ui.Status, "applies to a task row or epic header") {
		t.Errorf("edit on a review row: %q", m.ui.Status)
	}
}

func TestRenderEditConfirmGolden(t *testing.T) {
	ui := UIState{Selected: rowTask030, Actions: DefaultActions(true), Modal: &Modal{
		Decision: DecisionEdit, Subject: "task-030", Step: StepConfirm, Option: ConfirmNo, Changed: "title, notes",
		Edit: &EditFields{Title: "Better title"},
	}}
	out := Render(sampleSnapshot(), ui, 100, 26)
	renderLines(t, out, 26)
	checkGolden(t, "tui_edit_confirm.golden", out+"\n")
}

func selectRowUp(t *testing.T, m Model, i int) Model {
	t.Helper()
	for m.ui.Selected > i {
		m = press(t, m, "k")
	}
	return m
}
