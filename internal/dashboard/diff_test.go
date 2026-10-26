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

func TestBuildDiffEpicAndMissingRequest(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	proj, _ := app.DB.CreateProject("P", "")
	gitOut(t, app.RepoRoot, "branch", "epic/e")
	epic, _ := app.DB.CreateEpic("E", "epic description", proj.ID, "epic/e")
	if _, err := BuildDiff(app, Row{Kind: RowReview, ID: epic.ID, IsEpic: true}); err == nil || !strings.Contains(err.Error(), "no open review request") {
		t.Errorf("err = %v", err)
	}
	head := gitOut(t, app.RepoRoot, "rev-parse", "epic/e")
	app.DB.OpenReviewRequest("", epic.ID, head)
	v, err := BuildDiff(app, Row{Kind: RowReview, ID: epic.ID, IsEpic: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(v.Header, "\n"), "Description: epic description") || len(v.Diff) != 0 {
		t.Errorf("epic diff view = %+v", v)
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
