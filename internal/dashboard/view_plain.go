package dashboard

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/marqueewinq/harness/internal"
	"github.com/muesli/termenv"
)

type Styles struct {
	Plain     lipgloss.Style
	Header    lipgloss.Style
	Section   lipgloss.Style
	Group     lipgloss.Style
	Dim       lipgloss.Style
	Pass      lipgloss.Style
	Approved  lipgloss.Style
	Running   lipgloss.Style
	Fail      lipgloss.Style
	Gutter    lipgloss.Style
	Accent    lipgloss.Style
	AccentDim lipgloss.Style
}

// NewStyles with color=false renders plain text with no escape sequences.
func NewStyles(color bool) Styles {
	r := lipgloss.NewRenderer(io.Discard)
	if color {
		r.SetColorProfile(termenv.ANSI256)
		// The default renderer's answer is cached (Bubble Tea queries it at
		// init); this renderer writes to io.Discard and cannot ask.
		r.SetHasDarkBackground(lipgloss.HasDarkBackground())
	} else {
		r.SetColorProfile(termenv.Ascii)
	}
	return Styles{
		Plain:    r.NewStyle(),
		Header:   r.NewStyle().Bold(true),
		Section:  r.NewStyle().Bold(true).Underline(true),
		Group:    r.NewStyle().Bold(true),
		Dim:      r.NewStyle().Faint(true),
		Pass:     r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "2"}),
		Approved: r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "30", Dark: "6"}),
		Running:  r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "3"}),
		Fail:     r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "1"}),
		Gutter:   r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "1"}).Bold(true),
		Accent:   r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "6"}).Bold(true),
		// AccentDim keeps a disabled action's hotkey coloured but faint.
		AccentDim: r.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "6"}).Faint(true),
	}
}

// UseColor follows https://no-color.org: any non-empty NO_COLOR disables colour.
func UseColor(isTerminal bool, noColor string) bool {
	return isTerminal && noColor == ""
}

// RenderPlain is the `harness dashboard --once` output. width <= 0 disables truncation.
func RenderPlain(s Snapshot, width int, color bool) string {
	st := NewStyles(color)
	lines := append([]string{st.Header.Render(headerText(s))}, warningLines(st, s)...)
	list, _ := listLines(st, s, ListOptions{})
	for _, l := range list {
		lines = append(lines, l.text)
	}
	if width > 0 {
		for i, l := range lines {
			lines[i] = ansi.Truncate(l, width, "…")
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func headerText(s Snapshot) string {
	return fmt.Sprintf("%s · %s @ %s · %s", s.Repo, s.MainBranch, orDash(s.MainHead), s.Now.Format("15:04:05"))
}

func warningLines(st Styles, s Snapshot) []string {
	var out []string
	for _, w := range s.Warnings {
		out = append(out, st.Running.Render("warning: "+w))
	}
	return out
}

// listLine.row indexes the Row the line shows, or is -1.
type listLine struct {
	text string
	row  int
}

// Dashboard list views.
const (
	ViewActive = "active"
	ViewAll    = "all"
)

// ListOptions shape the list. The zero value is the active view with no
// filter and no fold markers: what --once prints.
type ListOptions struct {
	View string
	// Filter keeps rows whose id, title, state, review type or epic contains
	// it, ignoring case.
	Filter string
	// Collapsed epics show their header only; Folding draws ▸/▾ and task
	// counts on epic headers and applies Collapsed.
	Collapsed map[string]bool
	Folding   bool
}

func (o ListOptions) matches(fields ...string) bool {
	if o.Filter == "" {
		return true
	}
	f := strings.ToLower(o.Filter)
	for _, s := range fields {
		if strings.Contains(strings.ToLower(s), f) {
			return true
		}
	}
	return false
}

// listLines lays out the sections; rows lists the selectable rows in display order.
func listLines(st Styles, s Snapshot, o ListOptions) (lines []listLine, rows []Row) {
	add := func(text string) { lines = append(lines, listLine{text: text, row: -1}) }
	addRow := func(text string, r Row) {
		lines = append(lines, listLine{text: text, row: len(rows)})
		rows = append(rows, r)
	}
	// A section whose rows are all filtered out collapses to "· none".
	section := func(title string, body func()) {
		start := len(lines)
		add(st.Section.Render(title))
		body()
		if len(lines) == start+1 {
			lines[start].text = st.Dim.Render(title + " · none")
		}
	}
	groups := func(gs []EpicGroup, line func(TaskRow) string, selectable, dimDone bool) {
		for gi := range gs {
			g := &gs[gi]
			epicText := ""
			if g.Epic != nil {
				epicText = g.Epic.ID + " " + g.Epic.Title
			}
			var shown []*TaskRow
			for i := range g.Tasks {
				t := &g.Tasks[i]
				if o.matches(t.ID, t.Title, string(t.State), string(t.ReviewType), epicText) {
					shown = append(shown, t)
				}
			}
			if len(shown) == 0 && !(g.Epic != nil && o.matches(epicText, string(g.Epic.ReviewType))) {
				continue
			}
			header := groupHeader(st, *g)
			folded := false
			if g.Epic != nil && selectable && o.Folding {
				folded = o.Collapsed[g.Epic.ID]
				marker := "▾"
				if folded {
					marker = "▸"
				}
				header = " " + st.Accent.Render(marker) + header + fmt.Sprintf(" · %d tasks", len(g.Tasks))
			}
			if g.Epic != nil && selectable {
				addRow(header, Row{Kind: RowEpic, ID: g.Epic.ID, IsEpic: true, Group: g})
			} else {
				add(header)
			}
			if folded {
				continue
			}
			for _, t := range shown {
				text := line(*t)
				if dimDone && finished(t.State) {
					// Keep the leading gutter space plain: the cursor replaces it.
					text = " " + st.Dim.Render(strings.TrimPrefix(ansi.Strip(text), " "))
				}
				addRow(text, Row{Kind: RowTask, ID: t.ID, Task: t, Group: g})
			}
		}
	}

	section("AWAITING REVIEW", func() {
		for i := range s.Reviews {
			r := &s.Reviews[i]
			if o.matches(r.SubjectID, r.Title, r.Reviewer) {
				addRow(reviewLine(st, *r), Row{Kind: RowReview, ID: r.SubjectID, IsEpic: r.IsEpic, Review: r})
			}
		}
	})
	if o.View == ViewAll {
		section("ALL TASKS", func() { groups(s.Browse, func(t TaskRow) string { return taskLine(st, t) }, true, true) })
	} else {
		section("IN FLIGHT", func() { groups(s.Groups, func(t TaskRow) string { return taskLine(st, t) }, true, false) })
	}
	section("MERGE QUEUE", func() {
		for _, q := range s.Queue {
			if o.matches(q.TaskID) {
				add(queueLine(st, q))
			}
		}
	})
	if o.View != ViewAll {
		recentTitle := "RECENT (24h)"
		if s.All {
			recentTitle = "DONE"
		}
		section(recentTitle, func() { groups(s.Recent, func(t TaskRow) string { return recentLine(st, t) }, false, false) })
	}
	if o.Filter != "" && len(rows) == 0 {
		add(st.Dim.Render("no matches"))
	}
	return lines, rows
}

func gutter(st Styles, attn string) string {
	if attn != "" {
		return " " + st.Gutter.Render("!") + " "
	}
	return "   "
}

func reviewLine(st Styles, r ReviewRow) string {
	cols := fmt.Sprintf("%-9s %-8s %4s", r.SubjectID, shortSHA(r.CommitSHA), fmtAge(r.Age))
	return joinParts(gutter(st, r.Attention)+cols, reviewerText(st, r.Reviewer), st.Dim.Render(r.Title))
}

func reviewerText(st Styles, reviewer string) string {
	switch reviewer {
	case "":
		return ""
	case ReviewerHuman:
		return st.Running.Render("human")
	case internal.ReviewerRunning, ReviewerPending:
		return st.Running.Render("agent reviewer " + reviewer)
	case internal.ReviewerVerdict:
		return st.Pass.Render("agent reviewer " + reviewer)
	}
	return st.Fail.Render("agent reviewer " + reviewer)
}

func groupHeader(st Styles, g EpicGroup) string {
	if g.Epic == nil {
		return " " + st.Group.Render("No epic")
	}
	name := g.Epic.ID + " " + g.Epic.Title
	if g.Project != nil {
		name = g.Project.ID + " › " + name
	}
	parts := []string{st.Group.Render(name)}
	if g.Branch != "" {
		parts = append(parts, g.Branch)
	}
	parts = append(parts, fmt.Sprintf("%d/%d done", g.Done, g.Total))
	switch {
	case g.Approval != "":
		parts = append(parts, "approval "+approvalStyle(st, g.Approval).Render(g.Approval))
	case g.EpicReview != "":
		parts = append(parts, g.EpicReview)
	}
	return " " + strings.Join(parts, " · ")
}

func approvalStyle(st Styles, approval string) lipgloss.Style {
	switch {
	case strings.HasSuffix(approval, "not started yet"):
		return st.Dim
	case strings.Contains(approval, " · reviewer "):
		return st.Fail
	case strings.HasSuffix(approval, "approved"), strings.HasPrefix(approval, "none · "):
		return st.Pass
	}
	return st.Running
}

func taskLine(st Styles, t TaskRow) string {
	cols := fmt.Sprintf("%-9s %s %4s", t.ID, stateStyle(st, t.State).Render(fmt.Sprintf("%-17s", t.State)), fmtAge(t.Age))
	parts := []string{gutter(st, t.Attention) + cols, checksText(st, t.Checks)}
	if t.QueuePos > 0 {
		parts = append(parts, fmt.Sprintf("queue #%d", t.QueuePos))
	}
	if t.Approval != "" {
		parts = append(parts, approvalStyle(st, t.Approval).Render(t.Approval))
	}
	switch {
	case t.Attention != "":
		parts = append(parts, st.Fail.Render(t.Attention))
	case t.Approval == "":
		parts = append(parts, reviewerText(st, t.Reviewer))
	}
	parts = append(parts, st.Dim.Render(t.Title))
	return joinParts(parts...)
}

func checksText(st Styles, checks []CheckStatus) string {
	var running, failed []string
	passed := 0
	for _, c := range checks {
		switch c.Result {
		case CheckRunning:
			elapsed := "-"
			if c.ElapsedKnown {
				elapsed = fmtAge(c.Elapsed)
			}
			running = append(running, c.Name+" ▶ "+elapsed)
		case CheckFail:
			failed = append(failed, c.Name)
		case CheckPass:
			passed++
		}
	}
	switch {
	case len(running) > 0:
		return st.Running.Render(strings.Join(running, ", "))
	case len(failed) > 0:
		return st.Fail.Render("✗ " + strings.Join(failed, ","))
	case len(checks) > 0:
		return st.Pass.Render(fmt.Sprintf("✓ %d/%d", passed, len(checks)))
	}
	return ""
}

func queueLine(st Styles, q QueueRow) string {
	attn, status := "", st.Pass.Render("live")
	if q.Stale {
		attn, status = "stale", st.Fail.Render("stale")
	}
	return joinParts(gutter(st, attn)+fmt.Sprintf("%d %-9s pid %-7d hb %-4s", q.Position, q.TaskID, q.PID, fmtAge(q.HeartbeatAge)), status)
}

func recentLine(st Styles, t TaskRow) string {
	cols := fmt.Sprintf("%-9s %s %8s", t.ID, stateStyle(st, t.State).Render(fmt.Sprintf("%-6s", t.State)), fmtAge(t.Age)+" ago")
	return joinParts("   "+cols, st.Dim.Render(t.Title))
}

func stateStyle(st Styles, s internal.TaskState) lipgloss.Style {
	switch s {
	case internal.StateApproved:
		return st.Approved
	case internal.StateChecksPassed, internal.StateMerged:
		return st.Pass
	case internal.StateChecksRunning, internal.StateReviewRequested, internal.StateQueued, internal.StateMerging, internal.StateRequestChanges:
		return st.Running
	case internal.StateChecksFailed, internal.StateMergeFailed:
		return st.Fail
	case internal.StatePlanned, internal.StateClosed:
		return st.Dim
	}
	return st.Plain
}

func joinParts(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "  ")
}

func fmtAge(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
