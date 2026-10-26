package internal

import "fmt"

func (a *App) CmdWorkCheck(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	if !ValidTransition(task.State, StateChecksRunning) {
		return fmt.Errorf("cannot run checks in state %s", task.State)
	}

	if err := a.DB.UpdateTaskState(id, StateChecksRunning); err != nil {
		return err
	}

	// Resolve commit SHA from the worktree HEAD
	worktreeGit := NewGitClient(task.WorktreePath)
	commitSHA, _ := worktreeGit.RevParse("HEAD")

	fmt.Println("Running checks...")
	runner := NewCheckRunner(a.Config.Checks, task.WorktreePath)
	results, err := runner.RunAll()
	if err != nil {
		a.DB.UpdateTaskState(id, StateChecksFailed)
		return fmt.Errorf("check execution error: %w", err)
	}

	runGroup, err := a.DB.NextRunGroup(id)
	if err != nil {
		return err
	}

	for _, r := range results {
		exitCode := r.ExitCode
		if r.Skipped {
			exitCode = -1
		}
		if err := a.DB.RecordCheckRun(id, r.Name, exitCode, r.Stdout, r.Stderr, runGroup, r.StartedAt, r.FinishedAt, commitSHA); err != nil {
			a.DB.UpdateTaskState(id, StateChecksFailed)
			return fmt.Errorf("recording check %s: %w", r.Name, err)
		}
	}

	allPassed := PrintCheckResults(results)

	if !allPassed {
		if err := a.DB.UpdateTaskState(id, StateChecksFailed); err != nil {
			return err
		}
		fmt.Println("\nChecks failed.")
		fmt.Println("Next: fix the failures, then run: harness check")
		return fmt.Errorf("checks failed for %s", id)
	}
	if err := a.DB.UpdateTaskState(id, StateChecksPassed); err != nil {
		return err
	}
	fmt.Println("\nAll checks passed.")
	fmt.Println("Next: run harness review request")
	return nil
}
