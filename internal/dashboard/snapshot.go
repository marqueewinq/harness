// Package dashboard builds a read-only snapshot of harness state and renders it.
package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
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

// mergeReasonWidth caps the reason in a row's attention text; the detail
// pane shows it in full.
const mergeReasonWidth = 60

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
	// Browse is every task of any state and age under every epic (epics
	// without tasks included), for the dashboard's "all" view.
	Browse   []EpicGroup
	Warnings []string
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
	// ReviewSHA is the commit of the epic's open review request, if any.
	ReviewSHA string
	Approval  string
	Tasks     []TaskRow
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
	// MergeFailure is the recorded reason of a MERGE_FAILED task.
	MergeFailure string
	ReviewType   internal.ReviewType
	// Approval is the approval column: review type · where the gate stands.
	Approval string
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
		if r.MergeFailure == "" {
			return "merge failed"
		}
		return "merge failed: " + ansi.Truncate(r.MergeFailure, mergeReasonWidth, "…")
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

// gateStatus says where a subject's review gate stands, from its state (""
// for epics), reviewer status of an in-flight request, and latest verdict.
func gateStatus(state internal.TaskState, inFlight bool, reviewer, verdict string) string {
	switch {
	case inFlight && reviewer == ReviewerHuman:
		return "awaiting"
	case inFlight && (reviewer == "" || reviewer == ReviewerPending || reviewer == internal.ReviewerRunning):
		return "reviewing"
	case inFlight:
		return "reviewer " + reviewer
	}
	switch state {
	case internal.StateApproved, internal.StateQueued, internal.StateMerging, internal.StateMerged:
		return "approved"
	case internal.StateRequestChanges:
		return "changes requested"
	case "":
		if verdict != "" {
			return verdict
		}
	}
	return "not started yet"
}

// approvalText is the approval column. A none subject has no gate, so it
// shows who waived it instead.
func approvalText(rt internal.ReviewType, setBy, status string) string {
	switch rt {
	case internal.ReviewNone:
		return "none · " + noneProvenance(setBy)
	case "":
		rt = internal.ReviewDefault
	}
	return string(rt) + " · " + status
}

func noneProvenance(setBy string) string {
	switch {
	case strings.HasPrefix(setBy, internal.ReviewSetByPreApproved):
		return "pre-approved by " + shortSetter(strings.TrimPrefix(setBy, internal.ReviewSetByPreApproved))
	case setBy == internal.ReviewSetBySelfDeclared:
		return "self-declared"
	case setBy == "":
		return "no review"
	}
	return setBy
}

// shortSetter turns "terminal:alice (dashboard): reason" into "alice".
func shortSetter(who string) string {
	who = strings.TrimPrefix(who, "terminal:")
	if i := strings.IndexAny(who, " :"); i > 0 {
		who = who[:i]
	}
	return who
}

// isHumanReview: a human-typed subject's request launches no reviewer; a
// default one does unless the committed config's review.type is human.
func (b *builder) isHumanReview(rt internal.ReviewType) bool {
	return rt == internal.ReviewHuman || (rt != internal.ReviewNone && b.human)
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
	app           *internal.App
	now           time.Time
	human         bool
	queue         map[string]QueueRow
	snap          *Snapshot
	entered       map[string]time.Time
	checkRuns     map[string][]internal.CheckRun
	taskDecisions map[string]string
	epicDecisions map[string]string
	latestReqs    map[string]internal.ReviewRequest
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
	if b.entered, err = app.DB.StateEnteredAt(); err != nil {
		return Snapshot{}, err
	}
	if b.checkRuns, err = app.DB.LatestCheckRunsByTask(); err != nil {
		return Snapshot{}, err
	}
	if b.taskDecisions, b.epicDecisions, err = app.DB.LatestReviewDecisions(); err != nil {
		return Snapshot{}, err
	}
	if b.latestReqs, err = app.DB.LatestTaskReviewRequests(); err != nil {
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
		row := ReviewRow{CommitSHA: req.CommitSHA, Age: now.Sub(req.CreatedAt)}
		var rt internal.ReviewType
		if req.EpicID != "" {
			openByEpic[req.EpicID] = req
			row.SubjectID, row.IsEpic = req.EpicID, true
			if e := epicByID[req.EpicID]; e != nil {
				row.Title, rt = e.Title, e.ReviewType
			}
		} else {
			row.SubjectID = req.TaskID
			if t := taskByID[req.TaskID]; t != nil {
				row.Title, rt = t.Title, t.ReviewType
			}
		}
		if rt == internal.ReviewNone {
			continue
		}
		row.Reviewer = reviewerStatus(req, now, b.isHumanReview(rt))
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
			reviewer := ""
			if req := openByEpic[e.ID]; req != nil {
				g.ReviewSHA = req.CommitSHA
				reviewer = reviewerStatus(req, now, b.isHumanReview(e.ReviewType))
			}
			g.Approval = approvalText(e.ReviewType, e.ReviewTypeSetBy,
				gateStatus("", g.ReviewSHA != "", reviewer, verdictLabel(b.epicDecisions[e.ID])))
		}
		m[epicID] = g
		return g
	}
	browse := map[string]*EpicGroup{}
	for _, e := range epics {
		group(browse, e.ID)
	}
	for i := range tasks {
		t := &tasks[i]
		epicID := t.EpicID
		if epicByID[epicID] == nil {
			epicID = ""
		}
		row := b.taskRow(t)
		g := group(browse, epicID)
		g.Tasks = append(g.Tasks, row)
		switch {
		case inFlight(t.State) || (all && t.State == internal.StatePlanned):
			g := group(groups, epicID)
			g.Tasks = append(g.Tasks, row)
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
	s.Browse = sortGroups(browse)
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
	return verdictLabel(b.epicDecisions[epicID])
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
	if at, ok := b.entered[t.ID]; ok {
		return at
	}
	return t.UpdatedAt
}

func (b *builder) taskRow(t *internal.Task) TaskRow {
	since := b.stateSince(t)
	row := TaskRow{ID: t.ID, Title: t.Title, State: t.State, Age: b.now.Sub(since), ReviewType: t.ReviewType}

	if t.State == internal.StateChecksRunning {
		row.Checks = b.runningChecks(t.ID, since)
	} else {
		for _, r := range b.checkRuns[t.ID] {
			row.Checks = append(row.Checks, CheckStatus{Name: r.CheckName, Result: checkResult(r.ExitCode)})
		}
	}

	if t.State == internal.StateReviewRequested {
		if req, ok := b.latestReqs[t.ID]; ok {
			row.Review = &req
			row.Reviewer = reviewerStatus(&req, b.now, b.isHumanReview(t.ReviewType))
		}
	}
	row.Verdict = verdictLabel(b.taskDecisions[t.ID])
	row.Approval = approvalText(t.ReviewType, t.ReviewTypeSetBy,
		gateStatus(t.State, t.State == internal.StateReviewRequested, row.Reviewer, row.Verdict))
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
	if t.State == internal.StateMergeFailed {
		if m, err := b.app.LastMergeFailure(t.ID); err != nil {
			b.warn("reading merge failure of %s: %v", t.ID, err)
		} else if m != nil {
			row.MergeFailure = m.Reason
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

// fsClockSlack absorbs the kernel's coarse file clock lagging time.Now() by a
// tick, so a log created right after a run started may carry a timestamp
// slightly before the recorded start. Kept small so a log from a run that
// ended just before is not mistaken for a live one.
const fsClockSlack = 100 * time.Millisecond

func writtenSince(t, since time.Time) bool {
	return !t.Before(since.Add(-fsClockSlack))
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
		if err != nil || !writtenSince(fi.ModTime(), since) {
			continue
		}
		c := CheckStatus{Name: name, Result: CheckRunning}
		if born, ok := birthTime(path); ok && writtenSince(born, since) {
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
