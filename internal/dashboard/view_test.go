package dashboard

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func sampleDetail() *Detail {
	return &Detail{Key: "task:task-027", Title: "task-027 Sync epic branch", Lines: []string{
		"State: MERGE_FAILED for 1h · branch sync-epic · base epic/planning",
		"Worktree: /repo/.worktrees/sync-epic",
		"History:",
		"  10-26 15:40:00  (created) → IN_PROGRESS",
		"  10-26 15:42:07  QUEUED → MERGE_FAILED",
		"Checks (commit 81742677):",
		"  lint         PASS  1.2s",
		"  test         PASS  31.9s",
		"Review request #3 at 81742677 · 1h ago · closed: verdict",
		"Latest verdict: approved at 81742677 by terminal:me",
	}}
}

func renderLines(t *testing.T, out string, height int) []string {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Errorf("rendered %d lines, want %d", len(lines), height)
	}
	return lines
}

func TestRenderListGolden(t *testing.T) {
	out := Render(sampleSnapshot(), UIState{Selected: 3}, 100, 30)
	renderLines(t, out, 30)
	checkGolden(t, "tui_list.golden", out+"\n")
}

func TestRenderDetailGolden(t *testing.T) {
	ui := UIState{Selected: 3, DetailOpen: true, Detail: sampleDetail()}
	out := Render(sampleSnapshot(), ui, 100, 30)
	renderLines(t, out, 30)
	checkGolden(t, "tui_detail.golden", out+"\n")
}

func TestRenderDetailFullHeightOnSmallTerminal(t *testing.T) {
	ui := UIState{Selected: 3, DetailOpen: true, Detail: sampleDetail()}
	out := Render(sampleSnapshot(), ui, 80, 14)
	renderLines(t, out, 14)
	checkGolden(t, "tui_detail_small.golden", out+"\n")
}

func TestRenderDetailLoading(t *testing.T) {
	ui := UIState{Selected: 0, DetailOpen: true, Detail: sampleDetail()}
	out := Render(sampleSnapshot(), ui, 100, 30)
	if !strings.Contains(out, "── task-026 ") || !strings.Contains(out, "loading…") {
		t.Errorf("detail for another row shown instead of loading:\n%s", out)
	}
}

func TestRenderScrolledGolden(t *testing.T) {
	s := sampleSnapshot()
	last := len(Rows(s)) - 1
	ui := UIState{Selected: last}
	ui.Offset = ListOffset(s, ui, 12)
	out := Render(s, ui, 100, 12)
	lines := renderLines(t, out, 12)
	if !strings.Contains(out, "›  task-024") {
		t.Errorf("selected last row not visible:\n%s", out)
	}
	if strings.Contains(out, "AWAITING REVIEW") {
		t.Errorf("list not scrolled:\n%s", out)
	}
	if !strings.Contains(lines[len(lines)-1], "approve") {
		t.Errorf("key bar missing at bottom:\n%s", out)
	}
	checkGolden(t, "tui_scrolled.golden", out+"\n")
}

func sampleDiff() *DiffView {
	d := &DiffView{
		Key:   "review:task-026",
		Title: "task-026 Reviewer supervision · epic/planning...81742677",
		Header: []string{
			"Review of task-026 at 81742677 (requested 2026-10-26 16:30), base epic/planning",
			"Checks on 81742677: lint PASS, test PASS",
			"Previous comments:",
			"  [request_changes at 5d1c09aa] handle SIGHUP",
			"",
		},
		Diff: []string{
			"diff --git a/run.go b/run.go",
			"--- a/run.go",
			"+++ b/run.go",
			"@@ -1,3 +1,4 @@",
			" package internal",
			"-var old = 1",
			"+var timeout = 30",
			"+var heartbeat = 10",
		},
	}
	for i := 0; i < 20; i++ {
		d.Diff = append(d.Diff, fmt.Sprintf(" context %d", i))
	}
	return d
}

func TestRenderDiffGolden(t *testing.T) {
	ui := UIState{Selected: 0, DiffOpen: true, DiffKey: "review:task-026", Diff: sampleDiff(), DiffOffset: 2, Actions: DefaultActions(true)}
	out := Render(sampleSnapshot(), ui, 100, 20)
	renderLines(t, out, 20)
	checkGolden(t, "tui_diff.golden", out+"\n")
}

func TestRenderApproveModalGolden(t *testing.T) {
	ui := UIState{Selected: 0, Actions: DefaultActions(true), Modal: &Modal{
		Decision: "approve", Subject: "task-026", SHA: "8174267712345", Step: StepConfirm,
		Comment: "looks good", Input: "task-062", Error: `typed "task-062"; type task-026 exactly to confirm`,
	}}
	out := Render(sampleSnapshot(), ui, 100, 24)
	renderLines(t, out, 24)
	checkGolden(t, "tui_approve.golden", out+"\n")
}

func TestRenderRejectModalGolden(t *testing.T) {
	ui := UIState{Selected: 1, Actions: DefaultActions(true), Status: "previous action cancelled", Modal: &Modal{
		Decision: "request_changes", Subject: "epic-002", SHA: "5d1c09aa999", Step: StepComment,
		Error: "a comment is required to reject",
	}}
	out := Render(sampleSnapshot(), ui, 100, 24)
	renderLines(t, out, 24)
	checkGolden(t, "tui_reject.golden", out+"\n")
}

func TestModalKeyBarColor(t *testing.T) {
	st := NewStyles(true)
	ui := UIState{Color: true, Modal: &Modal{Decision: "approve", Subject: "task-026"}}
	out := Render(sampleSnapshot(), ui, 100, 24)
	bar := out[strings.LastIndex(out, "\n")+1:]
	if !strings.Contains(bar, st.Accent.Render("enter")+" "+st.Plain.Render("confirm")) || !strings.Contains(bar, st.Accent.Render("esc")) {
		t.Errorf("modal key bar = %q", bar)
	}
}

func TestReviewActionsEnabledOnlyOnReviewRowsWhenAllowed(t *testing.T) {
	st := NewStyles(true)
	rows := Rows(sampleSnapshot())
	review, task := rows[0], rows[2]
	on := keyBar(st, DefaultActions(true), &review)
	if !strings.Contains(on, st.Plain.Render("")+st.Accent.Render("a")+st.Plain.Render("pprove")) || !strings.Contains(on, st.Accent.Render("d")+st.Plain.Render("iff")) {
		t.Errorf("approve/diff not enabled on a review row: %q", on)
	}
	for _, bar := range []string{keyBar(st, DefaultActions(true), &task), keyBar(st, DefaultActions(false), &review)} {
		for _, label := range []string{"approve", "reject", "diff"} {
			if !strings.Contains(bar, st.Dim.Render(label)) {
				t.Errorf("%s not dimmed: %q", label, bar)
			}
		}
	}
}

func TestRenderTooSmallGolden(t *testing.T) {
	checkGolden(t, "tui_small.golden", Render(sampleSnapshot(), UIState{}, 59, 30)+"\n")
	if out := Render(sampleSnapshot(), UIState{}, 100, 11); !strings.Contains(out, "terminal too small") {
		t.Errorf("height 11: %q", out)
	}
}

func TestRenderStaleAndLoadingHeader(t *testing.T) {
	out := Render(sampleSnapshot(), UIState{Stale: "database is locked", Refreshing: true}, 100, 30)
	header := strings.Split(out, "\n")[0]
	if !strings.Contains(header, "stale: database is locked") || !strings.Contains(header, "⟳") || !strings.Contains(header, "main @ 39829b9") {
		t.Errorf("header = %q", header)
	}
	if !strings.Contains(out, "task-025") {
		t.Errorf("stale view dropped the last snapshot:\n%s", out)
	}
	if first := strings.Split(Render(Snapshot{}, UIState{}, 100, 30), "\n")[0]; !strings.Contains(first, "loading") {
		t.Errorf("empty snapshot header = %q", first)
	}
}

func TestRenderWidthBound(t *testing.T) {
	ui := UIState{Selected: 3, DetailOpen: true, Detail: sampleDetail(), Color: true}
	for _, line := range strings.Split(Render(sampleSnapshot(), ui, 60, 30), "\n") {
		if w := lipgloss.Width(line); w > 60 {
			t.Errorf("line width %d > 60: %q", w, line)
		}
	}
}

func TestKeyBarColor(t *testing.T) {
	st := NewStyles(true)
	sel := Rows(sampleSnapshot())[0]
	bar := keyBar(st, DefaultActions(false), &sel)
	refresh := st.Plain.Render("re") + st.Accent.Render("f") + st.Plain.Render("resh")
	if !strings.Contains(bar, refresh) {
		t.Errorf("refresh hotkey not accented inside its word: %q", bar)
	}
	if !strings.Contains(bar, st.Accent.Render("q")+st.Plain.Render("uit")) {
		t.Errorf("quit hotkey not accented: %q", bar)
	}
	for _, label := range []string{"approve", "reject", "diff"} {
		if !strings.Contains(bar, st.Dim.Render(label)) {
			t.Errorf("%s not dimmed: %q", label, bar)
		}
	}
	if !strings.Contains(bar, st.Accent.Render("enter")) {
		t.Errorf("enter hint not accented with a row selected: %q", bar)
	}
	if noRow := keyBar(st, DefaultActions(false), nil); !strings.Contains(noRow, st.Dim.Render("enter details")) {
		t.Errorf("details not dimmed without a row: %q", noRow)
	}
	if plain := keyBar(NewStyles(false), DefaultActions(false), &sel); plain != " approve  reject  diff  enter details  refresh  quit  ↑↓ select" {
		t.Errorf("plain key bar = %q", plain)
	}
}
