package dashboard

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marqueewinq/harness/internal"
)

// browseSnapshot is sampleSnapshot with an "all" view: the in-flight tasks
// plus a planned task, an old merged one, and an epic without tasks.
func browseSnapshot() Snapshot {
	s := sampleSnapshot()
	epic := s.Groups[0].Epic
	empty := &internal.Epic{ID: "epic-003", ProjectID: "proj-001", Title: "Someday", Status: internal.EpicStatusOpen}
	inEpic := append(append([]TaskRow{}, s.Groups[0].Tasks...),
		TaskRow{ID: "task-040", Title: "Planned follow-up", State: internal.StatePlanned, Age: 72 * time.Hour, Approval: "default · not started yet"},
	)
	noEpic := append([]TaskRow{
		{ID: "task-010", Title: "Old merged work", State: internal.StateMerged, Age: 30 * 24 * time.Hour},
	}, s.Groups[1].Tasks...)
	s.Browse = []EpicGroup{
		{Project: s.Groups[0].Project, Epic: epic, Branch: "epic/planning", Done: 3, Total: 8, Tasks: inEpic},
		{Project: s.Groups[0].Project, Epic: empty},
		{Tasks: noEpic},
	}
	return s
}

func lineWith(t *testing.T, view, id string) string {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		if strings.Contains(l, id) {
			return l
		}
	}
	return ""
}

func TestSnapshotBrowseHasEverything(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	proj, _ := app.DB.CreateProject("P", "")
	empty, _ := app.DB.CreateEpic("Empty", "", proj.ID, "", internal.ReviewSetting{})
	epic, _ := app.DB.CreateEpic("Busy", "", proj.ID, "", internal.ReviewSetting{})
	planned := seedTask(t, app, "planned", epic.ID, internal.StatePlanned)
	merged := seedTask(t, app, "merged", epic.ID, internal.StateMerged)
	closed := seedTask(t, app, "closed", "", internal.StateClosed)
	active := seedTask(t, app, "active", "", internal.StateInProgress)

	s := build(t, app, time.Now().Add(30*24*time.Hour), false)
	if got := rowIDs(s.Recent); len(got) != 0 {
		t.Fatalf("setup: recent = %v", got)
	}
	if len(s.Browse) != 3 || s.Browse[0].Epic == nil || s.Browse[0].Epic.ID != empty.ID || len(s.Browse[0].Tasks) != 0 ||
		s.Browse[1].Epic == nil || s.Browse[1].Epic.ID != epic.ID || s.Browse[2].Epic != nil {
		t.Fatalf("browse groups = %+v", s.Browse)
	}
	if got, want := rowIDs(s.Browse), []string{planned.ID, merged.ID, closed.ID, active.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("browse tasks = %v, want %v", got, want)
	}
	if contains(rowIDs(s.Groups), planned.ID) {
		t.Errorf("active groups gained the planned task")
	}
}

func TestViewToggle(t *testing.T) {
	f := &fakeLoader{snap: browseSnapshot()}
	m := selectRow(t, started(t, f), rowTask030)
	active := m.View()
	if strings.Contains(active, "task-040") || strings.Contains(active, "task-010") || strings.Contains(active, "epic-003") {
		t.Errorf("active view shows planned/old/empty:\n%s", active)
	}
	m = press(t, m, "v")
	all := m.View()
	for _, want := range []string{"view: all", "ALL TASKS", "task-040", "task-010", "epic-003 Someday", "No epic"} {
		if !strings.Contains(all, want) {
			t.Errorf("all view lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "RECENT") {
		t.Errorf("all view kept the recent section:\n%s", all)
	}
	if r := m.selected(); r == nil || r.ID != "task-030" {
		t.Errorf("selection lost across the view switch: %+v", r)
	}
	if !strings.Contains(lineWith(t, all, "task-030"), "›") {
		t.Errorf("cursor not on task-030:\n%s", all)
	}
	m = press(t, m, "v")
	if strings.Contains(m.View(), "ALL TASKS") || m.selected().ID != "task-030" {
		t.Errorf("v did not return to the active view with the selection kept")
	}
}

func TestActionsInAllView(t *testing.T) {
	s := browseSnapshot()
	rows := RowsFor(s, ListOptions{View: ViewAll})
	var merged, planned, emptyEpic *Row
	for i := range rows {
		switch rows[i].ID {
		case "task-010":
			merged = &rows[i]
		case "task-040":
			planned = &rows[i]
		case "epic-003":
			emptyEpic = &rows[i]
		}
	}
	if merged == nil || planned == nil || emptyEpic == nil {
		t.Fatalf("rows = %+v", rows)
	}
	enabled := func(r *Row, key string) bool {
		for _, a := range DefaultActions(true) {
			if a.Key == key {
				return a.enabled(Context{Row: r})
			}
		}
		t.Fatalf("no action %s", key)
		return false
	}
	for _, tc := range []struct {
		row  *Row
		key  string
		want bool
	}{
		{merged, "e", true}, {merged, "d", true}, {merged, "enter", true}, {merged, "p", false}, {merged, "t", false}, {merged, "a", false},
		{planned, "e", true}, {planned, "p", true}, {planned, "t", true},
		{emptyEpic, "e", true}, {emptyEpic, "d", true}, {emptyEpic, "p", true}, {emptyEpic, " ", true},
	} {
		if got := enabled(tc.row, tc.key); got != tc.want {
			t.Errorf("%s on %s: enabled=%v, want %v", tc.key, tc.row.ID, got, tc.want)
		}
	}
}

func TestFilter(t *testing.T) {
	f := &fakeLoader{snap: browseSnapshot()}
	m := started(t, f)
	m = press(t, m, "/")
	if !m.ui.Filtering || !strings.Contains(m.View(), "enter keep filter") {
		t.Fatalf("filter editor not open:\n%s", m.View())
	}
	m = typeText(t, m, "BROKEN")
	if got := rowIDsOf(m.rows); !reflect.DeepEqual(got, []string{"task-031"}) {
		t.Errorf("filtered rows = %v", got)
	}
	if !strings.Contains(strings.Split(m.View(), "\n")[0], "filter: BROKEN") {
		t.Errorf("header lacks the filter: %q", strings.Split(m.View(), "\n")[0])
	}
	m = press(t, m, "enter")
	if m.ui.Filtering || m.ui.List.Filter != "BROKEN" || m.selected().ID != "task-031" {
		t.Errorf("enter: filtering=%v filter=%q sel=%+v", m.ui.Filtering, m.ui.List.Filter, m.selected())
	}
	// j/k navigate the filtered list; v keeps the filter.
	m = press(t, m, "v")
	if got := rowIDsOf(m.rows); !reflect.DeepEqual(got, []string{"task-031"}) {
		t.Errorf("filter not applied to the all view: %v", got)
	}
	m = press(t, m, "/", "esc")
	if m.ui.List.Filter != "" || m.ui.Filtering || len(m.rows) < 8 {
		t.Errorf("esc did not clear: filter=%q rows=%d", m.ui.List.Filter, len(m.rows))
	}

	for filter, want := range map[string][]string{
		"planning": {"epic-002", "epic-002", "task-025", "task-027", "task-028", "task-040"},
		// A matching task keeps its epic header, so it stays reachable.
		"merge_failed": {"epic-002", "task-027"},
		"planned":      {"epic-002", "task-040"},
		"someday":      {"epic-003"},
	} {
		got := rowIDsOf(RowsFor(browseSnapshot(), ListOptions{View: ViewAll, Filter: filter}))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("filter %q = %v, want %v", filter, got, want)
		}
	}
	typed := sampleSnapshot()
	typed.Groups[0].Tasks[2].ReviewType = internal.ReviewNone
	typed.Groups[1].Tasks[0].ReviewType = internal.ReviewNone
	if got := rowIDsOf(RowsFor(typed, ListOptions{Filter: "none"})); !reflect.DeepEqual(got, []string{"epic-002", "task-028", "task-030"}) {
		t.Errorf("filter by review type: %v", got)
	}

	m = press(t, m, "/")
	m = typeText(t, m, "zzz")
	if v := m.View(); !strings.Contains(v, "no matches") || len(m.rows) != 0 {
		t.Errorf("no matches:\n%s", v)
	}
}

func rowIDsOf(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestCollapse(t *testing.T) {
	f := &fakeLoader{snap: browseSnapshot()}
	m := selectRow(t, started(t, f), rowEpicHeader)
	m = press(t, m, " ")
	v := m.View()
	if header := lineWith(t, v, "epic-002 Planning layer"); !strings.Contains(header, "▸") || !strings.Contains(header, "3 tasks") {
		t.Errorf("collapsed header = %q", header)
	}
	if strings.Contains(v, "task-025") || m.selected().ID != "epic-002" {
		t.Errorf("tasks still shown or selection moved:\n%s", v)
	}
	m = press(t, m, "f")
	if strings.Contains(m.View(), "task-025") {
		t.Errorf("collapse lost on refresh")
	}
	// task-040 is in epic-002 too: folded in the all view as well.
	m = press(t, m, "v")
	if strings.Contains(m.View(), "task-040") || !strings.Contains(lineWith(t, m.View(), "epic-002 Planning layer"), "4 tasks") {
		t.Errorf("collapse not kept in the all view:\n%s", m.View())
	}
	m = press(t, m, "v", " ")
	if !strings.Contains(m.View(), "task-025") || !strings.Contains(lineWith(t, m.View(), "epic-002 Planning layer"), "▾") {
		t.Errorf("space did not expand:\n%s", m.View())
	}

	// c folds every epic; a selected task hands the selection to its header.
	m = selectRow(t, m, rowTask025)
	m = press(t, m, "c")
	if m.selected().ID != "epic-002" || strings.Contains(m.View(), "task-025") || !strings.Contains(m.View(), "task-030") {
		t.Errorf("collapse all: sel=%+v\n%s", m.selected(), m.View())
	}
	m = press(t, m, "c")
	if !strings.Contains(m.View(), "task-025") {
		t.Errorf("second c did not expand all")
	}
	m = press(t, selectRow(t, m, rowTask030), " ")
	if !m.ui.StatusErr || !strings.Contains(m.ui.Status, "epic header") {
		t.Errorf("space on a task: %q", m.ui.Status)
	}
}

func TestViewSwitchHandsHiddenTaskToItsHeader(t *testing.T) {
	f := &fakeLoader{snap: browseSnapshot()}
	m := started(t, f)
	m = press(t, m, "v")
	m = selectRow(t, m, 6) // task-040 in the all view
	if m.selected().ID != "task-040" {
		t.Fatalf("selected %+v", m.selected())
	}
	m.ui.List.Collapsed["epic-002"] = true
	m = press(t, m, "v", "v")
	if m.selected().ID != "epic-002" || m.selected().Kind != RowEpic {
		t.Errorf("selection = %+v, want the epic-002 header", m.selected())
	}
}

func TestFilterInFoldedEpics(t *testing.T) {
	o := ListOptions{View: ViewAll, Folding: true, Filter: "planned", Collapsed: map[string]bool{"epic-002": true}}
	lines, rows := listLines(NewStyles(false), browseSnapshot(), o)
	if got := rowIDsOf(rows); !reflect.DeepEqual(got, []string{"epic-002", "task-040"}) {
		t.Errorf("a match inside a folded epic is hidden: %v", got)
	}
	var header string
	for _, l := range lines {
		if strings.Contains(l.text, "epic-002 Planning layer") {
			header = l.text
		}
	}
	if !strings.Contains(header, "· 1/4 tasks") || !strings.Contains(header, "▾") {
		t.Errorf("header = %q, want ▾ and 1/4 tasks", header)
	}
	o.Filter = "someday"
	if _, rows := listLines(NewStyles(false), browseSnapshot(), o); !reflect.DeepEqual(rowIDsOf(rows), []string{"epic-003"}) {
		t.Errorf("epic-only match: %v", rowIDsOf(rows))
	}
}

func TestFilterMatchesReviewRowType(t *testing.T) {
	s := sampleSnapshot()
	s.Reviews[1].ReviewType = internal.ReviewHuman
	if got := rowIDsOf(RowsFor(s, ListOptions{Filter: "HUMAN"})); len(got) == 0 || got[0] != "epic-002" {
		t.Errorf("review row not matched by review type: %v", got)
	}
}

func TestHelpOverlay(t *testing.T) {
	m := started(t, &fakeLoader{snap: browseSnapshot()})
	m = press(t, m, "?")
	v := m.View()
	for _, want := range []string{"── keys", "space", "fold", "collapse", "refresh", "expand", "enter", "details", "pgup pgdn"} {
		if !strings.Contains(v, want) {
			t.Errorf("help lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "AWAITING REVIEW") {
		t.Errorf("help did not replace the list:\n%s", v)
	}
	if next := press(t, m, "j"); !next.ui.Help || next.ui.Selected != m.ui.Selected {
		t.Errorf("keys leaked through the help")
	}
	for _, k := range []string{"esc", "?"} {
		if press(t, m, k).ui.Help {
			t.Errorf("%s did not close the help", k)
		}
	}
}

func TestKeyBarFits80Columns(t *testing.T) {
	none := sampleSnapshot()
	none.Groups[1].Tasks[0].ReviewType = internal.ReviewNone
	for _, s := range []Snapshot{sampleSnapshot(), none} {
		for _, sel := range []int{0, rowEpicHeader, rowTask030} {
			out := Render(s, UIState{Selected: sel, Actions: DefaultActions(true)}, 80, 30)
			bar := out[strings.LastIndex(out, "\n")+1:]
			if strings.Contains(bar, "…") || !strings.HasSuffix(bar, "quit") || !strings.Contains(bar, "? help") {
				t.Errorf("bar at 80 columns = %q", bar)
			}
		}
	}
}

func TestRenderBrowseGoldens(t *testing.T) {
	collapsed := map[string]bool{"epic-002": true}
	for name, ui := range map[string]UIState{
		"tui_all_view.golden":  {Selected: 6, List: ListOptions{View: ViewAll, Folding: true, Collapsed: map[string]bool{}}},
		"tui_filtered.golden":  {Selected: 0, List: ListOptions{View: ViewAll, Folding: true, Filter: "planning", Collapsed: map[string]bool{}}, Filtering: true, FilterInput: "planning"},
		"tui_collapsed.golden": {Selected: 2, List: ListOptions{View: ViewActive, Folding: true, Collapsed: collapsed}},
		"tui_help.golden":      {Selected: 2, Help: true, List: ListOptions{Folding: true, Collapsed: map[string]bool{}}},
	} {
		ui.Actions = DefaultActions(true)
		out := Render(browseSnapshot(), ui, 110, 34)
		renderLines(t, out, 34)
		checkGolden(t, name, out+"\n")
	}
}
