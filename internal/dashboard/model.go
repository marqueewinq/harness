package dashboard

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/marqueewinq/harness/internal"
)

// Loader runs off the UI goroutine, inside tea.Cmds.
type Loader struct {
	Snapshot func(now time.Time) (Snapshot, error)
	Detail   func(row Row, now time.Time) (Detail, error)
	Diff     func(row Row) (DiffView, error)
	// Verdict records a human verdict the dashboard has confirmed and
	// returns the CLI's report. sha is the request commit the human was
	// shown; the verdict is refused if the open request is for another.
	Verdict func(subject, decision, comment, sha string) (string, error)
	// Stop ends the in-flight review of subject at request commit sha.
	Stop func(subject string, outcome internal.StopOutcome, sha string) (string, error)
	// SetType records a review type change a human confirmed.
	SetType func(subject string, r internal.ReviewType, reason string) (string, error)
	// RemovePreApproval undoes a pre-approval a human confirmed.
	RemovePreApproval func(subject, reason string) (string, error)
	// EditFields reads a task's or epic's title, description and notes;
	// Edit records an edit a human confirmed, replacing old (the text the
	// editor was given) with new, and is refused if the text changed since.
	EditFields func(subject string) (EditFields, error)
	Edit       func(subject string, old, new EditFields) (string, error)
}

// DashboardVia labels dashboard verdicts: terminal:<user> (dashboard).
const DashboardVia = "dashboard"

func AppLoader(app *internal.App, all bool) Loader {
	return Loader{
		Snapshot: func(now time.Time) (Snapshot, error) { return BuildSnapshot(app, now, all) },
		Detail:   func(row Row, now time.Time) (Detail, error) { return BuildDetail(app, row, now) },
		Diff:     func(row Row) (DiffView, error) { return BuildDiff(app, row) },
		Verdict: func(subject, decision, comment, sha string) (string, error) {
			return app.RecordTerminalVerdict(subject, decision, comment, DashboardVia, sha)
		},
		Stop: func(subject string, outcome internal.StopOutcome, sha string) (string, error) {
			return app.RecordTerminalStop(subject, outcome, DashboardVia, sha)
		},
		SetType: func(subject string, r internal.ReviewType, reason string) (string, error) {
			return app.RecordTerminalReviewType(subject, r, DashboardVia, reason)
		},
		RemovePreApproval: func(subject, reason string) (string, error) {
			return app.RecordTerminalRemovePreApproval(subject, DashboardVia, reason)
		},
		EditFields: func(subject string) (EditFields, error) {
			if internal.IsEpicID(subject) {
				e, err := app.DB.GetEpic(subject)
				if err != nil {
					return EditFields{}, err
				}
				return EditFields{Title: e.Title, Description: e.Description, Notes: e.Notes}, nil
			}
			t, err := app.DB.GetTask(subject)
			if err != nil {
				return EditFields{}, err
			}
			return EditFields{Title: t.Title, Description: t.Description, Notes: t.Notes}, nil
		},
		Edit: func(subject string, old, new EditFields) (string, error) {
			return app.RecordTerminalEdit(subject, internal.SubjectText(old), internal.SubjectText(new), DashboardVia)
		},
	}
}

// pastGate: a QUEUED or MERGING task's pre-approval can no longer be removed.
func pastGate(r *Row) bool {
	return r != nil && r.Kind == RowTask && r.Task != nil &&
		(r.Task.State == internal.StateQueued || r.Task.State == internal.StateMerging)
}

// changeReviewType sets subject's review type to; with a review in flight
// (sha set) a change to none approves the requested commit and a change to
// human revokes the request, both stopping the reviewer. The stop runs
// first: it is the step that can be refused (commit moved, request changed),
// and a refusal must leave the type untouched rather than claim a
// pre-approval or human gate that did not take effect.
func changeReviewType(load Loader, subject string, to internal.ReviewType, reason, sha string) (string, error) {
	var stopped string
	if sha != "" && to != internal.ReviewDefault {
		outcome := internal.StopRevoke
		if to == internal.ReviewNone {
			outcome = internal.StopApprove
		}
		report, err := load.Stop(subject, outcome, sha)
		if err != nil {
			return "", err
		}
		stopped = report
	}
	report, err := load.SetType(subject, to, reason)
	if err != nil {
		if stopped != "" {
			return stopped, fmt.Errorf("review stopped (%s), but setting review type %s failed: %w; set it again with t",
				firstLine(stopped), to, err)
		}
		return "", err
	}
	return stopped + report, nil
}

func firstLine(s string) string {
	return strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]
}

// rowReviewType is the review type of a task row or epic header whose type
// may still change (not merged or closed).
func rowReviewType(r *Row) (internal.ReviewType, bool) {
	var rt internal.ReviewType
	switch {
	case r == nil:
		return "", false
	case r.Kind == RowTask && r.Task != nil && r.Task.State != internal.StateMerged && r.Task.State != internal.StateClosed:
		rt = r.Task.ReviewType
	case r.Kind == RowEpic && r.Group != nil && r.Group.Epic != nil && r.Group.Epic.Status != internal.EpicStatusClosed:
		rt = r.Group.Epic.ReviewType
	default:
		return "", false
	}
	if rt == "" {
		rt = internal.ReviewDefault
	}
	return rt, true
}

type Options struct {
	Interval time.Duration
	Color    bool
	// AllowActions enables approve/reject/diff; DenyReason explains a refusal.
	AllowActions bool
	DenyReason   string
	// EditorCommand builds the editor process for a file (default
	// DefaultEditorCommand); ExecProcess hands it the terminal (default
	// tea.ExecProcess). Tests replace both.
	EditorCommand func(path string) (*exec.Cmd, error)
	ExecProcess   func(*exec.Cmd, tea.ExecCallback) tea.Cmd
}

type snapshotMsg struct {
	snap Snapshot
	err  error
}

type detailMsg struct{ detail Detail }

type diffMsg struct{ diff DiffView }

type verdictMsg struct {
	report string
	err    error
	// skipped is the decision kind whose confirmation was skipped, if any.
	skipped string
}

type tickMsg time.Time

type Model struct {
	load Loader
	opts Options
	now  func() time.Time

	snap   Snapshot
	ui     UIState
	rows   []Row
	selKey string
	width  int
	height int
	input  textinput.Model

	loading       bool
	detailLoading bool
	detailAgain   bool
	diffLoading   bool

	// skipConfirm holds decision kinds the human chose "yes, don't ask
	// again" for. It lives only as long as this Model: never stored.
	skipConfirm map[string]bool
	// editPending names the subject whose edit file is open, from `e` until
	// the editor's result is handled; a second `e` meanwhile is refused.
	editPending string
}

func NewModel(load Loader, opts Options) Model {
	if opts.EditorCommand == nil {
		opts.EditorCommand = DefaultEditorCommand
	}
	if opts.ExecProcess == nil {
		opts.ExecProcess = tea.ExecProcess
	}
	return Model{
		load:        load,
		opts:        opts,
		now:         time.Now,
		loading:     true,
		skipConfirm: map[string]bool{},
		ui: UIState{Color: opts.Color, Actions: DefaultActions(opts.AllowActions), Refreshing: true,
			List: ListOptions{View: ViewActive, Collapsed: map[string]bool{}, Folding: true}},
	}
}

// DefaultActions: verdicts need an open review request and allowed actions;
// viewing a diff is harmless, so it only needs a row.
func DefaultActions(allowed bool) []Action {
	verdict := func(c Context) bool { return allowed && c.Row != nil && c.Row.RequestSHA() != "" }
	hasRow := func(c Context) bool { return c.Row != nil }
	typed := func(c Context) bool {
		_, ok := rowReviewType(c.Row)
		return allowed && ok
	}
	isNone := func(c Context) bool {
		rt, ok := rowReviewType(c.Row)
		return ok && rt == internal.ReviewNone
	}
	// p toggles: pre-approve, or on a none subject remove the pre-approval.
	preApprovable := func(c Context) bool {
		_, ok := rowReviewType(c.Row)
		return allowed && ok && !(isNone(c) && pastGate(c.Row))
	}
	togglePreApproval := func(m *Model, r Row) tea.Cmd {
		if rt, _ := rowReviewType(&r); rt == internal.ReviewNone {
			return m.openModal(DecisionRemovePreApproval, r)
		}
		return m.openModal(DecisionPreApprove, r)
	}
	return []Action{
		{Key: "a", Label: "approve", Enabled: verdict, Start: func(m *Model, r Row) tea.Cmd { return m.openModal("approve", r) }},
		{Key: "r", Label: "reject", Enabled: verdict, Start: func(m *Model, r Row) tea.Cmd { return m.openModal("request_changes", r) }},
		{Key: "p", Label: "pre-approve", Enabled: preApprovable, Start: togglePreApproval, AltLabel: "remove pre-approval", UseAlt: isNone},
		{Key: "t", Label: "type", Enabled: typed, Start: func(m *Model, r Row) tea.Cmd { return m.openModal(DecisionType, r) }},
		{Key: "e", Label: "edit", Enabled: func(c Context) bool { return allowed && isEditable(c.Row) }, Start: func(m *Model, r Row) tea.Cmd { return m.startEdit(r) }},
		{Key: "d", Label: "diff", Enabled: hasRow, Start: func(m *Model, r Row) tea.Cmd { return m.openDiff(r) }},
		{Key: "x", Label: "expand", Enabled: func(c Context) bool { return c.Expandable }},
		{Key: "enter", Hint: "enter", Label: "details", Enabled: hasRow},
		{Key: "v", Label: "view"},
		{Key: "/", Hint: "/", Label: "filter"},
		{Key: " ", Hint: "space", Label: "fold", Enabled: func(c Context) bool { return c.Row != nil && c.Row.Kind == RowEpic }},
		{Key: "c", Label: "collapse"},
		{Key: "f", Label: "refresh"},
		{Key: "q", Label: "quit"},
		{Key: "up", Hint: "↑↓", Label: "select"},
	}
}

func (m Model) context() Context {
	return Context{Row: m.actionRow(), Expandable: Expandable(m.snap, m.ui, m.width)}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), m.tickCmd())
}

func (m Model) refreshCmd() tea.Cmd {
	load, now := m.load.Snapshot, m.now
	return func() tea.Msg {
		s, err := load(now())
		return snapshotMsg{snap: s, err: err}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.opts.Interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// startDetail never overlaps loads; a request made meanwhile is served once
// the running load returns.
func (m *Model) startDetail() tea.Cmd {
	row := m.selected()
	if row == nil || !m.ui.DetailOpen {
		return nil
	}
	if m.detailLoading {
		m.detailAgain = true
		return nil
	}
	m.detailLoading = true
	r, load, now := *row, m.load.Detail, m.now
	return func() tea.Msg {
		d, err := load(r, now())
		if err != nil {
			d = Detail{Key: r.Key(), Title: r.ID, Lines: []string{"error: " + err.Error()}}
		}
		return detailMsg{detail: d}
	}
}

func (m Model) selected() *Row {
	if m.ui.Selected >= 0 && m.ui.Selected < len(m.rows) {
		return &m.rows[m.ui.Selected]
	}
	return nil
}

// startRefresh never overlaps loads: a slow snapshot just skips ticks.
func (m *Model) startRefresh() tea.Cmd {
	if m.loading {
		return nil
	}
	m.loading, m.ui.Refreshing = true, true
	return m.refreshCmd()
}

func (m *Model) fixOffset() {
	if m.height > 0 {
		m.ui.Offset = ListOffset(m.snap, m.ui, m.height)
		m.ui.DiffOffset = ClampDiffOffset(m.snap, m.ui, m.width, m.height)
		m.ui.DetailOffset = ClampDetailOffset(m.snap, m.ui, m.width, m.height)
	}
}

func (m *Model) move(delta int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	next := min(max(m.ui.Selected+delta, 0), len(m.rows)-1)
	if next == m.ui.Selected {
		return nil
	}
	m.ui.Selected = next
	m.selKey = m.rows[next].Key()
	m.ui.Expanded, m.ui.DetailOffset = false, 0
	m.fixOffset()
	return m.startDetail()
}

// applySnapshot keeps the selection on the same row when it still exists.
func (m *Model) applySnapshot(s Snapshot) {
	m.snap = s
	m.relist()
}

// relist rebuilds the rows for the current snapshot, view, filter and
// folding. The selection stays on the same row while it is listed, else on
// the row now at its position (the nearest one). It returns the detail load
// when the selection moved to another row.
func (m *Model) relist() tea.Cmd {
	before := m.selKey
	m.rows = RowsFor(m.snap, m.ui.List)
	sel := -1
	for i, r := range m.rows {
		if r.Key() == m.selKey {
			sel = i
		}
	}
	if sel < 0 {
		sel = max(min(m.ui.Selected, len(m.rows)-1), 0)
	}
	m.ui.Selected = sel
	if r := m.selected(); r != nil {
		m.selKey = r.Key()
	}
	m.fixOffset()
	if m.selKey != before {
		return m.startDetail()
	}
	return nil
}

func (m *Model) toggleView() tea.Cmd {
	if m.ui.List.View == ViewAll {
		m.ui.List.View = ViewActive
	} else {
		m.ui.List.View = ViewAll
	}
	return m.relist()
}

// toggleFold folds or unfolds the selected epic header's tasks.
func (m *Model) toggleFold() tea.Cmd {
	r := m.selected()
	if r == nil || r.Kind != RowEpic {
		m.setStatus("space folds an epic header; select one", true)
		return nil
	}
	m.ui.List.Collapsed[r.ID] = !m.ui.List.Collapsed[r.ID]
	return m.relist()
}

// toggleFoldAll collapses every epic of the current view, or expands them
// all when none is expanded. A selected task whose epic folds away hands the
// selection to its epic header.
func (m *Model) toggleFoldAll() tea.Cmd {
	groups := m.snap.Groups
	if m.ui.List.View == ViewAll {
		groups = m.snap.Browse
	}
	collapse := false
	for _, g := range groups {
		if g.Epic != nil && !m.ui.List.Collapsed[g.Epic.ID] {
			collapse = true
		}
	}
	for _, g := range groups {
		if g.Epic != nil {
			m.ui.List.Collapsed[g.Epic.ID] = collapse
		}
	}
	if r := m.selected(); collapse && r != nil && r.Kind == RowTask && r.Group != nil && r.Group.Epic != nil {
		m.selKey = "epic:" + r.Group.Epic.ID
	}
	return m.relist()
}

func (m *Model) startFilter() tea.Cmd {
	m.input = textinput.New()
	m.input.Prompt = ""
	m.input.CharLimit = 200
	m.input.Cursor.SetMode(cursor.CursorStatic)
	m.input.SetValue(m.ui.List.Filter)
	m.input.CursorEnd()
	m.ui.Filtering = true
	cmd := m.input.Focus()
	m.ui.FilterInput = m.input.View()
	m.fixOffset()
	return cmd
}

// handleFilterKey edits the filter incrementally; enter keeps it, esc
// clears it.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEnter:
		m.ui.Filtering = false
		m.fixOffset()
		return m, nil
	case tea.KeyEsc:
		m.ui.Filtering, m.ui.List.Filter = false, ""
		return m, m.relist()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.ui.FilterInput = m.input.View()
	m.ui.List.Filter = m.input.Value()
	return m, tea.Batch(cmd, m.relist())
}

func (m *Model) setStatus(text string, isErr bool) {
	m.ui.Status, m.ui.StatusErr = text, isErr
	m.fixOffset()
}

// shownDiff is the loaded diff of row r's review request, if it is open on
// screen; a branch-head diff is not what a verdict judges.
func (m *Model) shownDiff(r Row) *DiffView {
	d := m.ui.Diff
	if !m.ui.DiffOpen || d == nil || d.Failed || d.Key != r.Key() || d.SHA == "" {
		return nil
	}
	return d
}

// openDiff reuses a loaded diff only while it is of the row's current
// request commit; branch-head diffs and failed loads are always reloaded.
func (m *Model) openDiff(r Row) tea.Cmd {
	m.ui.DiffOpen, m.ui.DiffKey, m.ui.DiffOffset = true, r.Key(), 0
	if d := m.ui.Diff; d != nil && d.Key == r.Key() && !d.Failed && d.SHA != "" && d.SHA == r.RequestSHA() {
		return nil
	}
	m.ui.Diff = nil
	if m.diffLoading {
		return nil
	}
	m.diffLoading = true
	load := m.load.Diff
	return func() tea.Msg {
		d, err := load(r)
		if err != nil {
			d = DiffView{Key: r.Key(), Title: r.ID, Header: []string{"error: " + err.Error()}, Failed: true}
		}
		return diffMsg{diff: d}
	}
}

// openModal pins the verdict to the commit the human saw: the open diff's,
// else the row's request commit shown in the modal.
func (m *Model) openModal(decision string, r Row) tea.Cmd {
	sha := r.RequestSHA()
	if d := m.shownDiff(r); d != nil {
		sha = d.SHA
	}
	m.input = textinput.New()
	m.input.Prompt = ""
	m.input.CharLimit = 2000
	m.input.Cursor.SetMode(cursor.CursorStatic)
	m.ui.Modal = &Modal{Decision: decision, Subject: r.ID, SHA: sha, Step: StepComment}
	if decision == DecisionType {
		rt, _ := rowReviewType(&r)
		m.ui.Modal.Step, m.ui.Modal.Current, m.ui.Modal.Choice = StepPick, rt, rt
	}
	cmd := m.input.Focus()
	m.syncInput()
	return cmd
}

func (m Model) handlePickKey(md *Modal, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	i := 0
	for j, rt := range ReviewTypes {
		if rt == md.Choice {
			i = j
		}
	}
	switch msg.String() {
	case "up", "k":
		i = max(i-1, 0)
	case "down", "j":
		i = min(i+1, len(ReviewTypes)-1)
	case "enter":
		if md.Choice == md.Current {
			md.Error = "already " + string(md.Current) + "; pick another type"
			m.syncInput()
			return m, nil
		}
		return m.toConfirm(md)
	}
	md.Choice, md.Error = ReviewTypes[i], ""
	m.syncInput()
	return m, nil
}

// submit runs the confirmed modal's decision off the UI goroutine.
func (m Model) submit(md Modal, skipped string) tea.Cmd {
	load := m.load
	return func() tea.Msg {
		var report string
		var err error
		switch md.Decision {
		case DecisionPreApprove:
			report, err = changeReviewType(load, md.Subject, internal.ReviewNone, md.Comment, md.SHA)
		case DecisionType:
			report, err = changeReviewType(load, md.Subject, md.Choice, "", md.SHA)
		case DecisionRemovePreApproval:
			report, err = load.RemovePreApproval(md.Subject, md.Comment)
		case DecisionEdit:
			report, err = load.Edit(md.Subject, *md.EditOrig, *md.Edit)
		default:
			report, err = load.Verdict(md.Subject, md.Decision, md.Comment, md.SHA)
		}
		return verdictMsg{report: report, err: err, skipped: skipped}
	}
}

func (m *Model) syncInput() {
	if m.ui.Modal != nil {
		m.ui.Modal.Input = m.input.View()
	}
	m.fixOffset()
}

func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	md := *m.ui.Modal
	m.ui.Modal = &md
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.ui.Modal = nil
		m.setStatus("cancelled", false)
		return m, nil
	}
	if md.Step == StepPick {
		return m.handlePickKey(&md, msg)
	}
	if md.Step == StepConfirm {
		return m.handleConfirmKey(&md, msg)
	}
	if msg.Type == tea.KeyEnter {
		value := strings.TrimSpace(m.input.Value())
		if md.Decision == "request_changes" && value == "" {
			md.Error = "a comment is required to reject"
			m.syncInput()
			return m, nil
		}
		md.Comment, md.Error = value, ""
		m.input.Reset()
		return m.toConfirm(&md)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInput()
	return m, cmd
}

// toConfirm follows the comment or pick step: the y/N chooser, or the
// decision itself when its kind was set to "don't ask again" this session.
func (m Model) toConfirm(md *Modal) (tea.Model, tea.Cmd) {
	if key := skipKey(*md); m.skipConfirm[key] {
		return m.run(*md, decisionLabel(key))
	}
	md.Step, md.Option, md.Error = StepConfirm, ConfirmNo, ""
	m.syncInput()
	return m, nil
}

func (m Model) handleConfirmKey(md *Modal, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	option := md.Option
	switch msg.String() {
	case "left", "h", "shift+tab":
		md.Option = max(md.Option-1, 0)
		return m, nil
	case "right", "l", "tab":
		md.Option = min(md.Option+1, len(ConfirmOptions)-1)
		return m, nil
	case "y", "Y":
		option = ConfirmYes
	case "n", "N":
		option = ConfirmNo
	case "a", "A":
		option = ConfirmAlways
	case "enter":
	default:
		return m, nil
	}
	switch option {
	case ConfirmYes:
		return m.run(*md, "")
	case ConfirmAlways:
		m.skipConfirm[skipKey(*md)] = true
		m.ui.AutoConfirm = autoConfirmLabels(m.skipConfirm)
		return m.run(*md, "")
	}
	m.ui.Modal = nil
	m.setStatus("cancelled", false)
	return m, nil
}

// run closes the modal and records the decision; skipped names the kind
// whose confirmation was skipped, for the status line.
func (m Model) run(md Modal, skipped string) (tea.Model, tea.Cmd) {
	m.ui.Modal = nil
	status := "recording decision for " + md.Subject + "…"
	if skipped != "" {
		status += skippedNote(skipped)
	}
	m.setStatus(status, false)
	return m, m.submit(md, skipped)
}

func skippedNote(kind string) string {
	return " (confirmation skipped for " + kind + " this session)"
}

// skipKey scopes "don't ask again". Type changes are keyed by their target:
// skipping a switch to human must not also skip a switch to none, which can
// approve the commit under review.
func skipKey(md Modal) string {
	if md.Decision == DecisionType {
		return DecisionType + ":" + string(md.Choice)
	}
	return md.Decision
}

// decisionLabel names a modal decision kind (or skip key) for the user.
func decisionLabel(decision string) string {
	if choice, ok := strings.CutPrefix(decision, DecisionType+":"); ok {
		return "type → " + choice
	}
	switch decision {
	case "request_changes":
		return "reject"
	case DecisionPreApprove:
		return "pre-approve"
	case DecisionRemovePreApproval:
		return "remove pre-approval"
	case DecisionType:
		return "type change"
	}
	return decision
}

func autoConfirmLabels(skip map[string]bool) []string {
	var out []string
	kinds := []string{"approve", "request_changes", DecisionPreApprove, DecisionRemovePreApproval, DecisionEdit}
	for _, t := range []internal.ReviewType{internal.ReviewDefault, internal.ReviewHuman, internal.ReviewNone} {
		kinds = append(kinds, DecisionType+":"+string(t))
	}
	for _, d := range kinds {
		if skip[d] {
			out = append(out, decisionLabel(d))
		}
	}
	return out
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.fixOffset()
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.startRefresh(), m.tickCmd())
	case snapshotMsg:
		m.loading, m.ui.Refreshing = false, false
		if msg.err != nil {
			m.ui.Stale = msg.err.Error()
			return m, nil
		}
		m.ui.Stale = ""
		m.applySnapshot(msg.snap)
		return m, m.startDetail()
	case detailMsg:
		m.detailLoading = false
		if r := m.selected(); r != nil && r.Key() == msg.detail.Key {
			d := msg.detail
			m.ui.Detail = &d
		}
		if m.detailAgain {
			m.detailAgain = false
			return m, m.startDetail()
		}
		return m, nil
	case diffMsg:
		m.diffLoading = false
		d := msg.diff
		m.ui.Diff = &d
		if m.ui.DiffOpen && m.ui.DiffKey != d.Key {
			for _, r := range m.rows {
				if r.Key() == m.ui.DiffKey {
					return m, m.openDiff(r)
				}
			}
		}
		m.fixOffset()
		return m, nil
	case verdictMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		status := firstLine(msg.report)
		if msg.skipped != "" {
			status += skippedNote(msg.skipped)
		}
		m.setStatus(status, false)
		m.ui.DiffOpen, m.ui.Diff = false, nil
		return m, m.startRefresh()
	case editStartMsg:
		return m.runEditor(msg)
	case editDoneMsg:
		next, cmd := m.finishEdit(msg)
		return next, cmd
	case tea.KeyMsg:
		if m.ui.Filtering {
			return m.handleFilterKey(msg)
		}
		if m.ui.Modal != nil {
			return m.handleModalKey(msg)
		}
		if msg.String() != "ctrl+c" {
			m.setStatus("", false)
		}
		return m.handleKey(msg)
	}
	if m.ui.Modal != nil {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncInput()
		return m, cmd
	}
	return m, nil
}

// actionRow is the row actions apply to: the diff's while it is open.
func (m Model) actionRow() *Row {
	if m.ui.DiffOpen {
		for i := range m.rows {
			if m.rows[i].Key() == m.ui.DiffKey {
				return &m.rows[i]
			}
		}
		return nil
	}
	return m.selected()
}

func (m Model) disabledReason(key string) string {
	switch key {
	case "a", "r":
		if !m.opts.AllowActions {
			return "is disabled: " + m.opts.DenyReason
		}
		return "applies to a row with an open review request"
	case "e":
		if !m.opts.AllowActions {
			return "is disabled: " + m.opts.DenyReason
		}
		return "applies to a task row or epic header"
	case "p", "t":
		if !m.opts.AllowActions {
			return "is disabled: " + m.opts.DenyReason
		}
		if row := m.actionRow(); key == "p" && pastGate(row) {
			return fmt.Sprintf("cannot remove the pre-approval: %s is %s, already past the review gate", row.ID, row.Task.State)
		}
		return "applies to a task row or epic header that is not merged or closed"
	case "x":
		return "applies to an open detail or diff pane with a description or review comments"
	}
	return "applies to a task or epic row"
}

func (m Model) runAction(key string) (tea.Model, tea.Cmd) {
	ctx := m.context()
	for _, a := range m.ui.Actions {
		if a.Key != key {
			continue
		}
		if !a.enabled(ctx) {
			m.setStatus(a.Label+" "+m.disabledReason(key), true)
			return m, nil
		}
		if key == "x" {
			m.ui.Expanded = !m.ui.Expanded
			m.ui.DetailOffset = 0
			m.fixOffset()
			return m, nil
		}
		if a.Start == nil {
			return m, nil
		}
		cmd := a.Start(&m, *ctx.Row)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "a", "r", "d", "x", "p", "t", "e":
		if k == "d" && m.ui.DiffOpen {
			return m, nil
		}
		return m.runAction(k)
	case "f":
		return m, m.startRefresh()
	}
	if m.ui.DiffOpen {
		return m.handleDiffKey(k)
	}
	switch k {
	case "v":
		return m, m.toggleView()
	case "/":
		return m, m.startFilter()
	case " ":
		return m, m.toggleFold()
	case "c":
		return m, m.toggleFoldAll()
	case "up", "k":
		return m, m.move(-1)
	case "down", "j":
		return m, m.move(1)
	case "home", "g":
		return m, m.move(-len(m.rows))
	case "end", "G":
		return m, m.move(len(m.rows))
	case "enter", "tab":
		m.ui.DetailOpen = !m.ui.DetailOpen
		m.ui.DetailOffset = 0
		m.fixOffset()
		return m, m.startDetail()
	case "pgup", "pgdown":
		if m.ui.DetailOpen {
			page := DetailPage(m.snap, m.ui, m.height)
			if k == "pgup" {
				page = -page
			}
			m.ui.DetailOffset += page
			m.fixOffset()
		}
	}
	return m, nil
}

func (m Model) handleDiffKey(k string) (tea.Model, tea.Cmd) {
	page := DiffPage(m.snap, m.ui, m.height)
	switch k {
	case "esc":
		m.ui.DiffOpen = false
		m.fixOffset()
		return m, nil
	case "up", "k":
		m.ui.DiffOffset--
	case "down", "j":
		m.ui.DiffOffset++
	case "pgup", "b":
		m.ui.DiffOffset -= page
	case "pgdown", " ":
		m.ui.DiffOffset += page
	case "home", "g":
		m.ui.DiffOffset = 0
	case "end", "G":
		m.ui.DiffOffset = 1 << 30
	}
	m.fixOffset()
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	return Render(m.snap, m.ui, m.width, m.height)
}
