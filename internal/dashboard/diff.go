package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/marqueewinq/harness/internal"
)

type diffSubject struct {
	taskID, epicID string
	title, desc    string
	base, branch   string
	state          internal.TaskState
	reviews        []internal.Review
}

func loadDiffSubject(app *internal.App, row Row) (diffSubject, error) {
	if row.IsEpic {
		e, err := app.DB.GetEpic(row.ID)
		if err != nil {
			return diffSubject{}, err
		}
		reviews, err := app.DB.GetEpicReviews(e.ID)
		if err != nil {
			return diffSubject{}, err
		}
		return diffSubject{epicID: e.ID, title: e.Title, desc: e.Description, base: app.Config.MainBranch, branch: e.Branch, reviews: reviews}, nil
	}
	t, err := app.DB.GetTask(row.ID)
	if err != nil {
		return diffSubject{}, err
	}
	reviews, err := app.DB.GetReviews(t.ID)
	if err != nil {
		return diffSubject{}, err
	}
	return diffSubject{taskID: t.ID, title: t.Title, desc: t.Description, base: t.BaseBranch, branch: t.Branch, state: t.State, reviews: reviews}, nil
}

// BuildDiff shows the subject's diff. With an open review request it is
// exactly the requested commit against the base (SHA set: what a verdict
// judges); otherwise the branch head against the base, for information only.
func BuildDiff(app *internal.App, row Row) (DiffView, error) {
	sub, err := loadDiffSubject(app, row)
	if err != nil {
		return DiffView{}, err
	}
	req, err := app.DB.GetOpenReviewRequest(sub.taskID, sub.epicID)
	if err != nil {
		return DiffView{}, err
	}

	v := DiffView{Key: row.Key()}
	var b doc
	var target string
	switch {
	case req != nil:
		v.SHA, target = req.CommitSHA, req.CommitSHA
		v.Range = sub.base + "..." + shortSHA(req.CommitSHA)
		b.add("Requested commit %s (requested %s), base %s", shortSHA(req.CommitSHA), req.CreatedAt.Local().Format("2006-01-02 15:04"), sub.base)
	case sub.epicID == "" && (sub.state == internal.StatePlanned || sub.branch == ""):
		b.add("Not started; nothing to diff.")
	case sub.epicID != "" && sub.branch == "":
		b.add("Epic has no branch; nothing to diff.")
	default:
		head, err := app.Git.RevParse("refs/heads/" + sub.branch)
		if err != nil {
			if sub.state == internal.StateMerged || sub.state == internal.StateClosed {
				b.add("Branch %s deleted (%s); nothing to diff.", sub.branch, sub.state)
			} else {
				b.add("Branch %s not found; nothing to diff.", sub.branch)
			}
			break
		}
		target = head
		v.Range = fmt.Sprintf("%s...%s @ %s", sub.base, sub.branch, shortSHA(head))
		b.add("Branch head %s...%s @ %s (no open review request)", sub.base, sub.branch, shortSHA(head))
	}
	v.Title = row.ID + " " + flatten(sub.title)
	if v.Range != "" {
		v.Title += " · " + v.Range
	}
	b.text("", "Description: ", sub.desc)
	if sub.taskID != "" && target != "" {
		if err := checkLines(app, sub.taskID, target, b.add); err != nil {
			return DiffView{}, err
		}
	}
	commented := false
	for _, r := range sub.reviews {
		if strings.TrimSpace(r.Comment) == "" {
			continue
		}
		if !commented {
			b.add("Previous comments:")
			commented = true
		}
		b.text("  ", fmt.Sprintf("[%s at %s] ", r.Decision, orDash(shortSHA(r.CommitSHA))), r.Comment)
	}
	b.add("")
	v.Header, v.HeaderSpans = b.lines, b.spans

	if target == "" {
		return v, nil
	}
	diff, err := app.Git.Diff(sub.base, target)
	if err != nil {
		return DiffView{}, fmt.Errorf("git diff %s...%s: %w", sub.base, shortSHA(target), err)
	}
	if diff != "" {
		for _, l := range strings.Split(diff, "\n") {
			v.Diff = append(v.Diff, ansi.Strip(strings.ReplaceAll(strings.TrimRight(l, "\r"), "\t", "    ")))
		}
	}
	return v, nil
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
