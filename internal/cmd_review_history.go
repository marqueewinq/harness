package internal

import "fmt"

func (a *App) CmdReviewHistory(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	reviews, err := a.DB.GetReviews(id)
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
