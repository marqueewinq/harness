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
	err     error
}

// DefaultEditorCommand opens path in $VISUAL, $EDITOR, nano or vi.
func DefaultEditorCommand(path string) (*exec.Cmd, error) {
	argv, err := ResolveEditor(os.Getenv, exec.LookPath)
	if err != nil {
		return nil, err
	}
	return exec.Command(argv[0], append(argv[1:], path)...), nil
}

func isEditable(r *Row) bool {
	return r != nil && (r.Kind == RowTask || r.Kind == RowEpic)
}

// startEdit loads the subject's fields and writes them to a temp file, off
// the UI goroutine; the editor runs once editStartMsg arrives.
func (m *Model) startEdit(r Row) tea.Cmd {
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
		m.setStatus("edit "+msg.subject+": "+msg.err.Error(), true)
		return m, nil
	}
	cmd, err := m.opts.EditorCommand(msg.path)
	if err != nil {
		os.Remove(msg.path)
		m.setStatus("edit "+msg.subject+": "+err.Error(), true)
		return m, nil
	}
	return m, m.opts.ExecProcess(cmd, func(err error) tea.Msg {
		return editDoneMsg{subject: msg.subject, path: msg.path, orig: msg.orig, err: err}
	})
}

// finishEdit reads the edited file. The file is removed unless it holds an
// edit that could not be applied, so a typo never loses the human's text.
func (m Model) finishEdit(msg editDoneMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.setStatus(fmt.Sprintf("editor failed: %v (your text is in %s)", msg.err, msg.path), true)
		return m, nil
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.setStatus(fmt.Sprintf("reading %s: %v", msg.path, err), true)
		return m, nil
	}
	edited, err := ParseEditFile(string(data))
	if err != nil {
		m.setStatus(fmt.Sprintf("edit of %s not applied: %v (your text is in %s)", msg.subject, err, msg.path), true)
		return m, nil
	}
	os.Remove(msg.path)
	if edited.Title == "" {
		m.setStatus("title empty: edit of "+msg.subject+" cancelled", false)
		return m, nil
	}
	changed := changedFields(msg.orig, edited)
	if changed == "" {
		m.setStatus("no changes to "+msg.subject, false)
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
	md := Modal{Decision: DecisionEdit, Subject: msg.subject, Edit: &final, Changed: changed}
	m.ui.Modal = &md
	return m.toConfirm(&md)
}
