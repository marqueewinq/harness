package internal

import "fmt"

func (a *App) CmdNew(title, baseBranch, epicID, description, review string) error {
	reviewType, reviewSetBy, err := a.creationReviewType(review)
	if err != nil {
		return err
	}
	baseBranch = a.resolveBaseBranch(baseBranch, epicID)
	slug := Slugify(title)
	branch, worktreePath, parentTaskID, err := a.createTaskBranch(slug, baseBranch)
	if err != nil {
		return err
	}

	task, err := a.DB.CreateTask(title, description, branch, worktreePath, baseBranch, parentTaskID, epicID)
	if err != nil {
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating task: %w", err)
	}
	if err := a.DB.SetReviewType(task.ID, reviewType, reviewSetBy); err != nil {
		return fmt.Errorf("created %s, but its review type stays default: %w", task.ID, err)
	}

	fmt.Printf("Created %s: %q\n", task.ID, title)
	fmt.Printf("Review type: %s\n", reviewType)
	fmt.Printf("Branch: %s\n", branch)
	baseInfo := baseBranch
	if parentTaskID != "" {
		baseInfo = fmt.Sprintf("%s (%s)", baseBranch, parentTaskID)
	}
	fmt.Printf("Base: %s\n", baseInfo)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("\nNext: implement the changes in %s/\n", worktreePath)
	fmt.Println("      then run: harness work check")
	return nil
}
