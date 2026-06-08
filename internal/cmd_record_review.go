package internal

import "fmt"

func (a *App) CmdRecordReview(taskID, decision, comment string) error {
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

	switch decision {
	case "approve":
		if err := a.DB.RecordReview(id, "approve", comment); err != nil {
			return err
		}
		if err := a.DB.UpdateTaskState(id, StateApproved); err != nil {
			return err
		}
		fmt.Printf("Review recorded: APPROVE\n")
		fmt.Printf("Task %s is now APPROVED.\n", id)
		fmt.Println("Next: run harness merge")
	case "request-changes":
		if comment == "" {
			return fmt.Errorf("--comment is required for request-changes")
		}
		if err := a.DB.RecordReview(id, "request_changes", comment); err != nil {
			return err
		}
		if err := a.DB.UpdateTaskState(id, StateRequestChanges); err != nil {
			return err
		}
		fmt.Printf("Review recorded: REQUEST_CHANGES\n")
		fmt.Printf("Comment: %s\n", comment)
		fmt.Printf("Task %s is now REQUEST_CHANGES.\n", id)
		fmt.Println("Next: address the comments, then run: harness check")
	default:
		return fmt.Errorf("unknown decision %q (use 'approve' or 'request-changes')", decision)
	}
	return nil
}
