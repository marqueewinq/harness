package dashboard

import (
	"fmt"
	"strings"

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

// Action is a key bar entry. Row actions (approve, reject, diff) gain Confirm
// and Run fields with the human review task; built-in keys (details,
// refresh, quit, select) are handled by Model.Update.
type Action struct {
	Key string
	// Hint is shown, accented, before Label when Key is not a letter of Label.
	Hint    string
	Label   string
	Enabled func(row *Row) bool
}

func (a Action) enabled(row *Row) bool {
	return a.Enabled == nil || a.Enabled(row)
}

func DefaultActions() []Action {
	never := func(*Row) bool { return false }
	return []Action{
		{Key: "a", Label: "approve", Enabled: never},
		{Key: "r", Label: "reject", Enabled: never},
		{Key: "d", Label: "diff", Enabled: never},
		{Key: "enter", Hint: "enter", Label: "details", Enabled: func(r *Row) bool { return r != nil }},
		{Key: "f", Label: "refresh"},
		{Key: "q", Label: "quit"},
		{Key: "up", Hint: "↑↓", Label: "select"},
	}
}

// Detail is the detail pane content for the row whose Key it carries.
type Detail struct {
	Key   string
	Title string
	Lines []string
}

type UIState struct {
	Selected   int
	Offset     int
	DetailOpen bool
	Detail     *Detail
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
	listH   int
	detailH int
	selLine int
}

func computeLayout(st Styles, s Snapshot, ui UIState, height int) layout {
	l := layout{selLine: -1}
	l.lines, l.rows = listLines(st, s)
	l.header = append([]string{headerLine(st, s, ui)}, warningLines(st, s)...)
	avail := height - len(l.header) - 1
	if avail < 0 {
		avail = 0
	}
	l.listH = avail
	if ui.DetailOpen {
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
	actions := ui.Actions
	if actions == nil {
		actions = DefaultActions()
	}
	out = append(out, keyBar(st, actions, sel))
	for i, line := range out {
		out[i] = ansi.Truncate(line, width, "…")
	}
	return strings.Join(out, "\n")
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
	sep := "── " + title + " "
	if pad := width - ansi.StringWidth(sep); pad > 0 {
		sep += strings.Repeat("─", pad)
	}
	out := []string{st.Dim.Render(sep)}
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
