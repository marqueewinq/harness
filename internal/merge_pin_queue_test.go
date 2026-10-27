package internal

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A commit made while the merge waits in the queue must not ride along with
// the approval (or check run) the merge was admitted on.
func TestMergeRefusesBranchMovedWhileQueued(t *testing.T) {
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

			gitCommitFile(t, task.WorktreePath, "sneaky.txt", "unchecked\n", "commit while queued")
			if err := app.DB.DequeueTask("task-999"); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-done:
				if err == nil || !strings.Contains(err.Error(), "branch moved while waiting in the merge queue") {
					t.Fatalf("err = %v, want branch moved while waiting", err)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("merge did not finish")
			}
			mainAfter, err := app.Git.RevParse("main")
			if err != nil {
				t.Fatal(err)
			}
			if mainAfter != mainBefore {
				t.Error("main moved: a commit nobody checked or reviewed was merged")
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
