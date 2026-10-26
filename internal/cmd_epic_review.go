package internal

import "fmt"

func (a *App) cmdEpicReviewRequest(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
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
	token, err := a.DB.OpenReviewRequest("", epicID, head)
	if err != nil {
		return err
	}

	diff, _ := a.Git.Diff(a.Config.MainBranch, e.Branch)
	reviews, _ := a.DB.GetEpicReviews(epicID)
	prevComments := FormatPreviousComments(reviews)

	fmt.Printf("Review requested for epic %s (%s).\n\n", epicID, shortSHA(head))
	return a.deliverReview(review, epicID, e.Title, diff, prevComments, token)
}

func (a *App) cmdEpicReviewApprove(epicID, comment string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}

	req, err := a.recordVerdict("", epicID, e.Branch, "approve", comment)
	if err != nil {
		return err
	}
	fmt.Printf("Review recorded: APPROVE for epic %s (%s)\n", epicID, shortSHA(req.CommitSHA))
	fmt.Println("Next: run harness epic merge " + epicID)
	return nil
}

func (a *App) cmdEpicReviewReject(epicID, comment string) error {
	if _, err := a.recordVerdict("", epicID, "", "request_changes", comment); err != nil {
		return err
	}
	fmt.Printf("Review recorded: REQUEST_CHANGES for epic %s\n", epicID)
	fmt.Printf("Comment: %s\n", comment)
	return nil
}

func (a *App) cmdEpicReviewHistory(epicID string) error {
	reviews, err := a.DB.GetEpicReviews(epicID)
	if err != nil {
		return err
	}
	printReviews(reviews)
	return nil
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
