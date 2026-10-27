package internal

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A note added while the merge waits in the queue must stop it, like a moved
// branch does: the approval (or check run) did not cover it.
func TestMergeRefusesNotesAddedWhileQueued(t *testing.T) {
	t.Parallel()
	for name, setup := range map[string]func(t *testing.T) (*App, *Task){
		"approved": approvedTask,
		"review none": func(t *testing.T) (*App, *Task) {
			app, _, task := checkedNoneTask(t)
			return app, task
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app, task := setup(t)
			app.Config.Merge.RerunChecks = false
			app.Config.Merge.QueueTimeout = 60
			insertQueueRow(t, app, "task-999", os.Getppid(), time.Now())
			mainBefore, err := app.Git.RevParse("main")
			if err != nil {
				t.Fatal(err)
			}

			done := make(chan error, 1)
			go func() { done <- app.CmdWorkMerge(task.ID) }()
			for i := 0; ; i++ {
				if pos, _ := app.DB.GetQueuePosition(task.ID); pos == 2 {
					break
				}
				if i > 100 {
					t.Fatal("merge never queued")
				}
				time.Sleep(50 * time.Millisecond)
			}

			editNotes(t, app, task.ID, "do not merge yet: handle the empty input case")
			if err := app.DB.DequeueTask("task-999"); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "while waiting in the merge queue") {
					t.Fatalf("err = %v, want notes refusal while queued", err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("merge did not finish")
			}
			mainAfter, err := app.Git.RevParse("main")
			if err != nil {
				t.Fatal(err)
			}
			if mainAfter != mainBefore {
				t.Error("main moved despite new notes")
			}
			got, err := app.DB.GetTask(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != StateMergeFailed {
				t.Errorf("state = %s, want MERGE_FAILED", got.State)
			}
		})
	}
}
