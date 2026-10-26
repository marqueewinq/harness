package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/marqueewinq/harness/internal"
)

// BuildDiff shows what a reviewer judges: the diff of exactly the requested
// commit against the subject's base, with its context.
func BuildDiff(app *internal.App, row Row) (DiffView, error) {
	var (
		taskID, epicID, title, description, base string
		reviews                                  []internal.Review
		err                                      error
	)
	if row.IsEpic {
		e, err := app.DB.GetEpic(row.ID)
		if err != nil {
			return DiffView{}, err
		}
		epicID, title, description, base = e.ID, e.Title, e.Description, app.Config.MainBranch
		reviews, err = app.DB.GetEpicReviews(e.ID)
		if err != nil {
			return DiffView{}, err
		}
	} else {
		t, err := app.DB.GetTask(row.ID)
		if err != nil {
			return DiffView{}, err
		}
		taskID, title, description, base = t.ID, t.Title, t.Description, t.BaseBranch
		reviews, err = app.DB.GetReviews(t.ID)
		if err != nil {
			return DiffView{}, err
		}
	}
	req, err := app.DB.GetOpenReviewRequest(taskID, epicID)
	if err != nil {
		return DiffView{}, err
	}
	if req == nil {
		return DiffView{}, fmt.Errorf("%s has no open review request", row.ID)
	}

	v := DiffView{Key: row.Key(), SHA: req.CommitSHA, Title: fmt.Sprintf("%s %s · %s...%s", row.ID, flatten(title), base, shortSHA(req.CommitSHA))}
	add := func(format string, args ...interface{}) { v.Header = append(v.Header, fmt.Sprintf(format, args...)) }
	add("Review of %s at %s (requested %s), base %s", row.ID, shortSHA(req.CommitSHA), req.CreatedAt.Local().Format("2006-01-02 15:04"), base)
	if description != "" {
		add("Description: %s", flatten(description))
	}
	if taskID != "" {
		if err := checkLines(app, taskID, req.CommitSHA, add); err != nil {
			return DiffView{}, err
		}
	}
	commented := false
	for _, r := range reviews {
		if r.Comment == "" {
			continue
		}
		if !commented {
			add("Previous comments:")
			commented = true
		}
		add("  [%s at %s] %s", r.Decision, orDash(shortSHA(r.CommitSHA)), flatten(r.Comment))
	}
	add("")

	diff, err := app.Git.Diff(base, req.CommitSHA)
	if err != nil {
		return DiffView{}, fmt.Errorf("git diff %s...%s: %w", base, shortSHA(req.CommitSHA), err)
	}
	if diff != "" {
		for _, l := range strings.Split(diff, "\n") {
			v.Diff = append(v.Diff, ansi.Strip(strings.ReplaceAll(strings.TrimRight(l, "\r"), "\t", "    ")))
		}
	}
	return v, nil
}

// flatten makes user text safe for one pane line: no escapes or line breaks.
func flatten(s string) string {
	s = ansi.Strip(s)
	return strings.Join(strings.Fields(s), " ")
}

func checkLines(app *internal.App, taskID, sha string, add func(string, ...interface{})) error {
	runs, err := app.DB.GetLatestCheckRuns(taskID)
	if err != nil {
		return err
	}
	if len(runs) > 0 && runs[0].CommitSHA == sha {
		var parts []string
		for _, r := range runs {
			switch r.ExitCode {
			case 0:
				parts = append(parts, r.CheckName+" PASS")
			case -1:
				parts = append(parts, r.CheckName+" SKIP")
			default:
				parts = append(parts, fmt.Sprintf("%s FAIL (exit %d)", r.CheckName, r.ExitCode))
			}
		}
		add("Checks on %s: %s", shortSHA(sha), strings.Join(parts, ", "))
		return nil
	}
	passed, err := app.DB.PassedChecksAt(taskID, sha)
	if err != nil {
		return err
	}
	if len(passed) == 0 {
		add("Checks on %s: none recorded", shortSHA(sha))
		return nil
	}
	names := make([]string, 0, len(passed))
	for n := range passed {
		names = append(names, n)
	}
	sort.Strings(names)
	add("Checks passed on %s: %s", shortSHA(sha), strings.Join(names, ", "))
	return nil
}
