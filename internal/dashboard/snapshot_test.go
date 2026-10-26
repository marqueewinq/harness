package dashboard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/marqueewinq/harness/internal"
)

func build(t *testing.T, app *internal.App, now time.Time, all bool) Snapshot {
	t.Helper()
	s, err := BuildSnapshot(app, now, all)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSnapshotStatesSplitIntoInFlightAndRecent(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	states := []internal.TaskState{
		internal.StatePlanned, internal.StateInProgress, internal.StateChecksRunning,
		internal.StateChecksPassed, internal.StateChecksFailed, internal.StateReviewRequested,
		internal.StateRequestChanges, internal.StateApproved, internal.StateQueued,
		internal.StateMerging, internal.StateMerged, internal.StateMergeFailed, internal.StateClosed,
	}
	ids := map[internal.TaskState]string{}
	for _, st := range states {
		ids[st] = seedTask(t, app, "task "+string(st), "", st).ID
	}

	s := build(t, app, time.Now(), false)
	var wantInFlight []string
	for _, st := range states {
		switch st {
		case internal.StatePlanned, internal.StateMerged, internal.StateClosed:
		default:
			wantInFlight = append(wantInFlight, ids[st])
		}
	}
	if got := rowIDs(s.Groups); !reflect.DeepEqual(got, wantInFlight) {
		t.Errorf("in flight = %v, want %v", got, wantInFlight)
	}
	for _, st := range states {
		if id, ok := ids[st]; ok && contains(wantInFlight, id) {
			if got := findRow(t, s.Groups, id).State; got != st {
				t.Errorf("%s state = %s, want %s", id, got, st)
			}
		}
	}
	wantRecent := []string{ids[internal.StateMerged], ids[internal.StateClosed]}
	if got := rowIDs(s.Recent); !reflect.DeepEqual(got, wantRecent) {
		t.Errorf("recent = %v, want %v", got, wantRecent)
	}

	all := build(t, app, time.Now(), true)
	if got := rowIDs(all.Groups); !contains(got, ids[internal.StatePlanned]) {
		t.Errorf("--all groups lack PLANNED task: %v", got)
	}
	if got := rowIDs(all.Groups); contains(got, ids[internal.StateMerged]) {
		t.Errorf("--all groups include MERGED task (belongs in recent): %v", got)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestSnapshotAgeInStateFromTransitions(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	task := seedTask(t, app, "aging", "", internal.StateChecksFailed)
	s := build(t, app, time.Now().Add(90*time.Minute), false)
	age := findRow(t, s.Groups, task.ID).Age
	if age < 90*time.Minute || age > 91*time.Minute {
		t.Errorf("age = %s, want ~90m", age)
	}
}

func TestSnapshotRecentWindow(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	task := seedTask(t, app, "done", "", internal.StateMerged)
	later := time.Now().Add(25 * time.Hour)
	if got := rowIDs(build(t, app, later, false).Recent); len(got) != 0 {
		t.Errorf("recent after 25h = %v, want empty", got)
	}
	if got := rowIDs(build(t, app, later, true).Recent); !reflect.DeepEqual(got, []string{task.ID}) {
		t.Errorf("--all recent after 25h = %v, want [%s]", got, task.ID)
	}
}

func TestSnapshotAttention(t *testing.T) {
	t.Parallel()
	reviewerOutcome := func(outcome string) func(t *testing.T, app *internal.App) string {
		return func(t *testing.T, app *internal.App) string {
			task := seedTask(t, app, "reviewed", "", internal.StateReviewRequested)
			_, reqID, err := app.DB.OpenReviewRequestID(task.ID, "", "abc12345ff")
			if err != nil {
				t.Fatal(err)
			}
			if err := app.DB.StartReviewerRun(reqID, os.Getpid(), 1); err != nil {
				t.Fatal(err)
			}
			if _, err := app.DB.FinishReviewerRun(reqID, outcome, 3); err != nil {
				t.Fatal(err)
			}
			return task.ID
		}
	}
	cases := []struct {
		name   string
		config string
		seed   func(t *testing.T, app *internal.App) string
		want   string
	}{
		{"none", humanConfig, func(t *testing.T, app *internal.App) string {
			return seedTask(t, app, "plain", "", internal.StateInProgress).ID
		}, ""},
		{"checks failed", humanConfig, func(t *testing.T, app *internal.App) string {
			return seedTask(t, app, "red", "", internal.StateChecksFailed).ID
		}, "checks failed"},
		{"merge failed", humanConfig, func(t *testing.T, app *internal.App) string {
			return seedTask(t, app, "conflict", "", internal.StateMergeFailed).ID
		}, "merge failed"},
		{"reviewer failed", agentConfig, reviewerOutcome(internal.ReviewerFailed), "reviewer failed"},
		{"reviewer timeout", agentConfig, reviewerOutcome(internal.ReviewerTimeout), "reviewer timeout"},
		{"reviewer no verdict", agentConfig, reviewerOutcome(internal.ReviewerNoVerdict), "reviewer no_verdict"},
		{"reviewer orphaned", agentConfig, func(t *testing.T, app *internal.App) string {
			task := seedTask(t, app, "orphan", "", internal.StateReviewRequested)
			_, reqID, err := app.DB.OpenReviewRequestID(task.ID, "", "abc12345ff")
			if err != nil {
				t.Fatal(err)
			}
			if err := app.DB.StartReviewerRun(reqID, deadPID(t), 4242); err != nil {
				t.Fatal(err)
			}
			return task.ID
		}, "reviewer orphaned"},
		{"stale queue lease", humanConfig, func(t *testing.T, app *internal.App) string {
			task := seedTask(t, app, "stuck", "", internal.StateQueued)
			if _, err := app.DB.EnqueueTask(task.ID, deadPID(t)); err != nil {
				t.Fatal(err)
			}
			return task.ID
		}, "stale queue lease"},
		{"live queue lease", humanConfig, func(t *testing.T, app *internal.App) string {
			task := seedTask(t, app, "merging", "", internal.StateQueued)
			if _, err := app.DB.EnqueueTask(task.ID, os.Getppid()); err != nil {
				t.Fatal(err)
			}
			return task.ID
		}, ""},
		{"unfinished git operation", humanConfig, func(t *testing.T, app *internal.App) string {
			wt := filepath.Join(app.RepoRoot, ".worktrees", "rebasing")
			gitOut(t, app.RepoRoot, "worktree", "add", "-q", "-b", "rebasing", wt)
			task := seedTaskAt(t, app, "rebasing", "", internal.StateMergeFailed, wt)
			marker := gitOut(t, wt, "rev-parse", "--path-format=absolute", "--git-path", "rebase-merge")
			if err := os.MkdirAll(marker, 0755); err != nil {
				t.Fatal(err)
			}
			return task.ID
		}, "unfinished rebase"},
		{"human review", humanConfig, func(t *testing.T, app *internal.App) string {
			task := seedTask(t, app, "await", "", internal.StateReviewRequested)
			if _, err := app.DB.OpenReviewRequest(task.ID, "", "abc12345ff"); err != nil {
				t.Fatal(err)
			}
			return task.ID
		}, "awaiting human review"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := newRepo(t, tc.config)
			id := tc.seed(t, app)
			row := findRow(t, build(t, app, time.Now(), false).Groups, id)
			if row.Attention != tc.want {
				t.Errorf("attention = %q, want %q (row %+v)", row.Attention, tc.want, row)
			}
		})
	}
}

func TestSnapshotEpicGrouping(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	p1, err := app.DB.CreateProject("Platform", "")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := app.DB.CreateProject("Other", "")
	if err != nil {
		t.Fatal(err)
	}
	e2, _ := app.DB.CreateEpic("Later epic", "", p2.ID, "")
	e1, _ := app.DB.CreateEpic("Planning layer", "", p1.ID, "epic/planning")

	seedTask(t, app, "m1", e1.ID, internal.StateMerged)
	seedTask(t, app, "c1", e1.ID, internal.StateClosed)
	b := seedTask(t, app, "b", e1.ID, internal.StateChecksRunning)
	seedTask(t, app, "p", e1.ID, internal.StatePlanned)
	a := seedTask(t, app, "a", e1.ID, internal.StateInProgress)
	x := seedTask(t, app, "x", e2.ID, internal.StateInProgress)
	loose := seedTask(t, app, "loose", "", internal.StateInProgress)
	if _, err := app.DB.OpenReviewRequest("", e1.ID, "deadbeef00"); err != nil {
		t.Fatal(err)
	}

	s := build(t, app, time.Now(), false)
	if len(s.Groups) != 3 {
		t.Fatalf("groups = %d, want 3: %+v", len(s.Groups), s.Groups)
	}
	g := s.Groups[0]
	if g.Epic == nil || g.Epic.ID != e1.ID || g.Project == nil || g.Project.ID != p1.ID {
		t.Fatalf("first group = %+v, want %s/%s", g, p1.ID, e1.ID)
	}
	if g.Branch != "epic/planning" || g.Done != 2 || g.Total != 5 {
		t.Errorf("group branch/done/total = %q %d/%d, want epic/planning 2/5", g.Branch, g.Done, g.Total)
	}
	if g.EpicReview != "review requested" {
		t.Errorf("epic review = %q, want review requested", g.EpicReview)
	}
	if got := rowIDs(s.Groups[:1]); !reflect.DeepEqual(got, []string{b.ID, a.ID}) {
		t.Errorf("epic tasks = %v, want [%s %s] (by task ID)", got, b.ID, a.ID)
	}
	if s.Groups[1].Epic == nil || s.Groups[1].Epic.ID != e2.ID || rowIDs(s.Groups[1:2])[0] != x.ID {
		t.Errorf("second group = %+v, want %s", s.Groups[1], e2.ID)
	}
	last := s.Groups[2]
	if last.Epic != nil || last.Project != nil || len(last.Tasks) != 1 || last.Tasks[0].ID != loose.ID {
		t.Errorf("last group = %+v, want No epic with %s", last, loose.ID)
	}

	if got := rowIDs(s.Recent); len(got) != 2 || s.Recent[0].Epic == nil || s.Recent[0].Epic.ID != e1.ID {
		t.Errorf("recent = %+v, want both done tasks under %s", s.Recent, e1.ID)
	}
}

func TestSnapshotReviews(t *testing.T) {
	t.Parallel()
	app := newRepo(t, agentConfig)
	proj, _ := app.DB.CreateProject("P", "")
	epic, _ := app.DB.CreateEpic("E", "", proj.ID, "epic/e")
	task := seedTask(t, app, "orphan", "", internal.StateReviewRequested)
	_, reqID, err := app.DB.OpenReviewRequestID(task.ID, "", "8174267712345")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.DB.StartReviewerRun(reqID, deadPID(t), 4242); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.OpenReviewRequest("", epic.ID, "5d1c09aa999"); err != nil {
		t.Fatal(err)
	}

	s := build(t, app, time.Now().Add(12*time.Minute), false)
	if len(s.Reviews) != 2 {
		t.Fatalf("reviews = %+v, want 2", s.Reviews)
	}
	r := s.Reviews[0]
	if r.SubjectID != task.ID || r.IsEpic || r.CommitSHA != "8174267712345" || r.Reviewer != internal.ReviewerOrphaned || r.Attention != "reviewer orphaned" {
		t.Errorf("task review row = %+v", r)
	}
	if r.Age < 11*time.Minute || r.Age > 13*time.Minute {
		t.Errorf("review age = %s, want ~12m", r.Age)
	}
	e := s.Reviews[1]
	if e.SubjectID != epic.ID || !e.IsEpic || e.Title != "E" || e.Reviewer != ReviewerPending || e.Attention != "" {
		t.Errorf("epic review row = %+v", e)
	}

	human := newRepo(t, humanConfig)
	ht := seedTask(t, human, "await", "", internal.StateReviewRequested)
	if _, err := human.DB.OpenReviewRequest(ht.ID, "", "abc"); err != nil {
		t.Fatal(err)
	}
	hs := build(t, human, time.Now(), false)
	if len(hs.Reviews) != 1 || hs.Reviews[0].Reviewer != ReviewerHuman || hs.Reviews[0].Attention != "awaiting human review" {
		t.Errorf("human reviews = %+v", hs.Reviews)
	}
}

func TestSnapshotQueue(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	live := seedTask(t, app, "live", "", internal.StateQueued)
	stale := seedTask(t, app, "stale", "", internal.StateQueued)
	if _, err := app.DB.EnqueueTask(live.ID, os.Getppid()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.EnqueueTask(stale.ID, deadPID(t)); err != nil {
		t.Fatal(err)
	}
	s := build(t, app, time.Now(), false)
	if len(s.Queue) != 2 {
		t.Fatalf("queue = %+v", s.Queue)
	}
	if q := s.Queue[0]; q.Position != 1 || q.TaskID != live.ID || q.Stale || q.PID != os.Getppid() {
		t.Errorf("queue[0] = %+v, want live %s at 1", q, live.ID)
	}
	if q := s.Queue[1]; q.Position != 2 || q.TaskID != stale.ID || !q.Stale {
		t.Errorf("queue[1] = %+v, want stale %s at 2", q, stale.ID)
	}
	if r := findRow(t, s.Groups, stale.ID); r.QueuePos != 2 || !r.QueueStale {
		t.Errorf("stale row queue = %d %v", r.QueuePos, r.QueueStale)
	}

	expired := build(t, app, time.Now().Add(internal.QueueLeaseTTL+time.Minute), false)
	if !expired.Queue[0].Stale {
		t.Errorf("live lease past TTL not stale: %+v", expired.Queue[0])
	}
}

func TestSnapshotChecks(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	done := seedTask(t, app, "done checks", "", internal.StateChecksFailed)
	start := time.Now().Add(-time.Minute)
	app.DB.RecordCheckRun(done.ID, "lint", 0, "", "", 1, start, start.Add(time.Second), "sha")
	app.DB.RecordCheckRun(done.ID, "test", 2, "", "boom", 1, start, start.Add(2*time.Second), "sha")
	row := findRow(t, build(t, app, time.Now(), false).Groups, done.ID)
	want := []CheckStatus{{Name: "lint", Result: CheckPass}, {Name: "test", Result: CheckFail}}
	if !reflect.DeepEqual(row.Checks, want) {
		t.Errorf("checks = %+v, want %+v", row.Checks, want)
	}

	running := seedTask(t, app, "running checks", "", internal.StateChecksRunning)
	logDir := internal.CheckLogDir(app.RepoRoot, running.ID)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(logDir, "lint.log")
	os.WriteFile(old, []byte("previous run\n"), 0644)
	past := time.Now().Add(-time.Hour)
	os.Chtimes(old, past, past)
	os.WriteFile(filepath.Join(logDir, "test.log"), []byte("running\n"), 0644)

	row = findRow(t, build(t, app, time.Now().Add(41*time.Second), false).Groups, running.ID)
	if len(row.Checks) != 1 || row.Checks[0].Name != "test" || row.Checks[0].Result != CheckRunning {
		t.Fatalf("running checks = %+v, want only test running", row.Checks)
	}
	if c := row.Checks[0]; c.ElapsedKnown && (c.Elapsed < 40*time.Second || c.Elapsed > 50*time.Second) {
		t.Errorf("elapsed = %s, want ~41s", c.Elapsed)
	}
}

func TestSnapshotHeader(t *testing.T) {
	t.Parallel()
	app := newRepo(t, humanConfig)
	s := build(t, app, time.Now(), false)
	if s.Repo != filepath.Base(app.RepoRoot) || s.MainBranch != "main" {
		t.Errorf("repo/main = %q %q", s.Repo, s.MainBranch)
	}
	head := gitOut(t, app.RepoRoot, "rev-parse", "--short", "main")
	if s.MainHead != head {
		t.Errorf("main head = %q, want %q", s.MainHead, head)
	}
	if len(s.Warnings) != 0 {
		t.Errorf("warnings = %v", s.Warnings)
	}
}
