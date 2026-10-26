package dashboard

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/marqueewinq/harness/internal"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/")

var fixedNow = time.Date(2026, 10, 26, 16, 42, 7, 0, time.UTC)

func sampleSnapshot() Snapshot {
	proj := &internal.Project{ID: "proj-001", Title: "Platform"}
	epic := &internal.Epic{ID: "epic-002", ProjectID: "proj-001", Title: "Planning layer", Branch: "epic/planning"}
	return Snapshot{
		Repo: "harness", MainBranch: "main", MainHead: "39829b9", Now: fixedNow,
		Reviews: []ReviewRow{
			{SubjectID: "task-026", Title: "Reviewer supervision", CommitSHA: "8174267712345", Age: 12 * time.Minute, Reviewer: internal.ReviewerOrphaned, Attention: "reviewer orphaned"},
			{SubjectID: "epic-002", IsEpic: true, Title: "Planning layer", CommitSHA: "5d1c09aa999", Age: 3 * time.Minute, Reviewer: ReviewerHuman, Attention: "awaiting human review"},
		},
		Groups: []EpicGroup{
			{Project: proj, Epic: epic, Branch: "epic/planning", Done: 3, Total: 7, EpicReview: "review requested", Tasks: []TaskRow{
				{ID: "task-025", Title: "Reviewer run records", State: internal.StateChecksRunning, Age: 3 * time.Minute,
					Checks: []CheckStatus{{Name: "test", Result: CheckRunning, Elapsed: 41 * time.Second, ElapsedKnown: true}, {Name: "lint", Result: CheckRunning}}},
				{ID: "task-027", Title: "Sync epic branch", State: internal.StateMergeFailed, Age: time.Hour,
					Checks: []CheckStatus{{Name: "lint", Result: CheckPass}, {Name: "test", Result: CheckPass}}, GitOp: "rebase", Attention: "unfinished rebase"},
				{ID: "task-028", Title: "Dashboard snapshot", State: internal.StateQueued, Age: 2 * time.Minute, QueuePos: 1,
					Checks: []CheckStatus{{Name: "lint", Result: CheckPass}, {Name: "test", Result: CheckPass}}},
			}},
			{Tasks: []TaskRow{
				{ID: "task-030", Title: "A very long task title that will certainly not fit into the configured width", State: internal.StateInProgress, Age: 5 * time.Minute},
				{ID: "task-031", Title: "Broken", State: internal.StateChecksFailed, Age: 26 * time.Hour,
					Checks: []CheckStatus{{Name: "lint", Result: CheckFail}, {Name: "test", Result: CheckSkip}}, Attention: "checks failed"},
			}},
		},
		Queue: []QueueRow{
			{Position: 1, TaskID: "task-028", PID: 5150, HeartbeatAge: 4 * time.Second},
			{Position: 2, TaskID: "task-029", PID: 6001, HeartbeatAge: 3 * time.Minute, Stale: true},
		},
		Recent: []EpicGroup{
			{Project: proj, Epic: epic, Branch: "epic/planning", Done: 3, Total: 7, Tasks: []TaskRow{
				{ID: "task-024", Title: "Terminal verdicts", State: internal.StateMerged, Age: 18 * time.Minute},
			}},
		},
		Warnings: []string{"reading committed review config: boom"},
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch (go test ./internal/dashboard -update to accept)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestRenderPlainGolden(t *testing.T) {
	checkGolden(t, "once.golden", RenderPlain(sampleSnapshot(), 100, false))
}

func TestRenderPlainEmptyGolden(t *testing.T) {
	checkGolden(t, "empty.golden", RenderPlain(Snapshot{Repo: "harness", MainBranch: "main", MainHead: "39829b9", Now: fixedNow}, 100, false))
}

func TestRenderPlainAllGolden(t *testing.T) {
	s := sampleSnapshot()
	s.All = true
	s.Reviews, s.Queue, s.Warnings = nil, nil, nil
	checkGolden(t, "all.golden", RenderPlain(s, 100, false))
}

func TestRenderPlainWidth(t *testing.T) {
	for _, color := range []bool{false, true} {
		for _, line := range strings.Split(RenderPlain(sampleSnapshot(), 60, color), "\n") {
			if w := lipgloss.Width(line); w > 60 {
				t.Errorf("color=%v line width %d > 60: %q", color, w, line)
			}
		}
	}
	unlimited := RenderPlain(sampleSnapshot(), 0, false)
	if !strings.Contains(unlimited, "certainly not fit into the configured width") {
		t.Errorf("width 0 truncated output:\n%s", unlimited)
	}
}

func TestRenderPlainColor(t *testing.T) {
	if out := RenderPlain(sampleSnapshot(), 100, false); strings.Contains(out, "\x1b") {
		t.Errorf("color off output has escapes:\n%q", out)
	}
	if out := RenderPlain(sampleSnapshot(), 100, true); !strings.Contains(out, "\x1b[") {
		t.Errorf("color on output has no ANSI escapes:\n%q", out)
	}
}

func TestUseColor(t *testing.T) {
	if UseColor(false, "") {
		t.Error("non-terminal must not use color")
	}
	if UseColor(true, "1") {
		t.Error("NO_COLOR must disable color")
	}
	if !UseColor(true, "") {
		t.Error("terminal without NO_COLOR should use color")
	}
}
