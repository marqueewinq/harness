package internal

import (
	"fmt"
	"strings"
)

func (a *App) cmdEpicReviewRequest(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}

	diff, _ := a.Git.Diff(a.Config.MainBranch, e.Branch)
	reviews, _ := a.DB.GetEpicReviews(epicID)
	prevComments := FormatPreviousComments(reviews)

	fmt.Printf("Review requested for epic %s.\n\n", epicID)
	fmt.Printf("Diff (%s..%s):\n%s\n\n", a.Config.MainBranch, e.Branch, diff)
	fmt.Printf("Epic title: %s\n\n", e.Title)
	fmt.Printf("Previous review comments:\n  %s\n\n", prevComments)

	if a.Config.Review.Prompt != "" {
		prompt := a.Config.Review.Prompt
		prompt = strings.ReplaceAll(prompt, "{{previous_comments}}", prevComments)
		prompt = strings.ReplaceAll(prompt, "{{task_description}}", e.Title)
		fmt.Printf("Review prompt:\n%s\n", prompt)
	}

	fmt.Println("---")
	if a.Config.Review.Type == "agent" {
		fmt.Println("For agent review: spawn a reviewer subagent with the above context.")
		fmt.Printf("  The reviewer must call: harness review approve %s\n", epicID)
		fmt.Printf("  or: harness review reject --comment \"...\" %s\n", epicID)
	} else {
		fmt.Println("For human review, in another terminal run:")
		fmt.Printf("  harness review approve %s\n", epicID)
	}
	return nil
}

func (a *App) cmdEpicReviewApprove(epicID, comment string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}

	sha, err := a.Git.RevParse(e.Branch)
	if err != nil {
		sha = ""
	}
	if err := a.DB.RecordEpicReview(epicID, "approve", comment, sha); err != nil {
		return err
	}
	fmt.Printf("Review recorded: APPROVE for epic %s\n", epicID)
	fmt.Println("Next: run harness epic merge " + epicID)
	return nil
}

func (a *App) cmdEpicReviewReject(epicID, comment string) error {
	if comment == "" {
		return fmt.Errorf("--comment is required for reject")
	}
	if err := a.DB.RecordEpicReview(epicID, "request_changes", comment, ""); err != nil {
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
	if len(reviews) == 0 {
		fmt.Println("No reviews yet.")
		return nil
	}
	for i, r := range reviews {
		fmt.Printf("Review #%d (%s):\n", i+1, r.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("  Decision: %s\n", r.Decision)
		if r.Comment != "" {
			fmt.Printf("  Comment: %s\n", r.Comment)
		}
		fmt.Println()
	}
	return nil
}
