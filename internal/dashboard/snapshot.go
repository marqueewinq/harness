// Package dashboard builds a read-only snapshot of harness state and renders it.
package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/marqueewinq/harness/internal"
)

// Reviewer values beyond the stored internal.Reviewer* run outcomes.
const (
	ReviewerHuman   = "human"
	ReviewerPending = "pending"
)

const (
	CheckPass    = "pass"
	CheckFail    = "fail"
	CheckSkip    = "skip"
	CheckRunning = "running"
)

const recentWindow = 24 * time.Hour

type Snapshot struct {
	Repo       string
	MainBranch string
	MainHead   string
	Now        time.Time
	All        bool
	Reviews    []ReviewRow
	Groups     []EpicGroup
	Queue      []QueueRow
	Recent     []EpicGroup
	Warnings   []string
}

type ReviewRow struct {
	SubjectID string
	IsEpic    bool
	Title     string
	CommitSHA string
	Age       time.Duration
	Reviewer  string
	Attention string
}

// EpicGroup with a nil Epic is the "No epic" group.
type EpicGroup struct {
	Project    *internal.Project
	Epic       *internal.Epic
	Branch     string
	Done       int
	Total      int
	EpicReview string
	Tasks      []TaskRow
}

type CheckStatus struct {
	Name         string
	Result       string
	Elapsed      time.Duration
	ElapsedKnown bool
}

type TaskRow struct {
	ID         string
	Title      string
	State      internal.TaskState
	Age        time.Duration
	Checks     []CheckStatus
	Review     *internal.ReviewRequest
	Reviewer   string
	Verdict    string
	QueuePos   int
	QueueStale bool
	GitOp      string
	Attention  string
}

type QueueRow struct {
	Position     int
	TaskID       string
	PID          int
	HeartbeatAge time.Duration
	Stale        bool
}

func inFlight(s internal.TaskState) bool {
	return s != internal.StatePlanned && s != internal.StateMerged && s != internal.StateClosed
}

func finished(s internal.TaskState) bool {
	return s == internal.StateMerged || s == internal.StateClosed
}

// attention is the single place deciding whether a task row gets the red `!`.
func attention(r TaskRow) string {
	switch {
	case r.GitOp != "":
		return "unfinished " + r.GitOp
	case r.State == internal.StateMergeFailed:
		return "merge failed"
	case r.State == internal.StateChecksFailed:
		return "checks failed"
	case r.QueueStale:
		return "stale queue lease"
	case r.State == internal.StateReviewRequested:
		return reviewerAttention(r.Reviewer)
	}
	return ""
}

func reviewerAttention(reviewer string) string {
	switch reviewer {
	case internal.ReviewerFailed, internal.ReviewerTimeout, internal.ReviewerOrphaned, internal.ReviewerNoVerdict, internal.ReviewerInterrupted:
		return "reviewer " + reviewer
	case ReviewerHuman:
		return "awaiting human review"
	}
	return ""
}

// reviewerStatus is "" for a request that ended without launching a reviewer.
func reviewerStatus(req *internal.ReviewRequest, now time.Time, human bool) string {
	switch {
	case req.ReviewerOutcome != "":
		return req.RunStatus(now)
	case req.Resolved:
		return ""
	case human:
		return ReviewerHuman
	}
	return ReviewerPending
}

var gitOpNames = map[string]string{
	"MERGE_HEAD":       "merge",
	"rebase-merge":     "rebase",
	"rebase-apply":     "rebase",
	"CHERRY_PICK_HEAD": "cherry-pick",
}

type builder struct {
	app   *internal.App
	now   time.Time
	human bool
	queue map[string]QueueRow
	snap  *Snapshot
}

func (b *builder) warn(format string, args ...interface{}) {
	b.snap.Warnings = append(b.snap.Warnings, fmt.Sprintf(format, args...))
}

// BuildSnapshot reads harness state once. all adds PLANNED tasks to Groups
// and lifts the 24h limit on Recent. Errors reading the task, epic, project,
// queue or review tables fail the snapshot; anything else becomes a warning.
func BuildSnapshot(app *internal.App, now time.Time, all bool) (Snapshot, error) {
	s := Snapshot{Repo: filepath.Base(app.RepoRoot), MainBranch: app.Config.MainBranch, Now: now, All: all}
	b := &builder{app: app, now: now, snap: &s, queue: map[string]QueueRow{}}

	if head, err := app.Git.RevParseShort("refs/heads/" + app.Config.MainBranch); err == nil {
		s.MainHead = head
	} else {
		b.warn("reading %s head failed", app.Config.MainBranch)
	}
	b.human = b.humanReview()

	tasks, err := app.DB.ListTasks()
	if err != nil {
		return Snapshot{}, err
	}
	epics, err := app.DB.ListEpics()
	if err != nil {
		return Snapshot{}, err
	}
	projects, err := app.DB.ListProjects()
	if err != nil {
		return Snapshot{}, err
	}
	entries, err := app.DB.ListQueue()
	if err != nil {
		return Snapshot{}, err
	}
	open, err := app.DB.ListOpenReviewRequests()
	if err != nil {
		return Snapshot{}, err
	}

	for i, e := range entries {
		q := QueueRow{Position: i + 1, TaskID: e.TaskID, PID: e.PID, HeartbeatAge: now.Sub(e.Heartbeat), Stale: internal.LeaseStale(e, now)}
		s.Queue = append(s.Queue, q)
		b.queue[e.TaskID] = q
	}

	epicByID := map[string]*internal.Epic{}
	for i := range epics {
		epicByID[epics[i].ID] = &epics[i]
	}
	projByID := map[string]*internal.Project{}
	for i := range projects {
		projByID[projects[i].ID] = &projects[i]
	}
	taskByID := map[string]*internal.Task{}
	byEpic := map[string][]internal.Task{}
	for i := range tasks {
		taskByID[tasks[i].ID] = &tasks[i]
		byEpic[tasks[i].EpicID] = append(byEpic[tasks[i].EpicID], tasks[i])
	}

	openByEpic := map[string]*internal.ReviewRequest{}
	for i := range open {
		req := &open[i]
		row := ReviewRow{CommitSHA: req.CommitSHA, Age: now.Sub(req.CreatedAt), Reviewer: reviewerStatus(req, now, b.human)}
		if req.EpicID != "" {
			openByEpic[req.EpicID] = req
			row.SubjectID, row.IsEpic = req.EpicID, true
			if e := epicByID[req.EpicID]; e != nil {
				row.Title = e.Title
			}
		} else {
			row.SubjectID = req.TaskID
			if t := taskByID[req.TaskID]; t != nil {
				row.Title = t.Title
			}
		}
		row.Attention = reviewerAttention(row.Reviewer)
		s.Reviews = append(s.Reviews, row)
	}

	groups := map[string]*EpicGroup{}
	recent := map[string]*EpicGroup{}
	group := func(m map[string]*EpicGroup, epicID string) *EpicGroup {
		if g, ok := m[epicID]; ok {
			return g
		}
		g := &EpicGroup{}
		if e := epicByID[epicID]; e != nil {
			g.Epic, g.Project, g.Branch = e, projByID[e.ProjectID], e.Branch
			g.Done, g.Total = internal.CountDone(byEpic[epicID])
			g.EpicReview = b.epicReview(e.ID, openByEpic[e.ID])
		}
		m[epicID] = g
		return g
	}
	for i := range tasks {
		t := &tasks[i]
		epicID := t.EpicID
		if epicByID[epicID] == nil {
			epicID = ""
		}
		switch {
		case inFlight(t.State) || (all && t.State == internal.StatePlanned):
			g := group(groups, epicID)
			g.Tasks = append(g.Tasks, b.taskRow(t))
		case finished(t.State):
			since := b.stateSince(t)
			if !all && now.Sub(since) > recentWindow {
				continue
			}
			g := group(recent, epicID)
			g.Tasks = append(g.Tasks, TaskRow{ID: t.ID, Title: t.Title, State: t.State, Age: now.Sub(since)})
		}
	}
	s.Groups = sortGroups(groups)
	s.Recent = sortGroups(recent)
	return s, nil
}

// humanReview reads review.type from the config committed on main, not
// app.Config: `harness review request` delivers by the committed copy, so it
// decides whether a reviewer is launched or a human must act.
func (b *builder) humanReview() bool {
	review, err := b.app.CommittedReviewConfig()
	if err != nil {
		b.warn("committed review config unreadable, using working copy: %v", err)
		review = b.app.Config.Review
	}
	return review.Type != "agent"
}

func (b *builder) epicReview(epicID string, open *internal.ReviewRequest) string {
	if open != nil {
		return "review requested"
	}
	reviews, err := b.app.DB.GetEpicReviews(epicID)
	if err != nil || len(reviews) == 0 {
		return ""
	}
	return verdictLabel(reviews[len(reviews)-1].Decision)
}

func verdictLabel(decision string) string {
	switch decision {
	case "approve":
		return "approved"
	case "request_changes":
		return "changes requested"
	}
	return decision
}

// stateSince is when the task entered its current state; UpdatedAt covers
// rows whose transition history is missing.
func (b *builder) stateSince(t *internal.Task) time.Time {
	trs, err := b.app.DB.GetTaskTransitions(t.ID)
	if err == nil {
		for i := len(trs) - 1; i >= 0; i-- {
			if trs[i].ToState == string(t.State) {
				return trs[i].CreatedAt
			}
		}
	}
	return t.UpdatedAt
}

func (b *builder) taskRow(t *internal.Task) TaskRow {
	since := b.stateSince(t)
	row := TaskRow{ID: t.ID, Title: t.Title, State: t.State, Age: b.now.Sub(since)}

	if t.State == internal.StateChecksRunning {
		row.Checks = b.runningChecks(t.ID, since)
	} else if runs, err := b.app.DB.GetLatestCheckRuns(t.ID); err == nil {
		for _, r := range runs {
			row.Checks = append(row.Checks, CheckStatus{Name: r.CheckName, Result: checkResult(r.ExitCode)})
		}
	}

	if t.State == internal.StateReviewRequested {
		if req, err := b.app.DB.GetLatestReviewRequest(t.ID, ""); err == nil && req != nil {
			row.Review = req
			row.Reviewer = reviewerStatus(req, b.now, b.human)
		}
	}
	if reviews, err := b.app.DB.GetReviews(t.ID); err == nil && len(reviews) > 0 {
		row.Verdict = verdictLabel(reviews[len(reviews)-1].Decision)
	}
	if q, ok := b.queue[t.ID]; ok {
		row.QueuePos, row.QueueStale = q.Position, q.Stale
	}
	if inFlight(t.State) && t.WorktreePath != "" {
		if _, err := os.Stat(t.WorktreePath); err == nil {
			if op, err := internal.NewGitClient(t.WorktreePath).OperationInProgress(); err == nil && op != "" {
				row.GitOp = gitOpNames[op]
			}
		}
	}
	row.Attention = attention(row)
	return row
}

func checkResult(exitCode int) string {
	switch exitCode {
	case 0:
		return CheckPass
	case -1:
		return CheckSkip
	}
	return CheckFail
}

// runningChecks lists the checks whose live log was written since the run
// began. A check that already finished within the run cannot be told apart
// from one still running until the run is recorded, so it also shows here.
func (b *builder) runningChecks(taskID string, since time.Time) []CheckStatus {
	names := make([]string, 0, len(b.app.Config.Checks))
	for name := range b.app.Config.Checks {
		names = append(names, name)
	}
	sort.Strings(names)
	dir := internal.CheckLogDir(b.app.RepoRoot, taskID)
	var out []CheckStatus
	for _, name := range names {
		path := filepath.Join(dir, name+".log")
		fi, err := os.Stat(path)
		if err != nil || fi.ModTime().Before(since) {
			continue
		}
		c := CheckStatus{Name: name, Result: CheckRunning}
		if born, ok := birthTime(path); ok && !born.Before(since) {
			c.Elapsed, c.ElapsedKnown = b.now.Sub(born), true
		}
		out = append(out, c)
	}
	return out
}

// idLess orders "task-9" before "task-10".
func idLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func sortGroups(m map[string]*EpicGroup) []EpicGroup {
	out := make([]EpicGroup, 0, len(m))
	for _, g := range m {
		sort.Slice(g.Tasks, func(i, j int) bool { return idLess(g.Tasks[i].ID, g.Tasks[j].ID) })
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Epic == nil) != (b.Epic == nil) {
			return b.Epic == nil
		}
		if a.Epic == nil {
			return false
		}
		if a.Epic.ProjectID != b.Epic.ProjectID {
			return idLess(a.Epic.ProjectID, b.Epic.ProjectID)
		}
		return idLess(a.Epic.ID, b.Epic.ID)
	})
	return out
}
