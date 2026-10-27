package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marqueewinq/harness/internal"
)

func seedMergeFailure(t *testing.T, app *internal.App, reason, log string) *internal.Task {
	t.Helper()
	task := seedTask(t, app, "conflicted "+reason, "", internal.StateMerging)
	if err := app.DB.UpdateTaskStateNote(task.ID, internal.StateMergeFailed, reason); err != nil {
		t.Fatal(err)
	}
	path := internal.MergeLogPath(app.RepoRoot, task.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(log), 0644); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestMergeFailureAttentionShowsReason(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	short := seedMergeFailure(t, app, "checks failed after rebase: test", "")
	long := seedMergeFailure(t, app, "prepare failed (rebase+ff onto main): git rebase main: exit status 1 (conflict in a.txt, b.txt, c.txt)", "")
	groups := build(t, app, time.Now(), false).Groups

	if got := findRow(t, groups, short.ID).Attention; got != "merge failed: checks failed after rebase: test" {
		t.Errorf("attention = %q", got)
	}
	got := findRow(t, groups, long.ID).Attention
	if !strings.HasPrefix(got, "merge failed: prepare failed") || !strings.HasSuffix(got, "…") || len([]rune(got)) > len("merge failed: ")+mergeReasonWidth {
		t.Errorf("long attention not truncated: %q", got)
	}
}

func TestMergeFailureDetailShowsReasonAndLogTail(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	var log strings.Builder
	for i := 1; i <= 30; i++ {
		log.WriteString("line " + string(rune('a'+i%26)) + "\n")
	}
	log.WriteString("CONFLICT (content): Merge conflict in a.txt\n")
	task := seedMergeFailure(t, app, "prepare failed (conflict in a.txt)", log.String())

	d, err := BuildDetail(app, Row{Kind: RowTask, ID: task.ID}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(d.Lines, "\n")
	for _, want := range []string{"Merge failed at ", "prepare failed (conflict in a.txt)", "Merge log " + internal.MergeLogPath(app.RepoRoot, task.ID), "CONFLICT (content): Merge conflict in a.txt"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail lacks %q:\n%s", want, text)
		}
	}
	if n := strings.Count(text, "\n  line "); n != logTailLines-1 {
		t.Errorf("log tail has %d earlier lines, want %d:\n%s", n, logTailLines-1, text)
	}
}
