package internal

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	// QueueLeaseTTL must exceed queueHeartbeatEvery by a wide margin: a live
	// merger renews its lease every queueHeartbeatEvery.
	QueueLeaseTTL       = 60 * time.Second
	queueHeartbeatEvery = 10 * time.Second
	queuePollEvery      = 2 * time.Second
)

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// PIDs are checked on this machine, so all mergers must share one host (the
// DB lives in the local checkout). A live PID that stopped renewing is hung.
func leaseStale(e QueueEntry, now time.Time) bool {
	if e.PID == os.Getpid() {
		return false
	}
	return !pidAlive(e.PID) || now.Sub(e.Heartbeat) > QueueLeaseTTL
}

func (a *App) startHeartbeat(taskID string) (stop func()) {
	done := make(chan struct{})
	pid := os.Getpid()
	go func() {
		t := time.NewTicker(queueHeartbeatEvery)
		defer t.Stop()
		warned := false
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := a.DB.Heartbeat(taskID, pid); err != nil && !warned {
					fmt.Fprintf(os.Stderr, "Warning: renewing merge queue lease for %s failed: %v\n", taskID, err)
					warned = true
				}
			}
		}
	}()
	return func() { close(done) }
}

// reclaimStaleEntries removes every dead or expired lease, not only the head,
// so an abandoned row anywhere in the queue never outlives its owner.
func (a *App) reclaimStaleEntries() error {
	entries, err := a.DB.ListQueue()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, e := range entries {
		if !leaseStale(e, now) {
			continue
		}
		removed, err := a.DB.RemoveQueueEntryIfUnchanged(e)
		if err != nil {
			return err
		}
		if !removed {
			continue
		}
		fmt.Printf("Reclaimed stale merge queue entry %s (pid %d, last heartbeat %s).\n",
			e.TaskID, e.PID, e.Heartbeat.Format(time.RFC3339))
		a.markAbandonedMerge(e.TaskID)
	}
	return nil
}

func (a *App) markAbandonedMerge(taskID string) {
	task, err := a.DB.GetTask(taskID)
	if err != nil {
		return
	}
	if task.State == StateQueued || task.State == StateMerging {
		if err := a.DB.UpdateTaskState(taskID, StateMergeFailed); err == nil {
			fmt.Printf("Task %s is now MERGE_FAILED. If its worktree has an unfinished rebase, run: harness work sync --abort %s; then: harness work check %s\n", taskID, taskID, taskID)
		}
	}
}

// recoverAbandonedMerge handles a task found QUEUED/MERGING at the start of a
// merge: a live lease means another process owns it; otherwise the merger
// died (or predates leases) and the task is released as MERGE_FAILED.
func (a *App) recoverAbandonedMerge(id string) error {
	entries, err := a.DB.ListQueue()
	if err != nil {
		return err
	}
	now := time.Now()
	for _, e := range entries {
		if e.TaskID != id {
			continue
		}
		if !leaseStale(e, now) {
			return fmt.Errorf("%s is already being merged by process %d", id, e.PID)
		}
		if _, err := a.DB.RemoveQueueEntryIfUnchanged(e); err != nil {
			return err
		}
	}
	a.markAbandonedMerge(id)
	return fmt.Errorf("recovered abandoned merge of %s (its merger is gone); re-run checks, then merge again", id)
}

func (a *App) CmdQueueList() error {
	entries, err := a.DB.ListQueue()
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("Merge queue is empty.")
		return nil
	}
	now := time.Now()
	fmt.Printf("%-4s %-10s %-8s %-10s %s\n", "#", "Task", "PID", "Heartbeat", "Status")
	for i, e := range entries {
		status := "live"
		if leaseStale(e, now) {
			status = "stale"
		}
		fmt.Printf("%-4d %-10s %-8d %-10s %s\n", i+1, e.TaskID, e.PID, now.Sub(e.Heartbeat).Truncate(time.Second).String()+" ago", status)
	}
	return nil
}

// CmdQueueClear removes taskID from the queue. A live lease needs force,
// because its owner is mid-merge.
func (a *App) CmdQueueClear(taskID string, force bool) error {
	taskID, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	entries, err := a.DB.ListQueue()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.TaskID != taskID {
			continue
		}
		if !leaseStale(e, time.Now()) && !force {
			return fmt.Errorf("%s is held by live process %d; pass --force to remove it anyway", taskID, e.PID)
		}
		if err := a.DB.DequeueTask(taskID); err != nil {
			return err
		}
		fmt.Printf("Removed %s from the merge queue.\n", taskID)
		a.markAbandonedMerge(taskID)
		return nil
	}
	if task, err := a.DB.GetTask(taskID); err == nil && (task.State == StateQueued || task.State == StateMerging) {
		fmt.Printf("%s is not in the merge queue but is stuck in %s.\n", taskID, task.State)
		a.markAbandonedMerge(taskID)
		return nil
	}
	return fmt.Errorf("%s is not in the merge queue", taskID)
}
