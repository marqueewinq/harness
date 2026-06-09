package internal

import "fmt"

func (a *App) CmdCheck(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	checkableStates := map[TaskState]bool{
		StateInProgress:      true,
		StateChecksFailed:    true,
		StateRequestChanges:  true,
		StateReviewRequested: true,
		StateMergeFailed:     true,
	}
	if !checkableStates[task.State] {
		return fmt.Errorf("cannot run checks in state %s (allowed: IN_PROGRESS, CHECKS_FAILED, REQUEST_CHANGES, REVIEW_REQUESTED, MERGE_FAILED)", task.State)
	}

	if err := a.DB.UpdateTaskState(id, StateChecksRunning); err != nil {
		return err
	}

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

	allPassed := true
	for _, r := range results {
		exitCode := r.ExitCode
		if r.Skipped {
			exitCode = -1
		}
		a.DB.RecordCheckRun(id, r.Name, exitCode, r.Stdout, r.Stderr, runGroup, r.StartedAt, r.FinishedAt)

		if r.Skipped {
			fmt.Printf("  %-12s SKIP\n", r.Name)
		} else if r.ExitCode == 0 {
			fmt.Printf("  %-12s PASS  (%.1fs)\n", r.Name, r.Duration.Seconds())
		} else {
			fmt.Printf("  %-12s FAIL  (%.1fs)\n", r.Name, r.Duration.Seconds())
			allPassed = false
		}
	}

	if allPassed {
		a.DB.UpdateTaskState(id, StateChecksPassed)
		fmt.Println("\nAll checks passed.")
		fmt.Println("Next: run harness request-review")
	} else {
		a.DB.UpdateTaskState(id, StateChecksFailed)
		fmt.Println("\nChecks failed.")
		fmt.Println("Next: fix the failures, then run: harness check")
	}
	return nil
}
