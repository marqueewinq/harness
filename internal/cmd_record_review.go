package internal

import "fmt"

// openReviewRequestFor returns the open request for the subject and, when
// requireHead is set, refuses if branch moved since the request: the verdict
// must cover exactly the commit the reviewer was shown.
func (a *App) openReviewRequestFor(taskID, epicID, branch string, requireHead bool) (*ReviewRequest, error) {
	subject := taskID
	if epicID != "" {
		subject = epicID
	}
	req, err := a.DB.GetOpenReviewRequest(taskID, epicID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("no open review request for %s (run: harness review request %s)", subject, subject)
	}
	if !requireHead {
		return req, nil
	}
	head, err := a.Git.RevParse(branch)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", branch, err)
	}
	if head != req.CommitSHA {
		return nil, fmt.Errorf("%s moved since review was requested (%s -> %s); re-run checks and request review again",
			branch, shortSHA(req.CommitSHA), shortSHA(head))
	}
	return req, nil
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (a *App) CmdReviewApprove(taskID, comment string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	if task.State != StateReviewRequested {
		return fmt.Errorf("cannot record review in state %s (need REVIEW_REQUESTED)", task.State)
	}

	req, err := a.openReviewRequestFor(id, "", task.Branch, true)
	if err != nil {
		return err
	}
	if err := a.DB.ResolveReviewRequest(req, "approve", comment); err != nil {
		return err
	}
	if err := a.DB.UpdateTaskState(id, StateApproved); err != nil {
		return err
	}
	fmt.Printf("Review recorded: APPROVE (%s)\n", shortSHA(req.CommitSHA))
	fmt.Printf("Task %s is now APPROVED.\n", id)
	fmt.Println("Next: run harness work merge")

	return nil
}

func (a *App) CmdReviewReject(taskID, comment string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	if task.State != StateReviewRequested {
		return fmt.Errorf("cannot record review in state %s (need REVIEW_REQUESTED)", task.State)
	}

	if comment == "" {
		return fmt.Errorf("--comment is required for reject")
	}
	req, err := a.openReviewRequestFor(id, "", task.Branch, false)
	if err != nil {
		return err
	}
	if err := a.DB.ResolveReviewRequest(req, "request_changes", comment); err != nil {
		return err
	}
	if err := a.DB.UpdateTaskState(id, StateRequestChanges); err != nil {
		return err
	}
	fmt.Printf("Review recorded: REQUEST_CHANGES\n")
	fmt.Printf("Comment: %s\n", comment)
	fmt.Printf("Task %s is now REQUEST_CHANGES.\n", id)
	fmt.Println("Next: address the comments, then run: harness check")
	return nil
}
