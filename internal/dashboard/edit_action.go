package dashboard

import (
	"fmt"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// DecisionEdit is the confirm kind of an edit made in the external editor.
const DecisionEdit = "edit"

type editStartMsg struct {
	subject string
	path    string
	orig    EditFields
	err     error
}

type editDoneMsg struct {
	subject string
	path    string
	orig    EditFields
	warning string
	err     error
}

// DefaultEditorCommand opens path in $VISUAL, $EDITOR, nano or vi; warning
// names a setting that was skipped because it did not parse.
func DefaultEditorCommand(path string) (*exec.Cmd, string, error) {
	argv, warning, err := ResolveEditor(os.Getenv, exec.LookPath)
	if err != nil {
		return nil, warning, err
	}
	return exec.Command(argv[0], append(argv[1:], path)...), warning, nil
}

func isEditable(r *Row) bool {
	return r != nil && (r.Kind == RowTask || r.Kind == RowEpic)
}

// keptText tells the human where an unsaved edit is.
func keptText(path string) string {
	return "nothing saved; your text is in " + path
}

func withWarning(status, warning string) string {
	if warning == "" {
		return status
	}
	return status + " (warning: " + warning + ")"
}

// startEdit loads the subject's fields and writes them to a temp file, off
// the UI goroutine; the editor runs once editStartMsg arrives.
func (m *Model) startEdit(r Row) tea.Cmd {
	if m.editPending != "" {
		m.setStatus("an edit of "+m.editPending+" is already open; finish it first", true)
		return nil
	}
	m.editPending = r.ID
	load, subject := m.load.EditFields, r.ID
	return func() tea.Msg {
		orig, err := load(subject)
		if err != nil {
			return editStartMsg{subject: subject, err: err}
		}
		f, err := os.CreateTemp("", "harness-edit-"+subject+"-*.txt")
		if err != nil {
			return editStartMsg{subject: subject, err: err}
		}
		_, werr := f.WriteString(FormatEditFile(subject, orig))
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(f.Name())
			return editStartMsg{subject: subject, err: werr}
		}
		return editStartMsg{subject: subject, path: f.Name(), orig: orig}
	}
}

// runEditor suspends the UI while the editor owns the terminal.
func (m Model) runEditor(msg editStartMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.editPending = ""
		m.setStatus("edit "+msg.subject+": "+msg.err.Error(), true)
		return m, nil
	}
	cmd, warning, err := m.opts.EditorCommand(msg.path)
	if err != nil {
		m.editPending = ""
		os.Remove(msg.path)
		m.setStatus(withWarning("edit "+msg.subject+": "+err.Error(), warning), true)
		return m, nil
	}
	return m, m.opts.ExecProcess(cmd, func(err error) tea.Msg {
		return editDoneMsg{subject: msg.subject, path: msg.path, orig: msg.orig, warning: warning, err: err}
	})
}

// finishEdit reads the edited file. The file is removed only when there is
// nothing to save or the save succeeded (see the verdictMsg handling): on any
// error, cancel or refusal it stays and the status names it, so the human's
// text is never lost.
func (m Model) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	m.editPending = ""
	fail := func(text string) (tea.Model, tea.Cmd) {
		m.setStatus(withWarning(text, msg.warning), true)
		return m, nil
	}
	if msg.err != nil {
		return fail(fmt.Sprintf("editor failed: %v; %s", msg.err, keptText(msg.path)))
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		return fail(fmt.Sprintf("reading %s: %v", msg.path, err))
	}
	edited, err := ParseEditFile(string(data))
	if err != nil {
		return fail(fmt.Sprintf("edit of %s not applied: %v; %s", msg.subject, err, keptText(msg.path)))
	}
	if edited.Title == "" {
		m.setStatus(withWarning("title empty: edit of "+msg.subject+" cancelled; "+keptText(msg.path), msg.warning), false)
		return m, nil
	}
	changed := changedFields(msg.orig, edited)
	if changed == "" {
		os.Remove(msg.path)
		m.setStatus(withWarning("no changes to "+msg.subject, msg.warning), false)
		return m, nil
	}
	// Unchanged fields keep their stored text exactly: the file cannot carry
	// trailing blank lines, and those alone must not count as an edit.
	final := msg.orig
	old := msg.orig.normalized()
	if edited.Title != old.Title {
		final.Title = edited.Title
	}
	if edited.Description != old.Description {
		final.Description = edited.Description
	}
	if edited.Notes != old.Notes {
		final.Notes = edited.Notes
	}
	orig := msg.orig
	md := Modal{Decision: DecisionEdit, Subject: msg.subject, Edit: &final, EditOrig: &orig, Changed: changed, EditPath: msg.path}
	m.ui.Modal = &md
	return m.toConfirm(&md)
}

// cancelledText is the status of a modal closed without a decision.
func cancelledText(md *Modal) string {
	if md != nil && md.EditPath != "" {
		return "cancelled; " + keptText(md.EditPath)
	}
	return "cancelled"
}
