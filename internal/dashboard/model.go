package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/marqueewinq/harness/internal"
)

// Loader runs off the UI goroutine, inside tea.Cmds.
type Loader struct {
	Snapshot func(now time.Time) (Snapshot, error)
	Detail   func(row Row, now time.Time) (Detail, error)
}

func AppLoader(app *internal.App, all bool) Loader {
	return Loader{
		Snapshot: func(now time.Time) (Snapshot, error) { return BuildSnapshot(app, now, all) },
		Detail:   func(row Row, now time.Time) (Detail, error) { return BuildDetail(app, row, now) },
	}
}

type snapshotMsg struct {
	snap Snapshot
	err  error
}

type detailMsg struct {
	detail Detail
}

type tickMsg time.Time

type Model struct {
	load     Loader
	interval time.Duration
	now      func() time.Time

	snap    Snapshot
	ui      UIState
	rows    []Row
	selKey  string
	loading bool
	width   int
	height  int
}

func NewModel(load Loader, interval time.Duration, color bool) Model {
	return Model{
		load:     load,
		interval: interval,
		now:      time.Now,
		loading:  true,
		ui:       UIState{Color: color, Actions: DefaultActions(), Refreshing: true},
	}
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
	return tea.Tick(m.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) detailCmd() tea.Cmd {
	row := m.selected()
	if row == nil || !m.ui.DetailOpen {
		return nil
	}
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
	}
}

func (m *Model) move(delta int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	next := m.ui.Selected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.rows) {
		next = len(m.rows) - 1
	}
	if next == m.ui.Selected {
		return nil
	}
	m.ui.Selected = next
	m.selKey = m.rows[next].Key()
	m.fixOffset()
	return m.detailCmd()
}

// applySnapshot keeps the selection on the same row when it still exists.
func (m *Model) applySnapshot(s Snapshot) {
	m.snap = s
	m.rows = Rows(s)
	sel := -1
	for i, r := range m.rows {
		if r.Key() == m.selKey {
			sel = i
		}
	}
	if sel < 0 {
		sel = m.ui.Selected
		if sel >= len(m.rows) {
			sel = len(m.rows) - 1
		}
		if sel < 0 {
			sel = 0
		}
	}
	m.ui.Selected = sel
	if r := m.selected(); r != nil {
		m.selKey = r.Key()
	}
	m.fixOffset()
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
		return m, m.detailCmd()
	case detailMsg:
		if r := m.selected(); r != nil && r.Key() == msg.detail.Key {
			d := msg.detail
			m.ui.Detail = &d
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "f":
		return m, m.startRefresh()
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
		m.fixOffset()
		return m, m.detailCmd()
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	return Render(m.snap, m.ui, m.width, m.height)
}
