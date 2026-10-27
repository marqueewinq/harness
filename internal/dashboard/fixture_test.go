package dashboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marqueewinq/harness/internal"
)

const humanConfig = `main_branch: main
worktree_dir: .worktrees
checks:
  lint:
    run: "true"
  test:
    run: "true"
review:
  type: human
`

const agentConfig = `main_branch: main
worktree_dir: .worktrees
checks:
  lint:
    run: "true"
  test:
    run: "true"
review:
  type: agent
  cmd: "true"
`

func TestMain(m *testing.M) {
	// The developer's global git config (gpg signing, hooks) must not leak
	// into test repos; signing in particular fails intermittently.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test",
	)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo returns an App on a fresh git repo whose committed .harness.yaml is config.
func newRepo(t *testing.T, config string) *internal.App {
	t.Helper()
	dir := t.TempDir()
	gitOut(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".harness/\n.worktrees/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", ".")
	gitOut(t, dir, "commit", "-q", "-m", "init")
	app, err := internal.LoadApp(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return app
}

var statePath = map[internal.TaskState][]internal.TaskState{
	internal.StateInProgress:      nil,
	internal.StateChecksRunning:   {internal.StateChecksRunning},
	internal.StateChecksPassed:    {internal.StateChecksRunning, internal.StateChecksPassed},
	internal.StateChecksFailed:    {internal.StateChecksRunning, internal.StateChecksFailed},
	internal.StateReviewRequested: {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested},
	internal.StateRequestChanges:  {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested, internal.StateRequestChanges},
	internal.StateApproved:        {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested, internal.StateApproved},
	internal.StateQueued:          {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested, internal.StateApproved, internal.StateQueued},
	internal.StateMerging:         {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested, internal.StateApproved, internal.StateQueued, internal.StateMerging},
	internal.StateMerged:          {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested, internal.StateApproved, internal.StateQueued, internal.StateMerging, internal.StateMerged},
	internal.StateMergeFailed:     {internal.StateChecksRunning, internal.StateChecksPassed, internal.StateReviewRequested, internal.StateApproved, internal.StateQueued, internal.StateMergeFailed},
	internal.StateClosed:          {internal.StateClosed},
}

// seedTask creates a DB-only task (its worktree path does not exist) and
// walks it to state through valid transitions.
func seedTask(t *testing.T, app *internal.App, title, epicID string, state internal.TaskState) *internal.Task {
	t.Helper()
	if state == internal.StatePlanned {
		task, err := app.DB.CreatePlannedTask(title, "", epicID)
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	return seedTaskAt(t, app, title, epicID, state, filepath.Join(t.TempDir(), "missing", internal.Slugify(title)))
}

func seedTaskAt(t *testing.T, app *internal.App, title, epicID string, state internal.TaskState, worktree string) *internal.Task {
	t.Helper()
	task, err := app.DB.CreateTask(title, "", internal.Slugify(title), worktree, "main", "", epicID)
	if err != nil {
		t.Fatal(err)
	}
	path, ok := statePath[state]
	if !ok {
		t.Fatalf("no path to %s", state)
	}
	for _, s := range path {
		if err := app.DB.UpdateTaskState(task.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	task.State = state
	return task
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func findRow(t *testing.T, groups []EpicGroup, id string) TaskRow {
	t.Helper()
	for _, g := range groups {
		for _, r := range g.Tasks {
			if r.ID == id {
				return r
			}
		}
	}
	t.Fatalf("task %s not in groups", id)
	return TaskRow{}
}

func rowIDs(groups []EpicGroup) []string {
	var ids []string
	for _, g := range groups {
		for _, r := range g.Tasks {
			ids = append(ids, r.ID)
		}
	}
	return ids
}
