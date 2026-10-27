package dashboard

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	RowEpic
)

// Row is a selectable line: an AWAITING REVIEW entry, a task, or an epic
// header under IN FLIGHT.
type Row struct {
	Kind   RowKind
	ID     string
	IsEpic bool
	Review *ReviewRow
	Task   *TaskRow
	Group  *EpicGroup
}

// Key identifies the row across snapshots; a task listed both under review
// and in flight is two rows.
func (r Row) Key() string {
	switch r.Kind {
	case RowReview:
		return "review:" + r.ID
	case RowEpic:
		return "epic:" + r.ID
	}
	return "task:" + r.ID
}

// RequestSHA is the commit of the row's open review request, or "".
func (r Row) RequestSHA() string {
	switch {
	case r.Kind == RowReview && r.Review != nil:
		return r.Review.CommitSHA
	case r.Kind == RowTask && r.Task != nil && r.Task.Review != nil && !r.Task.Review.Resolved:
		return r.Task.Review.CommitSHA
	case r.Kind == RowEpic && r.Group != nil:
		return r.Group.ReviewSHA
	}
	return ""
}

func Rows(s Snapshot) []Row {
	_, rows := listLines(NewStyles(false), s)
	return rows
}

// Context is what decides whether an action applies right now.
type Context struct {
	Row *Row
	// Expandable: the open detail or diff pane has user text to expand.
	Expandable bool
}

// Action is a key bar entry. Start runs it on the selected row; built-in keys
// (details, expand, refresh, quit, select) have no Start and are handled by
// Model.Update.
type Action struct {
	Key string
	// Hint is shown, accented, before Label when Key is not a letter of Label.
	Hint    string
	Label   string
	Enabled func(c Context) bool
	Start   func(m *Model, row Row) tea.Cmd
}

func (a Action) enabled(c Context) bool {
	return a.Enabled == nil || a.Enabled(c)
}

// Detail is the detail pane content for the row whose Key it carries; Spans
// mark the user-written text in Lines.
type Detail struct {
	Key   string
	Title string
	Lines []string
	Spans []Span
}

// DiffView is the diff pane: context lines, then the diff.
type DiffView struct {
	Key string
	// SHA is set only for a diff of an open review request's commit; a
	// verdict from this pane is pinned to it. Branch-head diffs leave it empty.
	SHA         string
	Range       string
	Title       string
	Header      []string
	HeaderSpans []Span
	Diff        []string
	Failed      bool
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
	Selected     int
	Offset       int
	DetailOpen   bool
	Detail       *Detail
	DetailOffset int
	DiffOpen     bool
	Diff         *DiffView
	DiffKey      string
	DiffOffset   int
	// Expanded shows user text in full in the detail and diff panes.
	Expanded bool
	Modal    *Modal
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

func selectedRow(rows []Row, ui UIState) *Row {
	if ui.Selected >= 0 && ui.Selected < len(rows) {
		return &rows[ui.Selected]
	}
	return nil
}

func DiffPage(s Snapshot, ui UIState, height int) int {
	return max(computeLayout(NewStyles(false), s, ui, height).bodyH-1, 1)
}

func ClampDiffOffset(s Snapshot, ui UIState, width, height int) int {
	total := len(diffBody(NewStyles(false), ui, width))
	return clampOffset(ui.DiffOffset, -1, DiffPage(s, ui, height), total)
}

// DetailPage is how many lines the detail pane scrolls per page.
func DetailPage(s Snapshot, ui UIState, height int) int {
	return max(computeLayout(NewStyles(false), s, ui, height).detailH-1, 1)
}

func ClampDetailOffset(s Snapshot, ui UIState, width, height int) int {
	st := NewStyles(false)
	l := computeLayout(st, s, ui, height)
	_, body := detailBody(st, ui, selectedRow(l.rows, ui), width)
	return clampOffset(ui.DetailOffset, -1, l.detailH-1, len(body))
}

// Expandable reports whether the pane on screen hides user text at this
// width, or is expanded and can collapse.
func Expandable(s Snapshot, ui UIState, width int) bool {
	st := NewStyles(false)
	var hidden int
	switch {
	case ui.DiffOpen:
		if ui.Diff == nil || ui.Diff.Key != ui.DiffKey {
			return false
		}
		_, hidden = paneLines(st, ui.Diff.Header, ui.Diff.HeaderSpans, width-1, false)
	default:
		sel := selectedRow(Rows(s), ui)
		if !ui.DetailOpen || sel == nil || ui.Detail == nil || ui.Detail.Key != sel.Key() {
			return false
		}
		_, hidden = paneLines(st, ui.Detail.Lines, ui.Detail.Spans, width-1, false)
	}
	return hidden > 0 || ui.Expanded
}

// Render draws the interactive dashboard as exactly height lines.
func Render(s Snapshot, ui UIState, width, height int) string {
	st := NewStyles(ui.Color)
	if width < minWidth || height < minHeight {
		return st.Fail.Render(fmt.Sprintf("terminal too small (%dx%d, need %dx%d)", width, height, minWidth, minHeight))
	}
	l := computeLayout(st, s, ui, height)
	sel := selectedRow(l.rows, ui)

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
			out = append(out, detailPane(st, s, ui, sel, width, height, l.detailH)...)
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
	out = append(out, keyBar(st, barActions(ui), Context{Row: sel, Expandable: Expandable(s, ui, width)}))
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
		if a.Key == "a" || a.Key == "r" || a.Key == "x" {
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

func scrollNote(offset, page, total int) string {
	if total <= page {
		return ""
	}
	return fmt.Sprintf(" · lines %d-%d of %d", offset+1, min(offset+page, total), total)
}

func detailBody(st Styles, ui UIState, sel *Row, width int) (string, []string) {
	switch {
	case sel == nil:
		return "details", []string{st.Dim.Render("nothing selected")}
	case ui.Detail == nil || ui.Detail.Key != sel.Key():
		return sel.ID, []string{st.Dim.Render("loading…")}
	}
	// Lines are drawn after a one-cell gutter, so wrap at width-1 and the
	// final line-fit truncation never cuts them.
	body, _ := paneLines(st, ui.Detail.Lines, ui.Detail.Spans, width-1, ui.Expanded)
	return ui.Detail.Title, body
}

func detailPane(st Styles, s Snapshot, ui UIState, sel *Row, width, height, paneH int) []string {
	title, body := detailBody(st, ui, sel, width)
	page := paneH - 1
	offset := ClampDetailOffset(s, ui, width, height)
	out := []string{separator(st, title+scrollNote(offset, page, len(body)), width)}
	for i := offset; i < offset+page; i++ {
		if i < len(body) {
			out = append(out, " "+body[i])
		} else {
			out = append(out, "")
		}
	}
	return out
}

func diffBody(st Styles, ui UIState, width int) []string {
	if ui.Diff == nil || ui.Diff.Key != ui.DiffKey {
		return []string{st.Dim.Render("loading…")}
	}
	header, _ := paneLines(st, ui.Diff.Header, ui.Diff.HeaderSpans, width-1, ui.Expanded)
	var body []string
	for _, h := range header {
		body = append(body, " "+h)
	}
	for _, d := range ui.Diff.Diff {
		body = append(body, diffLineStyle(st, d))
	}
	if len(ui.Diff.Diff) == 0 && ui.Diff.Range != "" {
		body = append(body, st.Dim.Render(" (empty diff)"))
	}
	return body
}

func diffPane(st Styles, s Snapshot, ui UIState, height, width, bodyH int) []string {
	if bodyH <= 0 {
		return nil
	}
	title := "diff"
	if ui.Diff != nil && ui.Diff.Key == ui.DiffKey {
		title = ui.Diff.Title
	}
	body := diffBody(st, ui, width)
	page := bodyH - 1
	offset := ClampDiffOffset(s, ui, width, height)
	out := []string{separator(st, title+scrollNote(offset, page, len(body)), width)}
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

// keyBar always colours each hotkey; a disabled action has the rest of its
// word dimmed and its key in the faint accent.
func keyBar(st Styles, actions []Action, c Context) string {
	var parts []string
	for _, a := range actions {
		key, rest := st.Accent, st.Plain
		if !a.enabled(c) {
			key, rest = st.AccentDim, st.Dim
		}
		if a.Hint != "" {
			parts = append(parts, key.Render(a.Hint)+" "+rest.Render(a.Label))
			continue
		}
		parts = append(parts, accentKey(key, rest, a.Label, a.Key))
	}
	return " " + strings.Join(parts, "  ")
}

func accentKey(key, rest lipgloss.Style, label, k string) string {
	i := strings.Index(label, k)
	if i < 0 {
		return key.Render(k) + " " + rest.Render(label)
	}
	return renderNonEmpty(rest, label[:i]) + key.Render(k) + renderNonEmpty(rest, label[i+len(k):])
}

func renderNonEmpty(st lipgloss.Style, s string) string {
	if s == "" {
		return ""
	}
	return st.Render(s)
}
