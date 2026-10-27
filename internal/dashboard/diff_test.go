package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marqueewinq/harness/internal"
)

// reviewedTask seeds a REVIEW_REQUESTED task whose branch has one commit,
// with an open request at that commit.
func reviewedTask(t *testing.T, app *internal.App) (*internal.Task, string) {
	t.Helper()
	wt := filepath.Join(app.RepoRoot, ".worktrees", "feature")
	gitOut(t, app.RepoRoot, "worktree", "add", "-q", "-b", "feature", wt)
	os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("new feature\n"), 0644)
	gitOut(t, wt, "add", ".")
	gitOut(t, wt, "commit", "-q", "-m", "feature")
	sha := gitOut(t, wt, "rev-parse", "HEAD")
	task := seedTaskAt(t, app, "feature", "", internal.StateReviewRequested, wt)
	start := time.Now()
	app.DB.RecordCheckRun(task.ID, "lint", 0, "", "", 1, start, start, sha)
	app.DB.RecordCheckRun(task.ID, "test", 0, "", "", 1, start, start, sha)
	if _, err := app.DB.OpenReviewRequest(task.ID, "", sha); err != nil {
		t.Fatal(err)
	}
	return task, sha
}

func TestBuildDiffTask(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	task, sha := reviewedTask(t, app)
	// A commit after the request must not show: the diff is of the requested SHA.
	wt := filepath.Join(app.RepoRoot, ".worktrees", "feature")
	os.WriteFile(filepath.Join(wt, "late.txt"), []byte("late\n"), 0644)
	gitOut(t, wt, "add", ".")
	gitOut(t, wt, "commit", "-q", "-m", "late")

	v, err := BuildDiff(app, Row{Kind: RowReview, ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	header := strings.Join(v.Header, "\n")
	diff := strings.Join(v.Diff, "\n")
	if v.Key != "review:"+task.ID || v.SHA != sha || !strings.Contains(v.Title, "main..."+sha[:8]) {
		t.Errorf("key/title = %q %q", v.Key, v.Title)
	}
	if !strings.Contains(header, "Checks on "+sha[:8]+": lint PASS, test PASS") {
		t.Errorf("header lacks checks:\n%s", header)
	}
	if !strings.Contains(diff, "+new feature") || strings.Contains(diff, "late") {
		t.Errorf("diff is not exactly the requested commit:\n%s", diff)
	}
}

func TestBuildDiffEpic(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	proj, _ := app.DB.CreateProject("P", "")
	wt := filepath.Join(app.RepoRoot, ".worktrees", "epic")
	gitOut(t, app.RepoRoot, "worktree", "add", "-q", "-b", "epic/e", wt)
	os.WriteFile(filepath.Join(wt, "epic.txt"), []byte("epic work\n"), 0644)
	gitOut(t, wt, "add", ".")
	gitOut(t, wt, "commit", "-q", "-m", "epic work")
	head := gitOut(t, wt, "rev-parse", "HEAD")
	epic, _ := app.DB.CreateEpic("E", "epic description\nsecond line", proj.ID, "epic/e")
	row := Row{Kind: RowEpic, ID: epic.ID, IsEpic: true}

	v, err := BuildDiff(app, row)
	if err != nil {
		t.Fatal(err)
	}
	header := strings.Join(v.Header, "\n")
	if v.SHA != "" || v.Range != "main...epic/e @ "+head[:8] || !strings.Contains(header, "Branch head main...epic/e") || !strings.Contains(strings.Join(v.Diff, "\n"), "+epic work") {
		t.Errorf("epic branch-head diff = %+v", v)
	}
	if !strings.Contains(header, "Description:\n  epic description\n  second line") || len(v.HeaderSpans) != 1 {
		t.Errorf("description not kept multi-line: %q spans=%v", header, v.HeaderSpans)
	}

	app.DB.OpenReviewRequest("", epic.ID, head)
	v, err = BuildDiff(app, row)
	if err != nil {
		t.Fatal(err)
	}
	if v.SHA != head || !strings.Contains(strings.Join(v.Header, "\n"), "Requested commit "+head[:8]) {
		t.Errorf("epic request diff = %+v", v)
	}
}

func TestBuildDiffTaskStates(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)

	wt := filepath.Join(app.RepoRoot, ".worktrees", "wip")
	gitOut(t, app.RepoRoot, "worktree", "add", "-q", "-b", "wip", wt)
	os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("work in progress\n"), 0644)
	gitOut(t, wt, "add", ".")
	gitOut(t, wt, "commit", "-q", "-m", "wip")
	head := gitOut(t, wt, "rev-parse", "HEAD")
	wip := seedTaskAt(t, app, "wip", "", internal.StateInProgress, wt)
	v, err := BuildDiff(app, Row{Kind: RowTask, ID: wip.ID})
	if err != nil {
		t.Fatal(err)
	}
	if v.SHA != "" || v.Range != "main...wip @ "+head[:8] || !strings.Contains(strings.Join(v.Diff, "\n"), "+work in progress") {
		t.Errorf("in-progress diff = %+v", v)
	}

	planned := seedTask(t, app, "later", "", internal.StatePlanned)
	merged := seedTask(t, app, "gone", "", internal.StateMerged)
	for _, tc := range []struct {
		id, want string
	}{
		{planned.ID, "Not started; nothing to diff."},
		{merged.ID, "Branch gone deleted (MERGED); nothing to diff."},
	} {
		v, err := BuildDiff(app, Row{Kind: RowTask, ID: tc.id})
		if err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
		if !strings.Contains(strings.Join(v.Header, "\n"), tc.want) || v.Range != "" || len(v.Diff) != 0 {
			t.Errorf("%s: view = %+v, want %q", tc.id, v, tc.want)
		}
		out := Render(Snapshot{Repo: "r", Now: fixedNow}, UIState{DiffOpen: true, DiffKey: v.Key, Diff: &v}, 100, 20)
		if !strings.Contains(out, tc.want) || strings.Contains(out, "empty diff") {
			t.Errorf("%s render:\n%s", tc.id, out)
		}
	}
}

// The real verdict path refuses without a terminal on stdin (the usual case
// under go test) and otherwise records terminal:<user> (dashboard); the
// approving path is also covered in package internal.
func TestAppLoaderVerdictUsesTerminalPath(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	task, sha := reviewedTask(t, app)
	report, err := AppLoader(app, false).Verdict(task.ID, "approve", "lgtm", sha)
	got, _ := app.DB.GetTask(task.ID)
	if err != nil {
		if !strings.Contains(err.Error(), "refusing a dashboard verdict") || got.State != internal.StateReviewRequested {
			t.Errorf("err = %v, state = %s", err, got.State)
		}
		return
	}
	reviews, _ := app.DB.GetReviews(task.ID)
	if got.State != internal.StateApproved || len(reviews) != 1 || !strings.HasSuffix(reviews[0].Reviewer, " (dashboard)") || !strings.Contains(report, "APPROVED") {
		t.Errorf("state = %s, reviews = %+v, report = %q", got.State, reviews, report)
	}
}
