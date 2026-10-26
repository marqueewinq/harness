package internal

func (a *App) CmdReviewHistory(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
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
