package internal

import (
	"fmt"
	"strings"
)

func (a *App) CmdEpicNew(title, description, projectID, branchFlag string, branchFlagSet bool) error {
	if projectID == "" {
		return fmt.Errorf("--project is required")
	}
	p, err := a.DB.GetProject(projectID)
	if err != nil {
		return err
	}
	if p.Status == ProjectStatusClosed {
		fmt.Printf("Warning: project %s is closed\n", projectID)
	}

	// Determine branch
	var branch string
	if branchFlagSet {
		// Explicit --branch flag
		branch = branchFlag
		if branch != "" && branch != a.Config.MainBranch {
			// Validate branch exists
			if _, err := a.Git.RevParse(branch); err != nil {
				return fmt.Errorf("branch %q does not exist", branch)
			}
		}
	} else if a.Config.EpicFeatureBranchesEnabled() {
		// Default: auto-create epic/<slug> branch
		branch = "epic/" + Slugify(title)
		if err := a.Git.CreateBranch(branch, a.Config.MainBranch); err != nil {
			return fmt.Errorf("creating epic branch: %w", err)
		}
	}
	// else: epic_feature_branches: false + no --branch -> no branch

	e, err := a.DB.CreateEpic(title, description, projectID, branch)
	if err != nil {
		// Clean up branch if we created it
		if branch != "" && !branchFlagSet {
			a.Git.DeleteBranch(branch)
		}
		return err
	}
	fmt.Printf("Created %s: %q (%s)\n", e.ID, e.Title, projectID)
	if branch != "" {
		fmt.Printf("Branch: %s\n", branch)
	}
	return nil
}

func (a *App) CmdEpicList(projectID string) error {
	var epics []Epic
	var err error
	if projectID != "" {
		epics, err = a.DB.ListEpicsByProject(projectID)
	} else {
		epics, err = a.DB.ListEpics()
	}
	if err != nil {
		return err
	}
	if len(epics) == 0 {
		fmt.Println("No epics. Run: harness epic new \"title\" --project <id>")
		return nil
	}
	fmt.Printf("%-10s %-10s %-8s %-20s %s\n", "ID", "Project", "Status", "Branch", "Title")
	for _, e := range epics {
		tasks, _ := a.DB.ListTasksByEpic(e.ID)
		done, total := CountDone(tasks)
		progress := ""
		if total > 0 {
			progress = fmt.Sprintf(" (%d/%d)", done, total)
		}
		branchDisplay := "-"
		if e.Branch != "" {
			branchDisplay = e.Branch
		}
		fmt.Printf("%-10s %-10s %-8s %-20s %s%s\n", e.ID, e.ProjectID, e.Status, branchDisplay, e.Title, progress)
	}
	return nil
}

func (a *App) CmdEpicView(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	p, _ := a.DB.GetProject(e.ProjectID)
	fmt.Printf("Epic: %s %q\n", e.ID, e.Title)
	fmt.Printf("Project: %s %q\n", p.ID, p.Title)
	fmt.Printf("Status: %s\n", e.Status)
	if e.Branch != "" {
		fmt.Printf("Branch: %s\n", e.Branch)
	}
	if e.Description != "" {
		fmt.Printf("Description: %s\n", e.Description)
	}
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	if len(tasks) == 0 {
		fmt.Printf("\nNo tasks. Run: harness plan \"description\" --epic %s\n", e.ID)
		return nil
	}
	fmt.Println("\nTasks:")
	for _, t := range tasks {
		fmt.Printf("  %-10s %-20s %s\n", t.ID, t.State, t.Title)
	}
	return nil
}

func (a *App) CmdEpicClose(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	tasks, _ := a.DB.ListTasksByEpic(e.ID)
	done, total := CountDone(tasks)
	if openCount := total - done; openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this epic\n", openCount)
	}
	if err := a.DB.UpdateEpicStatus(epicID, EpicStatusClosed); err != nil {
		return err
	}
	fmt.Printf("Closed %s: %q\n", e.ID, e.Title)
	return nil
}

func (a *App) CmdEpicEdit(epicID, title, description, projectID string, setTitle, setDescription, setProject bool) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Status == EpicStatusClosed {
		return fmt.Errorf("cannot edit closed epic %s", epicID)
	}
	newTitle := e.Title
	if setTitle {
		newTitle = title
	}
	newDesc := e.Description
	if setDescription {
		newDesc = description
	}
	newProject := e.ProjectID
	if setProject {
		newProject = projectID
		if newProject != "" {
			if _, err := a.DB.GetProject(newProject); err != nil {
				return err
			}
		}
	}
	if err := a.DB.UpdateEpic(epicID, newTitle, newDesc, newProject); err != nil {
		return err
	}
	fmt.Printf("Updated %s: %q\n", epicID, newTitle)
	return nil
}

func (a *App) CmdEpicMerge(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}
	if e.Branch == a.Config.MainBranch {
		return fmt.Errorf("epic %s branch is %s (nothing to merge)", epicID, a.Config.MainBranch)
	}

	// Check for approval
	latestApproval, err := a.DB.GetLatestEpicApproval(epicID)
	if err != nil {
		return err
	}
	if latestApproval == nil {
		return fmt.Errorf("epic %s has no approval. Run: harness review request %s", epicID, epicID)
	}

	// Staleness check
	currentHead, err := a.Git.RevParse(e.Branch)
	if err != nil {
		return fmt.Errorf("cannot get HEAD of %s: %w", e.Branch, err)
	}
	if latestApproval.CommitSHA != "" && currentHead != latestApproval.CommitSHA {
		fmt.Printf("Review is stale: approved commit %s but branch is now at %s\n",
			latestApproval.CommitSHA[:8], currentHead[:8])
		fmt.Printf("Run: harness review request %s\n", epicID)
		return nil
	}

	// Warn about open tasks
	tasks, _ := a.DB.ListTasksByEpic(epicID)
	done, total := CountDone(tasks)
	if openCount := total - done; openCount > 0 {
		fmt.Printf("Warning: %d tasks are still open under this epic\n", openCount)
	}

	// Enqueue in merge queue (use epic ID as queue key)
	// We use a temp task-like entry; just inline the queue logic
	fmt.Printf("Merging epic %s branch %s into %s...\n", epicID, e.Branch, a.Config.MainBranch)

	strategy, err := NewMergeStrategy(a.Config.Merge.Strategy)
	if err != nil {
		return err
	}

	// Create temp worktree for epic branch
	tmpWorktree := fmt.Sprintf("%s/.worktrees/_epic-merge-%s", a.RepoRoot, epicID)
	if err := a.Git.CreateWorktree(e.Branch, tmpWorktree); err != nil {
		return fmt.Errorf("creating temp worktree: %w", err)
	}

	// Prepare (rebase onto main)
	wtGit := NewGitClient(tmpWorktree)
	fmt.Printf("Preparing %s for %s merge into %s...\n", e.Branch, strategy.Name(), a.Config.MainBranch)
	if err := strategy.Prepare(wtGit, a.Config.MainBranch); err != nil {
		wtGit.RebaseAbort()
		a.Git.RemoveWorktree(tmpWorktree)
		fmt.Printf("Prepare failed: %s\n", err)
		return nil
	}

	// Rerun checks if configured
	if a.Config.Merge.RerunChecks {
		fmt.Println("Re-running checks...")
		runner := NewCheckRunner(a.Config.Checks, tmpWorktree)
		results, err := runner.RunAll()
		if err != nil {
			a.Git.RemoveWorktree(tmpWorktree)
			return fmt.Errorf("check execution error: %w", err)
		}
		allPassed := PrintCheckResults(results)
		if !allPassed {
			a.Git.RemoveWorktree(tmpWorktree)
			fmt.Println("Checks failed. Fix and retry.")
			return nil
		}
	}

	// Merge into main
	fmt.Printf("Merging %s into %s (strategy: %s)...\n", e.Branch, a.Config.MainBranch, strategy.Name())
	if err := a.Git.Checkout(a.Config.MainBranch); err != nil {
		a.Git.RemoveWorktree(tmpWorktree)
		return fmt.Errorf("checkout %s: %w", a.Config.MainBranch, err)
	}
	if err := strategy.Merge(a.Git, e.Branch, a.Config.MainBranch, e.Title); err != nil {
		a.Git.RemoveWorktree(tmpWorktree)
		fmt.Printf("Merge failed: %s\n", err)
		return nil
	}

	// Cleanup
	a.Git.RemoveWorktree(tmpWorktree)
	a.Git.DeleteBranch(e.Branch)
	a.DB.UpdateEpicBranch(epicID, "")
	a.DB.UpdateEpicStatus(epicID, EpicStatusClosed)

	fmt.Println("Merge successful.")
	fmt.Printf("Epic %s is now closed.\n", epicID)
	fmt.Printf("Branch %s deleted.\n", e.Branch)
	return nil
}

// --- Review dispatching ---

func (a *App) CmdReviewRequestDispatch(id string) error {
	if isEpicID(id) {
		return a.cmdEpicReviewRequest(id)
	}
	return a.CmdReviewRequest(id)
}

func (a *App) CmdReviewApproveDispatch(id, comment string) error {
	if isEpicID(id) {
		return a.cmdEpicReviewApprove(id, comment)
	}
	return a.CmdReviewApprove(id, comment)
}

func (a *App) CmdReviewRejectDispatch(id, comment string) error {
	if isEpicID(id) {
		return a.cmdEpicReviewReject(id, comment)
	}
	return a.CmdReviewReject(id, comment)
}

func (a *App) CmdReviewHistoryDispatch(id string) error {
	if isEpicID(id) {
		return a.cmdEpicReviewHistory(id)
	}
	return a.CmdReviewHistory(id)
}

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

// CmdEpicDiff shows diff of epic branch vs main.
func (a *App) CmdEpicDiff(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}
	diff, err := a.Git.Diff(a.Config.MainBranch, e.Branch)
	if err != nil {
		return err
	}
	fmt.Println(diff)
	return nil
}

// CmdDiffDispatch dispatches diff to epic or task.
func (a *App) CmdDiffDispatch(id string) error {
	if isEpicID(id) {
		return a.CmdEpicDiff(id)
	}
	return a.CmdWorkDiff(id)
}


