package internal

import (
	"fmt"
	"time"
)

func (a *App) cmdEpicReviewRequest(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	a.showNewEpicNotes(epicID)
	if e.ReviewType == ReviewNone {
		return refuseReviewOfNone(epicID, "harness epic merge "+epicID)
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}
	review, err := a.trustedReviewConfig()
	if err != nil {
		return err
	}

	head, err := a.Git.RevParse(e.Branch)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", e.Branch, err)
	}
	review, token, reqID, err := a.openReviewFor("", epicID, head, e.ReviewType, review)
	if err != nil {
		return err
	}

	diff, _ := a.Git.Diff(a.Config.MainBranch, e.Branch)
	reviews, _ := a.DB.GetEpicReviews(epicID)
	prevComments := FormatPreviousComments(reviews)

	fmt.Printf("Review requested for epic %s (%s).\n\n", epicID, shortSHA(head))
	stopped, err := a.deliverReview(review, reqID, epicID, e.Title, e.Notes, diff, prevComments, token, "run: harness review request "+epicID)
	if stopped != "" {
		fmt.Print(stoppedByHumanReport(stopped, "epic "+epicID, "run harness epic merge "+epicID))
	}
	return err
}

func (a *App) cmdEpicReviewApprove(epicID, comment string) error {
	return a.epicVerdict(epicID, "approve", comment, a.authorizeVerdict)
}

func (a *App) cmdEpicReviewReject(epicID, comment string) error {
	return a.epicVerdict(epicID, "request_changes", comment, a.authorizeVerdict)
}

func (a *App) epicVerdict(epicID, decision, comment string, auth verdictAuth) error {
	report, err := a.epicVerdictReport(epicID, decision, comment, "", auth)
	fmt.Print(report)
	return err
}

func (a *App) epicVerdictReport(epicID, decision, comment, expectSHA string, auth verdictAuth) (string, error) {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return "", err
	}
	if decision == "approve" && e.Branch == "" {
		return "", fmt.Errorf("epic %s has no branch", epicID)
	}
	req, err := a.recordVerdict("", epicID, e.Branch, decision, comment, expectSHA, auth)
	if err != nil {
		return "", err
	}
	if decision == "approve" {
		return fmt.Sprintf("Review recorded: APPROVE for epic %s (%s)\nNext: run harness epic merge %s\n",
			epicID, shortSHA(req.CommitSHA), epicID), nil
	}
	return fmt.Sprintf("Review recorded: REQUEST_CHANGES for epic %s\nComment: %s\n", epicID, comment), nil
}

func (a *App) cmdEpicReviewHistory(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	fmt.Printf("Review type: %s\n\n", FormatReviewType(e.ReviewType, e.ReviewTypeSetBy, e.ReviewTypeSetAt))
	reviews, err := a.DB.GetEpicReviews(epicID)
	if err != nil {
		return err
	}
	printReviews(reviews)
	runs, err := a.DB.ListReviewerRuns("", epicID)
	if err != nil {
		return err
	}
	printReviewerRuns(runs)
	return nil
}

func FormatReviewerRun(r *ReviewRequest, now time.Time) (line, status string) {
	status = r.RunStatus(now)
	s := fmt.Sprintf("%s (request #%d, commit %s, pid %d", status, r.ID, shortSHA(r.CommitSHA), r.ReviewerPID)
	if !r.ReviewerStarted.IsZero() {
		s += ", started " + r.ReviewerStarted.Format("2006-01-02 15:04:05")
	}
	switch {
	case !r.ReviewerFinished.IsZero():
		if !r.ReviewerStarted.IsZero() {
			s += ", ran " + r.ReviewerFinished.Sub(r.ReviewerStarted).String()
		}
		s += fmt.Sprintf(", exit %d", r.ReviewerExitCode)
	case !r.ReviewerHeartbeat.IsZero():
		s += ", last heartbeat " + now.Sub(r.ReviewerHeartbeat).Truncate(time.Second).String() + " ago"
	}
	return s + ")", status
}

func printReviewerRuns(runs []ReviewRequest) {
	if len(runs) == 0 {
		return
	}
	now := time.Now()
	fmt.Println("Reviewer runs:")
	for i := range runs {
		line, _ := FormatReviewerRun(&runs[i], now)
		fmt.Printf("  %s\n", line)
	}
}

func printReviews(reviews []Review) {
	if len(reviews) == 0 {
		fmt.Println("No reviews yet.")
		return
	}
	for i, r := range reviews {
		fmt.Printf("Review #%d (%s):\n", i+1, r.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("  Decision: %s\n", r.Decision)
		if r.CommitSHA != "" {
			fmt.Printf("  Commit: %s\n", shortSHA(r.CommitSHA))
		}
		if r.Reviewer != "" {
			fmt.Printf("  Reviewer: %s\n", r.Reviewer)
		}
		if r.Comment != "" {
			fmt.Printf("  Comment: %s\n", r.Comment)
		}
		fmt.Println()
	}
}
