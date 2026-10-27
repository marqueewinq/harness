package internal

import "fmt"

func (a *App) CmdReviewHistory(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}
	fmt.Printf("Review type: %s\n\n", FormatReviewType(task.ReviewType, task.ReviewTypeSetBy, task.ReviewTypeSetAt))
	reviews, err := a.DB.GetReviews(id)
	if err != nil {
		return err
	}
	printReviews(reviews)
	runs, err := a.DB.ListReviewerRuns(id, "")
	if err != nil {
		return err
	}
	printReviewerRuns(runs)
	return nil
}
