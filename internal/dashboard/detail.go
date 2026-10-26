package dashboard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/marqueewinq/harness/internal"
)

const (
	logTailLines    = 10
	logTailBytes    = 16 << 10
	stderrLines     = 5
	historyMaxLines = 8
)

// BuildDetail reads what `harness work status` shows for the row's subject,
// plus the tail of a running check's live log.
func BuildDetail(app *internal.App, row Row, now time.Time) (Detail, error) {
	if row.IsEpic {
		return epicDetail(app, row, now)
	}
	return taskDetail(app, row, now)
}

func taskDetail(app *internal.App, row Row, now time.Time) (Detail, error) {
	t, err := app.DB.GetTask(row.ID)
	if err != nil {
		return Detail{}, err
	}
	var b doc
	add := b.add

	trs, err := app.DB.GetTaskTransitions(t.ID)
	if err != nil {
		return Detail{}, err
	}
	var since time.Time
	for _, tr := range trs {
		if tr.ToState == string(t.State) {
			since = tr.CreatedAt
		}
	}
	state := string(t.State)
	if !since.IsZero() {
		state += " for " + fmtAge(now.Sub(since))
	}
	add("State: %s · branch %s · base %s", state, orDash(t.Branch), orDash(t.BaseBranch))
	add("Worktree: %s", orDash(t.WorktreePath))
	b.text("", "Description: ", t.Description)

	if len(trs) > 0 {
		add("History:")
		start := 0
		if len(trs) > historyMaxLines {
			start = len(trs) - historyMaxLines
			add("  … %d earlier", start)
		}
		for _, tr := range trs[start:] {
			from := tr.FromState
			if from == "" {
				from = "(created)"
			}
			add("  %s  %s → %s", tr.CreatedAt.Format("01-02 15:04:05"), from, tr.ToState)
		}
	}

	runs, err := app.DB.GetLatestCheckRuns(t.ID)
	if err != nil {
		return Detail{}, err
	}
	if len(runs) > 0 {
		add("Checks (commit %s):", orDash(shortSHA(runs[0].CommitSHA)))
		for _, r := range runs {
			if r.ExitCode == -1 {
				add("  %-12s SKIP", r.CheckName)
				continue
			}
			result := "PASS"
			if r.ExitCode != 0 {
				result = fmt.Sprintf("FAIL (exit %d)", r.ExitCode)
			}
			add("  %-12s %s  %.1fs", r.CheckName, result, r.FinishedAt.Sub(r.StartedAt).Seconds())
			if r.ExitCode != 0 {
				for _, l := range lastLines(r.Stderr, stderrLines) {
					add("    %s", l)
				}
			}
		}
	}

	req, err := app.DB.GetLatestReviewRequest(t.ID, "")
	if err != nil {
		return Detail{}, err
	}
	reviewLines(add, req, now)
	reviews, err := app.DB.GetReviews(t.ID)
	if err != nil {
		return Detail{}, err
	}
	verdictLines(&b, reviews)

	entries, err := app.DB.ListQueue()
	if err != nil {
		return Detail{}, err
	}
	for i, e := range entries {
		if e.TaskID != t.ID {
			continue
		}
		status := "live"
		if internal.LeaseStale(e, now) {
			status = "stale"
		}
		add("Queue: #%d · pid %d · heartbeat %s ago · %s", i+1, e.PID, fmtAge(now.Sub(e.Heartbeat)), status)
	}

	if t.State == internal.StateChecksRunning {
		if name, tail := liveLogTail(app, t.ID, since); name != "" {
			add("Live log %s:", name)
			for _, l := range tail {
				add("  %s", l)
			}
		}
	}
	return b.detail(row.Key(), t.ID+" "+flatten(t.Title)), nil
}

func (b *doc) detail(key, title string) Detail {
	return Detail{Key: key, Title: title, Lines: b.lines, Full: b.full, Expandable: b.expandable}
}

func epicDetail(app *internal.App, row Row, now time.Time) (Detail, error) {
	e, err := app.DB.GetEpic(row.ID)
	if err != nil {
		return Detail{}, err
	}
	var b doc
	add := b.add
	add("Epic: %s · project %s · branch %s", e.Status, e.ProjectID, orDash(e.Branch))
	b.text("", "Description: ", e.Description)
	tasks, err := app.DB.ListTasksByEpic(e.ID)
	if err != nil {
		return Detail{}, err
	}
	done, total := internal.CountDone(tasks)
	add("Tasks: %d/%d done", done, total)
	req, err := app.DB.GetLatestReviewRequest("", e.ID)
	if err != nil {
		return Detail{}, err
	}
	reviewLines(add, req, now)
	reviews, err := app.DB.GetEpicReviews(e.ID)
	if err != nil {
		return Detail{}, err
	}
	verdictLines(&b, reviews)
	return b.detail(row.Key(), e.ID+" "+flatten(e.Title)), nil
}

func reviewLines(add func(string, ...interface{}), req *internal.ReviewRequest, now time.Time) {
	if req == nil {
		add("Review: not requested")
		return
	}
	status := "open"
	if req.Resolved {
		status = "closed: " + req.Resolution
	}
	add("Review request #%d at %s · %s ago · %s", req.ID, shortSHA(req.CommitSHA), fmtAge(now.Sub(req.CreatedAt)), status)
	if req.ReviewerOutcome != "" {
		line, _ := internal.FormatReviewerRun(req, now)
		add("Reviewer run: %s", line)
	}
}

func verdictLines(b *doc, reviews []internal.Review) {
	if len(reviews) == 0 {
		return
	}
	r := reviews[len(reviews)-1]
	line := fmt.Sprintf("Latest verdict: %s at %s", verdictLabel(r.Decision), orDash(shortSHA(r.CommitSHA)))
	if r.Reviewer != "" {
		line += " by " + r.Reviewer
	}
	b.add("%s", line)
	b.text("  ", "", r.Comment)
}

// liveLogTail returns the most recently written check log of the run that
// began at since.
func liveLogTail(app *internal.App, taskID string, since time.Time) (string, []string) {
	dir := internal.CheckLogDir(app.RepoRoot, taskID)
	var name string
	var newest time.Time
	for check := range app.Config.Checks {
		fi, err := os.Stat(filepath.Join(dir, check+".log"))
		if err != nil || !writtenSince(fi.ModTime(), since) || fi.ModTime().Before(newest) {
			continue
		}
		name, newest = check+".log", fi.ModTime()
	}
	if name == "" {
		return "", nil
	}
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return name, []string{"-"}
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > logTailBytes {
		f.Seek(-logTailBytes, io.SeekEnd)
	}
	data, _ := io.ReadAll(f)
	return name, lastLines(string(data), logTailLines)
}

func lastLines(s string, n int) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	// Check output may carry its own colours and \r progress redraws, which
	// would corrupt the pane; keep the text after the last \r.
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		if j := strings.LastIndex(l, "\r"); j >= 0 {
			l = l[j+1:]
		}
		lines[i] = strings.ReplaceAll(ansi.Strip(l), "\t", "    ")
	}
	return lines
}
