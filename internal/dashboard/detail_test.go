package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marqueewinq/harness/internal"
)

func TestBuildTaskDetail(t *testing.T) {
	t.Parallel()
	app := newRepo(t, agentConfig)
	task := seedTask(t, app, "detail me", "", internal.StateReviewRequested)
	start := time.Now().Add(-time.Minute)
	app.DB.RecordCheckRun(task.ID, "lint", 0, "", "", 1, start, start.Add(1200*time.Millisecond), "8174267712345")
	app.DB.RecordCheckRun(task.ID, "test", 2, "", "line1\nboom\n", 1, start, start.Add(3*time.Second), "8174267712345")
	_, reqID, err := app.DB.OpenReviewRequestID(task.ID, "", "8174267712345")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DB.StartReviewerRun(reqID, deadPID(t), 4242); err != nil {
		t.Fatal(err)
	}

	d, err := BuildDetail(app, Row{Kind: RowTask, ID: task.ID}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(d.Lines, "\n")
	for _, want := range []string{
		"State: REVIEW_REQUESTED", "History:", "CHECKS_PASSED → REVIEW_REQUESTED",
		"Checks (commit 81742677):", "lint         PASS  1.2s", "test         FAIL (exit 2)", "    boom",
		"Review request #", "open", "Reviewer run: orphaned", "4242",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("detail lacks %q:\n%s", want, text)
		}
	}
	if d.Key != "task:"+task.ID || !strings.Contains(d.Title, "detail me") {
		t.Errorf("key/title = %q %q", d.Key, d.Title)
	}
}

func TestBuildTaskDetailLiveLogAndQueue(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	running := seedTask(t, app, "running", "", internal.StateChecksRunning)
	dir := internal.CheckLogDir(app.RepoRoot, running.ID)
	os.MkdirAll(dir, 0755)
	var log strings.Builder
	for i := 0; i < 30; i++ {
		log.WriteString("progress\rline " + string(rune('a'+i%26)) + "\n")
	}
	log.WriteString("\x1b[31mlast red line\x1b[0m\n")
	os.WriteFile(filepath.Join(dir, "test.log"), []byte(log.String()), 0644)

	d, err := BuildDetail(app, Row{Kind: RowTask, ID: running.ID}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(d.Lines, "\n")
	if !strings.Contains(text, "Live log test.log:") || !strings.Contains(text, "  last red line") {
		t.Errorf("live log tail missing:\n%s", text)
	}
	if strings.Contains(text, "\x1b") || strings.Contains(text, "\r") || strings.Contains(text, "progress") {
		t.Errorf("log tail not sanitized:\n%q", text)
	}
	if n := strings.Count(text, "\n  line "); n != logTailLines-1 {
		t.Errorf("tail has %d plain lines, want %d", n, logTailLines-1)
	}

	queued := seedTask(t, app, "queued", "", internal.StateQueued)
	if _, err := app.DB.EnqueueTask(queued.ID, deadPID(t)); err != nil {
		t.Fatal(err)
	}
	d, err = BuildDetail(app, Row{Kind: RowTask, ID: queued.ID}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if text := strings.Join(d.Lines, "\n"); !strings.Contains(text, "Queue: #1") || !strings.Contains(text, "stale") || !strings.Contains(text, "Review: not requested") {
		t.Errorf("queue detail:\n%s", text)
	}
}

func TestBuildEpicDetail(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	proj, _ := app.DB.CreateProject("P", "")
	epic, _ := app.DB.CreateEpic("Planning layer", "", proj.ID, "epic/planning")
	seedTask(t, app, "done", epic.ID, internal.StateMerged)
	seedTask(t, app, "open", epic.ID, internal.StateInProgress)
	if _, err := app.DB.OpenReviewRequest("", epic.ID, "5d1c09aa999"); err != nil {
		t.Fatal(err)
	}
	row := Row{Kind: RowReview, ID: epic.ID, IsEpic: true}
	d, err := BuildDetail(app, row, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(d.Lines, "\n")
	for _, want := range []string{"branch epic/planning", "Tasks: 1/2 done", "Review request #", "5d1c09aa", "open"} {
		if !strings.Contains(text, want) {
			t.Errorf("epic detail lacks %q:\n%s", want, text)
		}
	}
	if d.Key != "review:"+epic.ID {
		t.Errorf("key = %q", d.Key)
	}
}
