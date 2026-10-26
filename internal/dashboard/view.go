package dashboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	minWidth  = 60
	minHeight = 12
	// Below this height an open detail pane takes the whole body.
	splitHeight = 24
)

type RowKind int

const (
	RowReview RowKind = iota
	RowTask
)

// Row is a selectable line: an AWAITING REVIEW entry or a task.
type Row struct {
	Kind   RowKind
	ID     string
	IsEpic bool
	Review *ReviewRow
	Task   *TaskRow
}

// Key identifies the row across snapshots; a task listed both under review
// and in flight is two rows.
func (r Row) Key() string {
	if r.Kind == RowReview {
		return "review:" + r.ID
	}
	return "task:" + r.ID
}

func Rows(s Snapshot) []Row {
	_, rows := listLines(NewStyles(false), s)
	return rows
}

// Action is a key bar entry. Start runs it on the selected row; built-in keys
// (details, refresh, quit, select) have no Start and are handled by
// Model.Update.
type Action struct {
	Key string
	// Hint is shown, accented, before Label when Key is not a letter of Label.
	Hint    string
	Label   string
	Enabled func(row *Row) bool
	Start   func(m *Model, row Row) tea.Cmd
}

func (a Action) enabled(row *Row) bool {
	return a.Enabled == nil || a.Enabled(row)
}

// Detail is the detail pane content for the row whose Key it carries.
type Detail struct {
	Key   string
	Title string
	Lines []string
}

// DiffView is the review pane: context lines, then the diff of exactly the
// requested commit.
type DiffView struct {
	Key    string
	Title  string
	Header []string
	Diff   []string
}

type ModalStep int

const (
	StepComment ModalStep = iota
	StepConfirm
)

type Modal struct {
	Decision string
	Subject  string
	SHA      string
	Step     ModalStep
	Comment  string
	// Input is the rendered line editor.
	Input string
	Error string
}

type UIState struct {
	Selected   int
	Offset     int
	DetailOpen bool
	Detail     *Detail
	DiffOpen   bool
	Diff       *DiffView
	DiffKey    string
	DiffOffset int
	Modal      *Modal
	// Status is shown above the key bar until the next key press.
	Status    string
	StatusErr bool
	// Stale is the last snapshot error while an older snapshot is shown.
	Stale      string
	Refreshing bool
	Color      bool
	Actions    []Action
}

type layout struct {
	header  []string
	lines   []listLine
	rows    []Row
	bodyH   int
	listH   int
	detailH int
	selLine int
}

func computeLayout(st Styles, s Snapshot, ui UIState, height int) layout {
	l := layout{selLine: -1}
	l.lines, l.rows = listLines(st, s)
	l.header = append([]string{headerLine(st, s, ui)}, warningLines(st, s)...)
	avail := height - len(l.header) - 1 - len(modalLines(st, ui.Modal))
	if ui.Status != "" {
		avail--
	}
	if avail < 0 {
		avail = 0
	}
	l.bodyH, l.listH = avail, avail
	if ui.DetailOpen && !ui.DiffOpen {
		if height >= splitHeight {
			l.detailH = avail / 2
		} else {
			l.detailH = avail
		}
		l.listH = avail - l.detailH
	}
	for i, ln := range l.lines {
		if ln.row == ui.Selected && ln.row >= 0 {
			l.selLine = i
		}
	}
	return l
}

// clampOffset keeps the selected line visible in a viewH-line window.
func clampOffset(offset, selLine, viewH, total int) int {
	if viewH <= 0 {
		return 0
	}
	if selLine >= 0 {
		if selLine < offset {
			offset = selLine
		}
		if selLine >= offset+viewH {
			offset = selLine - viewH + 1
		}
	}
	if last := total - viewH; offset > last {
		offset = last
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// ListOffset is the scroll offset Render will use; the model stores it so
// scrolling stays put between frames.
func ListOffset(s Snapshot, ui UIState, height int) int {
	l := computeLayout(NewStyles(false), s, ui, height)
	return clampOffset(ui.Offset, l.selLine, l.listH, len(l.lines))
}

func DiffPage(s Snapshot, ui UIState, height int) int {
	h := computeLayout(NewStyles(false), s, ui, height).bodyH - 1
	if h < 1 {
		return 1
	}
	return h
}

func ClampDiffOffset(s Snapshot, ui UIState, height int) int {
	total := 0
	if ui.Diff != nil {
		total = len(ui.Diff.Header) + max(len(ui.Diff.Diff), 1)
	}
	return clampOffset(ui.DiffOffset, -1, DiffPage(s, ui, height), total)
}

// Render draws the interactive dashboard as exactly height lines.
func Render(s Snapshot, ui UIState, width, height int) string {
	st := NewStyles(ui.Color)
	if width < minWidth || height < minHeight {
		return st.Fail.Render(fmt.Sprintf("terminal too small (%dx%d, need %dx%d)", width, height, minWidth, minHeight))
	}
	l := computeLayout(st, s, ui, height)
	var sel *Row
	if ui.Selected >= 0 && ui.Selected < len(l.rows) {
		sel = &l.rows[ui.Selected]
	}

	out := append([]string{}, l.header...)
	if ui.DiffOpen {
		out = append(out, diffPane(st, s, ui, height, width, l.bodyH)...)
	} else {
		offset := clampOffset(ui.Offset, l.selLine, l.listH, len(l.lines))
		for i := offset; i < offset+l.listH; i++ {
			switch {
			case i >= len(l.lines):
				out = append(out, "")
			case i == l.selLine:
				out = append(out, st.Accent.Render("›")+strings.TrimPrefix(l.lines[i].text, " "))
			default:
				out = append(out, l.lines[i].text)
			}
		}
		if l.detailH > 0 {
			out = append(out, detailPane(st, ui, sel, width, l.detailH)...)
		}
	}
	out = append(out, modalLines(st, ui.Modal)...)
	if ui.Status != "" {
		style := st.Pass
		if ui.StatusErr {
			style = st.Fail
		}
		out = append(out, " "+style.Render(ui.Status))
	}
	out = append(out, keyBar(st, barActions(ui), sel))
	for i, line := range out {
		out[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(out, "\n")
}

func barActions(ui UIState) []Action {
	if ui.Modal != nil {
		return []Action{{Key: "enter", Hint: "enter", Label: "confirm"}, {Key: "esc", Hint: "esc", Label: "cancel"}}
	}
	actions := ui.Actions
	if actions == nil {
		actions = DefaultActions(false)
	}
	if !ui.DiffOpen {
		return actions
	}
	var out []Action
	for _, a := range actions {
		if a.Key == "a" || a.Key == "r" {
			out = append(out, a)
		}
	}
	return append(out,
		Action{Key: "down", Hint: "↑↓ pgup pgdn", Label: "scroll"},
		Action{Key: "esc", Hint: "esc", Label: "close"},
		Action{Key: "q", Label: "quit"},
	)
}

func headerLine(st Styles, s Snapshot, ui UIState) string {
	text := "harness dashboard · loading…"
	if !s.Now.IsZero() {
		text = headerText(s)
	}
	line := st.Header.Render(text)
	if ui.Refreshing {
		line += " " + st.Running.Render("⟳")
	}
	if ui.Stale != "" {
		line += "  " + st.Fail.Render("stale: "+ui.Stale)
	}
	return line
}

func separator(st Styles, title string, width int) string {
	sep := "── " + title + " "
	if pad := width - ansi.StringWidth(sep); pad > 0 {
		sep += strings.Repeat("─", pad)
	}
	return st.Dim.Render(sep)
}

func detailPane(st Styles, ui UIState, sel *Row, width, height int) []string {
	title := "details"
	var body []string
	switch {
	case sel == nil:
		body = []string{st.Dim.Render("nothing selected")}
	case ui.Detail == nil || ui.Detail.Key != sel.Key():
		title = sel.ID
		body = []string{st.Dim.Render("loading…")}
	default:
		title = ui.Detail.Title
		body = ui.Detail.Lines
	}
	out := []string{separator(st, title, width)}
	for i := 0; i < height-1; i++ {
		if i < len(body) {
			out = append(out, " "+body[i])
		} else {
			out = append(out, "")
		}
	}
	if len(body) > height-1 && height > 1 {
		out[len(out)-1] = st.Dim.Render(fmt.Sprintf(" … %d more lines", len(body)-(height-2)))
	}
	return out
}

func diffPane(st Styles, s Snapshot, ui UIState, height, width, bodyH int) []string {
	if bodyH <= 0 {
		return nil
	}
	var title string
	var body []string
	if ui.Diff == nil || ui.Diff.Key != ui.DiffKey {
		title = "diff"
		body = []string{st.Dim.Render("loading…")}
	} else {
		title = ui.Diff.Title
		for _, h := range ui.Diff.Header {
			body = append(body, " "+h)
		}
		for _, d := range ui.Diff.Diff {
			body = append(body, diffLineStyle(st, d))
		}
		if len(ui.Diff.Diff) == 0 {
			body = append(body, st.Dim.Render(" (empty diff)"))
		}
	}
	page := bodyH - 1
	offset := ClampDiffOffset(s, ui, height)
	if total := len(body); total > page {
		title += fmt.Sprintf(" · lines %d-%d of %d", offset+1, min(offset+page, total), total)
	}
	out := []string{separator(st, title, width)}
	for i := offset; i < offset+page; i++ {
		if i < len(body) {
			out = append(out, body[i])
		} else {
			out = append(out, "")
		}
	}
	return out
}

func diffLineStyle(st Styles, line string) string {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "diff "):
		return st.Group.Render(line)
	case strings.HasPrefix(line, "+"):
		return st.Pass.Render(line)
	case strings.HasPrefix(line, "-"):
		return st.Fail.Render(line)
	case strings.HasPrefix(line, "@@"):
		return st.Accent.Render(line)
	}
	return line
}

func modalLines(st Styles, m *Modal) []string {
	if m == nil {
		return nil
	}
	verb := "Approve"
	commentPrompt := "Comment (optional, enter to continue):"
	if m.Decision == "request_changes" {
		verb = "Reject"
		commentPrompt = "Comment (required):"
	}
	lines := []string{st.Header.Render(fmt.Sprintf("── %s %s at %s? ──", verb, m.Subject, shortSHA(m.SHA)))}
	if m.Step == StepComment {
		lines = append(lines, " "+commentPrompt)
	} else {
		if m.Comment != "" {
			lines = append(lines, " Comment: "+m.Comment)
		}
		lines = append(lines, fmt.Sprintf(" Type %s to confirm:", m.Subject))
	}
	lines = append(lines, " > "+m.Input)
	if m.Error != "" {
		lines = append(lines, " "+st.Fail.Render(m.Error))
	}
	return lines
}

func keyBar(st Styles, actions []Action, sel *Row) string {
	var parts []string
	for _, a := range actions {
		on := a.enabled(sel)
		switch {
		case !on && a.Hint != "":
			parts = append(parts, st.Dim.Render(a.Hint+" "+a.Label))
		case !on:
			parts = append(parts, st.Dim.Render(a.Label))
		case a.Hint != "":
			parts = append(parts, st.Accent.Render(a.Hint)+" "+st.Plain.Render(a.Label))
		default:
			parts = append(parts, accentKey(st, a.Label, a.Key))
		}
	}
	return " " + strings.Join(parts, "  ")
}

func accentKey(st Styles, label, key string) string {
	i := strings.Index(label, key)
	if i < 0 {
		return st.Accent.Render(key) + " " + st.Plain.Render(label)
	}
	return st.Plain.Render(label[:i]) + st.Accent.Render(key) + st.Plain.Render(label[i+len(key):])
}
