package dashboard

import (
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
	bar := keyBar(st, DefaultActions(), &sel)
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
	if noRow := keyBar(st, DefaultActions(), nil); !strings.Contains(noRow, st.Dim.Render("enter details")) {
		t.Errorf("details not dimmed without a row: %q", noRow)
	}
	if plain := keyBar(NewStyles(false), DefaultActions(), &sel); plain != " approve  reject  diff  enter details  refresh  quit  ↑↓ select" {
		t.Errorf("plain key bar = %q", plain)
	}
}
