package internal

import "fmt"

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

	sha, err := a.Git.RevParse(task.Branch)
	if err != nil {
		sha = ""
	}
	if err := a.DB.RecordReview(id, "approve", comment, sha); err != nil {
		return err
	}
	if err := a.DB.UpdateTaskState(id, StateApproved); err != nil {
		return err
	}
	fmt.Printf("Review recorded: APPROVE\n")
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
	if err := a.DB.RecordReview(id, "request_changes", comment, ""); err != nil {
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
