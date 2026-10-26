# Harness Phase 0 Implementation Plan

> **Note:** This is the original v1 implementation plan. It is kept for
> history. For current behaviour and command names, `README.md` is
> authoritative.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working `harness` CLI binary that enforces a task-based development workflow with checks, reviews, and gated merges.

**Architecture:** Single Go binary using cobra for CLI, sqlite for state, YAML for config. All git operations are subprocess calls. Commands are methods on an `App` struct that holds injected dependencies (config, DB, git client). Tests use temp git repos for integration testing.

**Tech Stack:** Go 1.22+, cobra (CLI), gopkg.in/yaml.v3, modernc.org/sqlite (pure Go, no CGO), database/sql

---

## File Structure

```
harness/
├── main.go                          # cobra root + subcommand registration
├── internal/
│   ├── config.go                    # Config struct + Load() from YAML
│   ├── config_test.go
│   ├── db.go                        # sqlite Open(), schema, Close()
│   ├── task_repo.go                 # task CRUD operations
│   ├── check_repo.go               # check_run CRUD operations
│   ├── review_repo.go              # review CRUD operations
│   ├── queue_repo.go               # merge_queue CRUD operations
│   ├── db_test.go                   # tests for all repo operations
│   ├── state.go                     # state constants + ValidTransition()
│   ├── state_test.go
│   ├── git.go                       # GitClient: branch, worktree, diff, merge, rebase
│   ├── git_test.go
│   ├── dag.go                       # DAG topo sort + validation
│   ├── dag_test.go
│   ├── runner.go                    # CheckRunner: sequential DAG execution
│   ├── runner_test.go
│   ├── app.go                       # App struct + dependency wiring
│   ├── cmd_init.go                  # harness init
│   ├── cmd_new.go                   # harness new
│   ├── cmd_list.go                  # harness list
│   ├── cmd_status.go                # harness status
│   ├── cmd_check.go                 # harness check
│   ├── cmd_diff.go                  # harness diff
│   ├── cmd_request_review.go        # harness request-review
│   ├── cmd_record_review.go         # harness record-review
│   ├── cmd_review_history.go        # harness review-history
│   └── cmd_merge.go                 # harness merge
├── go.mod
├── go.sum
├── Makefile
├── .harness.yaml                    # self-hosting config (added in Task 14)
└── docs/
    ├── spec.md
    └── plan.md
```

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`, `main.go`, `Makefile`, `internal/app.go`

- [ ] **Step 1: Initialize Go module and install dependencies**

```bash
cd ~/code/harness
go mod init github.com/marqueewinq/harness
go get github.com/spf13/cobra@latest
go get gopkg.in/yaml.v3
go get modernc.org/sqlite
```

- [ ] **Step 2: Create Makefile**

```makefile
.PHONY: build test install

build:
	go build -o harness ./main.go

test:
	go test ./...

install:
	go install ./...
```

- [ ] **Step 3: Create main.go with cobra root command**

```go
// main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "harness",
		Short: "Local workflow enforcement for AI coding agents",
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Create internal/app.go with App struct skeleton**

```go
// internal/app.go
package internal

// App holds all dependencies for command execution.
type App struct {
	Config *Config
	DB     *DB
	Git    *GitClient
}
```

- [ ] **Step 5: Verify build**

Run: `make build`
Expected: produces `./harness` binary, exits 0.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "project scaffolding: go module, cobra, makefile"
```

---

### Task 2: Configuration

**Files:**
- Create: `internal/config.go`, `internal/config_test.go`

- [ ] **Step 1: Write config parsing tests**

```go
// internal/config_test.go
package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	yaml := `
main_branch: main
worktree_dir: .worktrees
checks:
  test:
    run: make test
  lint:
    run: make lint
    needs: [test]
merge:
  require:
    checks: [lint]
    review: approved
  strategy: squash
  rerun_checks: true
review:
  type: agent
  prompt: "Review this."
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MainBranch != "main" {
		t.Errorf("MainBranch = %q, want %q", cfg.MainBranch, "main")
	}
	if cfg.WorktreeDir != ".worktrees" {
		t.Errorf("WorktreeDir = %q, want %q", cfg.WorktreeDir, ".worktrees")
	}
	if len(cfg.Checks) != 2 {
		t.Errorf("len(Checks) = %d, want 2", len(cfg.Checks))
	}
	if cfg.Checks["lint"].Run != "make lint" {
		t.Errorf("Checks[lint].Run = %q", cfg.Checks["lint"].Run)
	}
	if len(cfg.Checks["lint"].Needs) != 1 || cfg.Checks["lint"].Needs[0] != "test" {
		t.Errorf("Checks[lint].Needs = %v", cfg.Checks["lint"].Needs)
	}
	if cfg.Merge.Strategy != "squash" {
		t.Errorf("Merge.Strategy = %q, want %q", cfg.Merge.Strategy, "squash")
	}
	if !cfg.Merge.RerunChecks {
		t.Error("Merge.RerunChecks should be true")
	}
	if cfg.Review.Type != "agent" {
		t.Errorf("Review.Type = %q, want %q", cfg.Review.Type, "agent")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	yaml := `
checks:
  test:
    run: make test
`
	os.WriteFile(filepath.Join(dir, ".harness.yaml"), []byte(yaml), 0644)

	cfg, err := LoadConfig(filepath.Join(dir, ".harness.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MainBranch != "main" {
		t.Errorf("MainBranch default = %q, want %q", cfg.MainBranch, "main")
	}
	if cfg.WorktreeDir != ".worktrees" {
		t.Errorf("WorktreeDir default = %q, want %q", cfg.WorktreeDir, ".worktrees")
	}
	if cfg.Merge.Strategy != "merge" {
		t.Errorf("Merge.Strategy default = %q, want %q", cfg.Merge.Strategy, "merge")
	}
}

func TestLoadConfigMissing(t *testing.T) {
	_, err := LoadConfig("/nonexistent/.harness.yaml")
	if err == nil {
		t.Error("expected error for missing config")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ -run TestLoadConfig -v`
Expected: compilation error — `LoadConfig` not defined.

- [ ] **Step 3: Implement config.go**

```go
// internal/config.go
package internal

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type CheckConfig struct {
	Run   string   `yaml:"run"`
	Needs []string `yaml:"needs,omitempty"`
}

type MergeRequire struct {
	Checks []string `yaml:"checks"`
	Review string   `yaml:"review"`
}

type MergeConfig struct {
	Require     MergeRequire `yaml:"require"`
	Strategy    string       `yaml:"strategy"`
	RerunChecks bool         `yaml:"rerun_checks"`
}

type ReviewConfig struct {
	Type   string `yaml:"type"`
	Prompt string `yaml:"prompt"`
}

type Config struct {
	MainBranch string                 `yaml:"main_branch"`
	WorktreeDir string               `yaml:"worktree_dir"`
	Checks     map[string]CheckConfig `yaml:"checks"`
	Merge      MergeConfig            `yaml:"merge"`
	Review     ReviewConfig           `yaml:"review"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Defaults
	if cfg.MainBranch == "" {
		cfg.MainBranch = "main"
	}
	if cfg.WorktreeDir == "" {
		cfg.WorktreeDir = ".worktrees"
	}
	if cfg.Merge.Strategy == "" {
		cfg.Merge.Strategy = "merge"
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ -run TestLoadConfig -v`
Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config.go internal/config_test.go
git commit -m "config: YAML parsing with defaults"
```

---

### Task 3: State Machine

**Files:**
- Create: `internal/state.go`, `internal/state_test.go`

- [ ] **Step 1: Write state machine tests**

```go
// internal/state_test.go
package internal

import "testing"

func TestValidTransitions(t *testing.T) {
	valid := []struct {
		from, to TaskState
	}{
		{StateNew, StateInProgress},
		{StateInProgress, StateChecksRunning},
		{StateChecksRunning, StateChecksPassed},
		{StateChecksRunning, StateChecksFailed},
		{StateChecksFailed, StateChecksRunning},
		{StateChecksPassed, StateReviewRequested},
		{StateReviewRequested, StateApproved},
		{StateReviewRequested, StateRequestChanges},
		{StateRequestChanges, StateChecksRunning},
		{StateApproved, StateQueued},
		{StateQueued, StateMerging},
		{StateMerging, StateMerged},
		{StateMerging, StateMergeFailed},
		{StateMergeFailed, StateChecksRunning},
	}

	for _, tt := range valid {
		if !ValidTransition(tt.from, tt.to) {
			t.Errorf("expected %s -> %s to be valid", tt.from, tt.to)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	invalid := []struct {
		from, to TaskState
	}{
		{StateNew, StateChecksRunning},
		{StateInProgress, StateMerged},
		{StateChecksPassed, StateMerged},
		{StateMerged, StateInProgress},
		{StateApproved, StateInProgress},
		{StateQueued, StateChecksPassed},
	}

	for _, tt := range invalid {
		if ValidTransition(tt.from, tt.to) {
			t.Errorf("expected %s -> %s to be invalid", tt.from, tt.to)
		}
	}
}

func TestCheckableStates(t *testing.T) {
	checkable := []TaskState{
		StateInProgress, StateChecksFailed, StateRequestChanges, StateMergeFailed,
	}
	for _, s := range checkable {
		if !ValidTransition(s, StateChecksRunning) {
			t.Errorf("expected %s -> CHECKS_RUNNING to be valid", s)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ -run TestValidTransition -v`
Expected: compilation error.

- [ ] **Step 3: Implement state.go**

```go
// internal/state.go
package internal

type TaskState string

const (
	StateNew             TaskState = "NEW"
	StateInProgress      TaskState = "IN_PROGRESS"
	StateChecksRunning   TaskState = "CHECKS_RUNNING"
	StateChecksPassed    TaskState = "CHECKS_PASSED"
	StateChecksFailed    TaskState = "CHECKS_FAILED"
	StateReviewRequested TaskState = "REVIEW_REQUESTED"
	StateApproved        TaskState = "APPROVED"
	StateRequestChanges  TaskState = "REQUEST_CHANGES"
	StateQueued          TaskState = "QUEUED"
	StateMerging         TaskState = "MERGING"
	StateMerged          TaskState = "MERGED"
	StateMergeFailed     TaskState = "MERGE_FAILED"
)

var validTransitions = map[TaskState][]TaskState{
	StateNew:             {StateInProgress},
	StateInProgress:      {StateChecksRunning},
	StateChecksRunning:   {StateChecksPassed, StateChecksFailed},
	StateChecksPassed:    {StateReviewRequested},
	StateChecksFailed:    {StateChecksRunning},
	StateReviewRequested: {StateApproved, StateRequestChanges},
	StateApproved:        {StateQueued},
	StateRequestChanges:  {StateChecksRunning},
	StateQueued:          {StateMerging},
	StateMerging:         {StateMerged, StateMergeFailed},
	StateMergeFailed:     {StateChecksRunning},
}

func ValidTransition(from, to TaskState) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ -run "TestValidTransition|TestInvalidTransition|TestCheckable" -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state.go internal/state_test.go
git commit -m "state machine: states and transition validation"
```

---

### Task 4: Database Layer

**Files:**
- Create: `internal/db.go`, `internal/task_repo.go`, `internal/check_repo.go`, `internal/review_repo.go`, `internal/queue_repo.go`, `internal/db_test.go`

- [ ] **Step 1: Write DB + task repo tests**

```go
// internal/db_test.go
package internal

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenDB(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
}

func TestTaskCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create
	task, err := db.CreateTask("fix bug", "fix-bug", ".worktrees/fix-bug")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "task-001" {
		t.Errorf("ID = %q, want %q", task.ID, "task-001")
	}
	if task.State != StateInProgress {
		t.Errorf("State = %q, want %q", task.State, StateInProgress)
	}

	// Second task gets incremented ID
	task2, err := db.CreateTask("add feature", "add-feature", ".worktrees/add-feature")
	if err != nil {
		t.Fatal(err)
	}
	if task2.ID != "task-002" {
		t.Errorf("ID = %q, want %q", task2.ID, "task-002")
	}

	// Get by ID
	got, err := db.GetTask("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "fix bug" {
		t.Errorf("Description = %q", got.Description)
	}

	// List
	tasks, err := db.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("len(tasks) = %d, want 2", len(tasks))
	}

	// Update state
	err = db.UpdateTaskState("task-001", StateChecksRunning)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetTask("task-001")
	if got.State != StateChecksRunning {
		t.Errorf("State = %q, want %q", got.State, StateChecksRunning)
	}
}

func TestGetTaskByBranch(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("fix bug", "fix-bug", ".worktrees/fix-bug")
	got, err := db.GetTaskByBranch("fix-bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "task-001" {
		t.Errorf("ID = %q", got.ID)
	}
}

func TestCheckRunCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test")

	err = db.RecordCheckRun("task-001", "test", 0, "ok\n", "", 1, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = db.RecordCheckRun("task-001", "lint", 1, "", "fail\n", 1, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	runs, err := db.GetLatestCheckRuns("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Errorf("len(runs) = %d, want 2", len(runs))
	}
}

func TestReviewCRUD(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("test", "test-branch", ".worktrees/test")

	err = db.RecordReview("task-001", "request_changes", "fix the edge case")
	if err != nil {
		t.Fatal(err)
	}
	err = db.RecordReview("task-001", "approve", "")
	if err != nil {
		t.Fatal(err)
	}

	reviews, err := db.GetReviews("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 {
		t.Errorf("len(reviews) = %d, want 2", len(reviews))
	}
	if reviews[0].Decision != "request_changes" {
		t.Errorf("reviews[0].Decision = %q", reviews[0].Decision)
	}
}

func TestMergeQueue(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.CreateTask("task A", "branch-a", ".worktrees/a")
	db.CreateTask("task B", "branch-b", ".worktrees/b")

	pos1, err := db.EnqueueTask("task-001")
	if err != nil {
		t.Fatal(err)
	}
	pos2, err := db.EnqueueTask("task-002")
	if err != nil {
		t.Fatal(err)
	}
	if pos1 >= pos2 {
		t.Errorf("pos1=%d should be < pos2=%d", pos1, pos2)
	}

	first, err := db.PeekQueue()
	if err != nil {
		t.Fatal(err)
	}
	if first != "task-001" {
		t.Errorf("first = %q, want task-001", first)
	}

	err = db.DequeueTask("task-001")
	if err != nil {
		t.Fatal(err)
	}

	first, err = db.PeekQueue()
	if err != nil {
		t.Fatal(err)
	}
	if first != "task-002" {
		t.Errorf("first after dequeue = %q, want task-002", first)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ -run "TestOpenDB|TestTaskCRUD|TestCheckRun|TestReview|TestMergeQueue" -v`
Expected: compilation error.

- [ ] **Step 3: Implement db.go — connection + schema**

```go
// internal/db.go
package internal

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func OpenDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := createSchema(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func createSchema(conn *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		description TEXT NOT NULL,
		branch TEXT NOT NULL UNIQUE,
		worktree_path TEXT NOT NULL,
		state TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS check_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		check_name TEXT NOT NULL,
		exit_code INTEGER NOT NULL,
		stdout TEXT NOT NULL DEFAULT '',
		stderr TEXT NOT NULL DEFAULT '',
		started_at TIMESTAMP NOT NULL,
		finished_at TIMESTAMP NOT NULL,
		run_group INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS reviews (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL REFERENCES tasks(id),
		decision TEXT NOT NULL,
		comment TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS merge_queue (
		position INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
		queued_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := conn.Exec(schema)
	return err
}
```

- [ ] **Step 4: Implement task_repo.go**

```go
// internal/task_repo.go
package internal

import (
	"fmt"
	"time"
)

type Task struct {
	ID           string
	Description  string
	Branch       string
	WorktreePath string
	State        TaskState
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (db *DB) CreateTask(description, branch, worktreePath string) (*Task, error) {
	id, err := db.nextTaskID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, description, branch, worktree_path, state, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, description, branch, worktreePath, string(StateInProgress), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	return &Task{
		ID: id, Description: description, Branch: branch,
		WorktreePath: worktreePath, State: StateInProgress,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (db *DB) nextTaskID() (string, error) {
	var count int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&count)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("task-%03d", count+1), nil
}

func (db *DB) GetTask(id string) (*Task, error) {
	t := &Task{}
	err := db.conn.QueryRow(
		`SELECT id, description, branch, worktree_path, state, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.Description, &t.Branch, &t.WorktreePath, &t.State, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("task %q not found: %w", id, err)
	}
	return t, nil
}

func (db *DB) GetTaskByBranch(branch string) (*Task, error) {
	t := &Task{}
	err := db.conn.QueryRow(
		`SELECT id, description, branch, worktree_path, state, created_at, updated_at
		 FROM tasks WHERE branch = ?`, branch,
	).Scan(&t.ID, &t.Description, &t.Branch, &t.WorktreePath, &t.State, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("task with branch %q not found: %w", branch, err)
	}
	return t, nil
}

func (db *DB) ListTasks() ([]Task, error) {
	rows, err := db.conn.Query(
		`SELECT id, description, branch, worktree_path, state, created_at, updated_at
		 FROM tasks ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Description, &t.Branch, &t.WorktreePath, &t.State, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (db *DB) UpdateTaskState(id string, state TaskState) error {
	_, err := db.conn.Exec(
		`UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), time.Now(), id,
	)
	return err
}
```

- [ ] **Step 5: Implement check_repo.go**

```go
// internal/check_repo.go
package internal

import (
	"time"
)

type CheckRun struct {
	ID         int
	TaskID     string
	CheckName  string
	ExitCode   int
	Stdout     string
	Stderr     string
	StartedAt  time.Time
	FinishedAt time.Time
	RunGroup   int
}

func (db *DB) RecordCheckRun(taskID, checkName string, exitCode int, stdout, stderr string, runGroup int, startedAt, finishedAt time.Time) error {
	_, err := db.conn.Exec(
		`INSERT INTO check_runs (task_id, check_name, exit_code, stdout, stderr, run_group, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		taskID, checkName, exitCode, stdout, stderr, runGroup, startedAt, finishedAt,
	)
	return err
}

func (db *DB) GetLatestCheckRuns(taskID string) ([]CheckRun, error) {
	// Get the latest run_group for this task
	var maxGroup int
	err := db.conn.QueryRow(
		`SELECT COALESCE(MAX(run_group), 0) FROM check_runs WHERE task_id = ?`, taskID,
	).Scan(&maxGroup)
	if err != nil {
		return nil, err
	}
	if maxGroup == 0 {
		return nil, nil
	}

	rows, err := db.conn.Query(
		`SELECT id, task_id, check_name, exit_code, stdout, stderr, started_at, finished_at, run_group
		 FROM check_runs WHERE task_id = ? AND run_group = ? ORDER BY started_at ASC`,
		taskID, maxGroup,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []CheckRun
	for rows.Next() {
		var r CheckRun
		if err := rows.Scan(&r.ID, &r.TaskID, &r.CheckName, &r.ExitCode, &r.Stdout, &r.Stderr, &r.StartedAt, &r.FinishedAt, &r.RunGroup); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, nil
}

func (db *DB) NextRunGroup(taskID string) (int, error) {
	var maxGroup int
	err := db.conn.QueryRow(
		`SELECT COALESCE(MAX(run_group), 0) FROM check_runs WHERE task_id = ?`, taskID,
	).Scan(&maxGroup)
	if err != nil {
		return 0, err
	}
	return maxGroup + 1, nil
}
```

- [ ] **Step 6: Implement review_repo.go**

```go
// internal/review_repo.go
package internal

import "time"

type Review struct {
	ID        int
	TaskID    string
	Decision  string
	Comment   string
	CreatedAt time.Time
}

func (db *DB) RecordReview(taskID, decision, comment string) error {
	_, err := db.conn.Exec(
		`INSERT INTO reviews (task_id, decision, comment) VALUES (?, ?, ?)`,
		taskID, decision, comment,
	)
	return err
}

func (db *DB) GetReviews(taskID string) ([]Review, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, decision, comment, created_at
		 FROM reviews WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Decision, &r.Comment, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}
```

- [ ] **Step 7: Implement queue_repo.go**

```go
// internal/queue_repo.go
package internal

func (db *DB) EnqueueTask(taskID string) (int, error) {
	result, err := db.conn.Exec(
		`INSERT INTO merge_queue (task_id) VALUES (?)`, taskID,
	)
	if err != nil {
		return 0, err
	}
	pos, err := result.LastInsertId()
	return int(pos), err
}

func (db *DB) DequeueTask(taskID string) error {
	_, err := db.conn.Exec(`DELETE FROM merge_queue WHERE task_id = ?`, taskID)
	return err
}

func (db *DB) PeekQueue() (string, error) {
	var taskID string
	err := db.conn.QueryRow(
		`SELECT task_id FROM merge_queue ORDER BY position ASC LIMIT 1`,
	).Scan(&taskID)
	if err != nil {
		return "", err
	}
	return taskID, nil
}

func (db *DB) GetQueuePosition(taskID string) (int, error) {
	// Returns 1-based position in queue (how many tasks are ahead + 1)
	var pos int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM merge_queue
		WHERE position <= (SELECT position FROM merge_queue WHERE task_id = ?)`,
		taskID,
	).Scan(&pos)
	return pos, err
}
```

- [ ] **Step 8: Run all tests**

Run: `go test ./internal/ -run "TestOpenDB|TestTaskCRUD|TestGetTaskByBranch|TestCheckRun|TestReview|TestMergeQueue" -v`
Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/db.go internal/task_repo.go internal/check_repo.go internal/review_repo.go internal/queue_repo.go internal/db_test.go
git commit -m "database: sqlite schema and CRUD for tasks, checks, reviews, queue"
```

---

### Task 5: Git Operations

**Files:**
- Create: `internal/git.go`, `internal/git_test.go`

- [ ] **Step 1: Write git operations tests**

```go
// internal/git_test.go
package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a bare git repo with one commit on main.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "checkout", "-b", "main"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v failed: %s\n%s", args, err, out)
		}
	}
	return dir
}

func TestCreateBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	err := git.CreateBranch("feature-x", "main")
	if err != nil {
		t.Fatal(err)
	}

	out, err := git.run("branch", "--list", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "feature-x") {
		t.Errorf("branch not created: %q", out)
	}
}

func TestCreateWorktree(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	err := git.CreateBranch("feature-x", "main")
	if err != nil {
		t.Fatal(err)
	}
	err = git.CreateWorktree("feature-x", wtPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("worktree directory not created")
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	err := git.RemoveWorktree(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed")
	}
}

func TestDiff(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	// Make a change in the worktree
	os.WriteFile(filepath.Join(wtPath, "hello.txt"), []byte("hello\n"), 0644)
	wtGit := NewGitClient(wtPath)
	wtGit.run("add", "hello.txt")
	wtGit.run("commit", "-m", "add hello")

	diff, err := git.Diff("main", "feature-x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "hello.txt") {
		t.Errorf("diff should mention hello.txt: %q", diff)
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	branch, err := git.CurrentBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want %q", branch, "main")
	}
}

func TestMergeFastForward(t *testing.T) {
	dir := initTestRepo(t)
	git := NewGitClient(dir)

	wtPath := filepath.Join(dir, ".worktrees", "feature-x")
	git.CreateBranch("feature-x", "main")
	git.CreateWorktree("feature-x", wtPath)

	os.WriteFile(filepath.Join(wtPath, "hello.txt"), []byte("hello\n"), 0644)
	wtGit := NewGitClient(wtPath)
	wtGit.run("add", "hello.txt")
	wtGit.run("commit", "-m", "add hello")

	err := git.MergeFF("feature-x")
	if err != nil {
		t.Fatal(err)
	}

	// Verify file exists on main
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); os.IsNotExist(err) {
		t.Error("merged file should exist on main")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ -run "TestCreateBranch|TestCreateWorktree|TestRemoveWorktree|TestDiff|TestCurrentBranch|TestMergeFF" -v`
Expected: compilation error.

- [ ] **Step 3: Implement git.go**

```go
// internal/git.go
package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitClient struct {
	dir string
}

func NewGitClient(dir string) *GitClient {
	return &GitClient{dir: dir}
}

func (g *GitClient) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=harness", "GIT_AUTHOR_EMAIL=harness@local", "GIT_COMMITTER_NAME=harness", "GIT_COMMITTER_EMAIL=harness@local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func (g *GitClient) CreateBranch(name, from string) error {
	_, err := g.run("branch", name, from)
	return err
}

func (g *GitClient) DeleteBranch(name string) error {
	_, err := g.run("branch", "-D", name)
	return err
}

func (g *GitClient) CreateWorktree(branch, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	_, err := g.run("worktree", "add", path, branch)
	return err
}

func (g *GitClient) RemoveWorktree(path string) error {
	_, err := g.run("worktree", "remove", path, "--force")
	return err
}

func (g *GitClient) Diff(base, head string) (string, error) {
	return g.run("diff", base+"..."+head)
}

func (g *GitClient) CurrentBranch() (string, error) {
	return g.run("rev-parse", "--abbrev-ref", "HEAD")
}

func (g *GitClient) Rebase(onto string) error {
	_, err := g.run("rebase", onto)
	return err
}

func (g *GitClient) RebaseAbort() error {
	_, err := g.run("rebase", "--abort")
	return err
}

func (g *GitClient) MergeFF(branch string) error {
	_, err := g.run("merge", "--ff-only", branch)
	return err
}

func (g *GitClient) Checkout(branch string) error {
	_, err := g.run("checkout", branch)
	return err
}

func (g *GitClient) RevParse(ref string) (string, error) {
	return g.run("rev-parse", ref)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ -run "TestCreateBranch|TestCreateWorktree|TestRemoveWorktree|TestDiff|TestCurrentBranch|TestMergeFF" -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git.go internal/git_test.go
git commit -m "git: wrapper for branch, worktree, diff, merge operations"
```

---

### Task 6: Check DAG

**Files:**
- Create: `internal/dag.go`, `internal/dag_test.go`

- [ ] **Step 1: Write DAG tests**

```go
// internal/dag_test.go
package internal

import (
	"reflect"
	"testing"
)

func TestTopoSort(t *testing.T) {
	checks := map[string]CheckConfig{
		"install": {Run: "npm install"},
		"lint":    {Run: "npm run lint", Needs: []string{"install"}},
		"test":    {Run: "make test", Needs: []string{"install"}},
		"build":   {Run: "make build", Needs: []string{"test", "lint"}},
	}

	order, err := TopoSort(checks)
	if err != nil {
		t.Fatal(err)
	}

	// install must come before lint, test; lint and test before build
	pos := map[string]int{}
	for i, name := range order {
		pos[name] = i
	}

	if pos["install"] >= pos["lint"] {
		t.Errorf("install(%d) should come before lint(%d)", pos["install"], pos["lint"])
	}
	if pos["install"] >= pos["test"] {
		t.Errorf("install(%d) should come before test(%d)", pos["install"], pos["test"])
	}
	if pos["lint"] >= pos["build"] {
		t.Errorf("lint(%d) should come before build(%d)", pos["lint"], pos["build"])
	}
	if pos["test"] >= pos["build"] {
		t.Errorf("test(%d) should come before build(%d)", pos["test"], pos["build"])
	}
}

func TestTopoSortCycle(t *testing.T) {
	checks := map[string]CheckConfig{
		"a": {Run: "a", Needs: []string{"b"}},
		"b": {Run: "b", Needs: []string{"a"}},
	}
	_, err := TopoSort(checks)
	if err == nil {
		t.Error("expected cycle error")
	}
}

func TestTopoSortUnknownDep(t *testing.T) {
	checks := map[string]CheckConfig{
		"test": {Run: "test", Needs: []string{"nonexistent"}},
	}
	_, err := TopoSort(checks)
	if err == nil {
		t.Error("expected unknown dependency error")
	}
}

func TestTopoSortSingle(t *testing.T) {
	checks := map[string]CheckConfig{
		"test": {Run: "make test"},
	}
	order, err := TopoSort(checks)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []string{"test"}) {
		t.Errorf("order = %v, want [test]", order)
	}
}

func TestReadyChecks(t *testing.T) {
	checks := map[string]CheckConfig{
		"install": {Run: "npm install"},
		"lint":    {Run: "lint", Needs: []string{"install"}},
		"test":    {Run: "test", Needs: []string{"install"}},
		"build":   {Run: "build", Needs: []string{"test", "lint"}},
	}

	// Nothing completed: only install is ready
	ready := ReadyChecks(checks, map[string]bool{})
	if !reflect.DeepEqual(ready, []string{"install"}) {
		t.Errorf("ready = %v, want [install]", ready)
	}

	// install done: lint and test are ready
	ready = ReadyChecks(checks, map[string]bool{"install": true})
	if len(ready) != 2 {
		t.Errorf("ready = %v, want [lint, test]", ready)
	}

	// install+lint+test done: build is ready
	ready = ReadyChecks(checks, map[string]bool{"install": true, "lint": true, "test": true})
	if !reflect.DeepEqual(ready, []string{"build"}) {
		t.Errorf("ready = %v, want [build]", ready)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ -run "TestTopoSort|TestReadyChecks" -v`
Expected: compilation error.

- [ ] **Step 3: Implement dag.go**

```go
// internal/dag.go
package internal

import (
	"fmt"
	"sort"
)

// TopoSort returns check names in topological order.
// Returns error if cycle detected or unknown dependency.
func TopoSort(checks map[string]CheckConfig) ([]string, error) {
	// Validate dependencies exist
	for name, check := range checks {
		for _, dep := range check.Needs {
			if _, ok := checks[dep]; !ok {
				return nil, fmt.Errorf("check %q depends on unknown check %q", name, dep)
			}
		}
	}

	// Kahn's algorithm
	inDegree := map[string]int{}
	dependents := map[string][]string{} // dep -> list of checks that need it

	for name := range checks {
		inDegree[name] = 0
	}
	for name, check := range checks {
		inDegree[name] = len(check.Needs)
		for _, dep := range check.Needs {
			dependents[dep] = append(dependents[dep], name)
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // deterministic order

	var result []string
	for len(queue) > 0 {
		// Sort for deterministic output
		sort.Strings(queue)
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(result) != len(checks) {
		return nil, fmt.Errorf("cycle detected in check dependencies")
	}

	return result, nil
}

// ReadyChecks returns check names whose dependencies are all in the completed set.
// Excludes already-completed checks.
func ReadyChecks(checks map[string]CheckConfig, completed map[string]bool) []string {
	var ready []string
	for name, check := range checks {
		if completed[name] {
			continue
		}
		allMet := true
		for _, dep := range check.Needs {
			if !completed[dep] {
				allMet = false
				break
			}
		}
		if allMet {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	return ready
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ -run "TestTopoSort|TestReadyChecks" -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dag.go internal/dag_test.go
git commit -m "dag: topological sort and ready-check computation"
```

---

### Task 7: Check Runner

**Files:**
- Create: `internal/runner.go`, `internal/runner_test.go`

- [ ] **Step 1: Write runner tests**

```go
// internal/runner_test.go
package internal

import (
	"testing"
)

func TestRunnerAllPass(t *testing.T) {
	checks := map[string]CheckConfig{
		"step1": {Run: "echo hello"},
		"step2": {Run: "echo world", Needs: []string{"step1"}},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("check %q failed: exit %d, stderr: %s", r.Name, r.ExitCode, r.Stderr)
		}
	}
}

func TestRunnerFailure(t *testing.T) {
	checks := map[string]CheckConfig{
		"pass": {Run: "echo ok"},
		"fail": {Run: "exit 1", Needs: []string{"pass"}},
		"skip": {Run: "echo should-not-run", Needs: []string{"fail"}},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}

	resultMap := map[string]CheckResult{}
	for _, r := range results {
		resultMap[r.Name] = r
	}

	if resultMap["pass"].ExitCode != 0 {
		t.Error("pass should have exit 0")
	}
	if resultMap["fail"].ExitCode == 0 {
		t.Error("fail should have non-zero exit")
	}
	if !resultMap["skip"].Skipped {
		t.Error("skip should be skipped")
	}
}

func TestRunnerParallel(t *testing.T) {
	// Two independent checks should both run
	checks := map[string]CheckConfig{
		"a": {Run: "echo a"},
		"b": {Run: "echo b"},
	}
	runner := NewCheckRunner(checks, t.TempDir())
	results, err := runner.RunAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("check %q failed", r.Name)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ -run "TestRunner" -v`
Expected: compilation error.

- [ ] **Step 3: Implement runner.go**

The runner executes checks following DAG order. Independent checks at the same DAG level run in parallel via goroutines.

```go
// internal/runner.go
package internal

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

type CheckResult struct {
	Name       string
	ExitCode   int
	Stdout     string
	Stderr     string
	Skipped    bool
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
}

type CheckRunner struct {
	checks map[string]CheckConfig
	dir    string
}

func NewCheckRunner(checks map[string]CheckConfig, dir string) *CheckRunner {
	return &CheckRunner{checks: checks, dir: dir}
}

func (cr *CheckRunner) RunAll() ([]CheckResult, error) {
	order, err := TopoSort(cr.checks)
	if err != nil {
		return nil, fmt.Errorf("invalid check DAG: %w", err)
	}

	completed := map[string]bool{}
	failed := map[string]bool{}
	results := map[string]CheckResult{}
	var mu sync.Mutex

	for len(completed)+len(failed) < len(order) {
		ready := ReadyChecks(cr.checks, completed)
		if len(ready) == 0 {
			// Remaining checks have failed dependencies — mark as skipped
			for _, name := range order {
				if !completed[name] && !failed[name] {
					if _, exists := results[name]; !exists {
						results[name] = CheckResult{Name: name, Skipped: true}
						failed[name] = true
					}
				}
			}
			break
		}

		// Filter out checks with failed dependencies
		var runnable []string
		for _, name := range ready {
			hasFailed := false
			for _, dep := range cr.checks[name].Needs {
				if failed[dep] {
					hasFailed = true
					break
				}
			}
			if hasFailed {
				results[name] = CheckResult{Name: name, Skipped: true}
				failed[name] = true
			} else {
				runnable = append(runnable, name)
			}
		}

		if len(runnable) == 0 {
			continue
		}

		// Run ready checks in parallel
		var wg sync.WaitGroup
		for _, name := range runnable {
			wg.Add(1)
			go func(n string) {
				defer wg.Done()
				result := cr.runOne(n)
				mu.Lock()
				results[n] = result
				if result.ExitCode == 0 {
					completed[n] = true
				} else {
					failed[n] = true
				}
				mu.Unlock()
			}(name)
		}
		wg.Wait()
	}

	// Return results in topological order
	var ordered []CheckResult
	for _, name := range order {
		if r, ok := results[name]; ok {
			ordered = append(ordered, r)
		}
	}
	return ordered, nil
}

func (cr *CheckRunner) runOne(name string) CheckResult {
	check := cr.checks[name]
	start := time.Now()

	cmd := exec.Command("sh", "-c", check.Run)
	cmd.Dir = cr.dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	finish := time.Now()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return CheckResult{
		Name:       name,
		ExitCode:   exitCode,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		StartedAt:  start,
		FinishedAt: finish,
		Duration:   finish.Sub(start),
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ -run "TestRunner" -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner.go internal/runner_test.go
git commit -m "runner: DAG-aware parallel check execution"
```

---

### Task 8: CLI Commands — init, new, list, status

**Files:**
- Create: `internal/cmd_init.go`, `internal/cmd_new.go`, `internal/cmd_list.go`, `internal/cmd_status.go`
- Modify: `main.go` (register commands), `internal/app.go` (add helper methods)

- [ ] **Step 1: Update app.go with helper methods**

```go
// internal/app.go
package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type App struct {
	Config    *Config
	DB        *DB
	Git       *GitClient
	RepoRoot  string
}

// FindRepoRoot walks up from cwd to find .harness.yaml.
// Returns the directory containing .harness.yaml.
func FindRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".harness.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .harness.yaml found (walk up from cwd)")
		}
		dir = parent
	}
}

// LoadApp creates an App from the repo root. Call after FindRepoRoot.
func LoadApp(repoRoot string) (*App, error) {
	cfg, err := LoadConfig(filepath.Join(repoRoot, ".harness.yaml"))
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(repoRoot, ".harness", "harness.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}

	git := NewGitClient(repoRoot)

	return &App{Config: cfg, DB: db, Git: git, RepoRoot: repoRoot}, nil
}

func (a *App) Close() {
	if a.DB != nil {
		a.DB.Close()
	}
}

// Slugify converts a description to a branch-safe name.
func Slugify(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// ResolveTaskID returns the task ID — either from explicit arg or detected from current worktree.
func (a *App) ResolveTaskID(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	// Detect from current branch
	branch, err := a.Git.CurrentBranch()
	if err != nil {
		return "", fmt.Errorf("cannot detect task: %w", err)
	}

	task, err := a.DB.GetTaskByBranch(branch)
	if err != nil {
		return "", fmt.Errorf("no task found for branch %q — pass task ID explicitly", branch)
	}
	return task.ID, nil
}
```

- [ ] **Step 2: Implement cmd_init.go**

```go
// internal/cmd_init.go
package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func (a *App) CmdInit() error {
	// This is called without a loaded App — it creates the scaffold
	return nil // placeholder, Init is special
}

// RunInit creates .harness.yaml scaffold and .harness/ directory.
// Does not require an existing App. Operates in the given directory.
func RunInit(dir string) error {
	configPath := filepath.Join(dir, ".harness.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf(".harness.yaml already exists")
	}

	scaffold := `# Harness workflow configuration
# See: https://github.com/marqueewinq/harness

main_branch: main
worktree_dir: .worktrees

checks:
  test:
    run: make test

merge:
  require:
    checks: [test]
    review: approved
  strategy: merge
  rerun_checks: true

review:
  type: human
  prompt: |
    Review this change for correctness, test coverage, and project conventions.
    Previous review comments: {{previous_comments}}
    Task description: {{task_description}}
    After reviewing, call: harness record-review approve
    or: harness record-review request-changes --comment "your comments"
`
	if err := os.WriteFile(configPath, []byte(scaffold), 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	harnessDir := filepath.Join(dir, ".harness")
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return fmt.Errorf("creating .harness/: %w", err)
	}

	// Create empty DB
	db, err := OpenDB(filepath.Join(harnessDir, "harness.db"))
	if err != nil {
		return fmt.Errorf("creating database: %w", err)
	}
	db.Close()

	// Add .harness/ to .gitignore
	gitignorePath := filepath.Join(dir, ".gitignore")
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}
	defer f.Close()
	f.WriteString("\n.harness/\n.worktrees/\n")

	fmt.Println("Initialized harness.")
	fmt.Printf("  Config: %s\n", configPath)
	fmt.Printf("  Database: %s/harness.db\n", harnessDir)
	fmt.Println("\nEdit .harness.yaml to configure checks and merge requirements.")
	return nil
}
```

- [ ] **Step 3: Implement cmd_new.go**

```go
// internal/cmd_new.go
package internal

import (
	"fmt"
	"path/filepath"
)

func (a *App) CmdNew(description string) error {
	branch := Slugify(description)
	worktreePath := filepath.Join(a.RepoRoot, a.Config.WorktreeDir, branch)

	if err := a.Git.CreateBranch(branch, a.Config.MainBranch); err != nil {
		return fmt.Errorf("creating branch: %w", err)
	}

	if err := a.Git.CreateWorktree(branch, worktreePath); err != nil {
		// Clean up branch on failure
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating worktree: %w", err)
	}

	task, err := a.DB.CreateTask(description, branch, worktreePath)
	if err != nil {
		// Clean up on failure
		a.Git.RemoveWorktree(worktreePath)
		a.Git.DeleteBranch(branch)
		return fmt.Errorf("creating task: %w", err)
	}

	fmt.Printf("Created %s: %q\n", task.ID, description)
	fmt.Printf("Branch: %s\n", branch)
	fmt.Printf("Worktree: %s\n", worktreePath)
	fmt.Printf("\nNext: implement the changes in %s/\n", worktreePath)
	fmt.Println("      then run: harness check")
	return nil
}
```

- [ ] **Step 4: Implement cmd_list.go**

```go
// internal/cmd_list.go
package internal

import (
	"fmt"
)

func (a *App) CmdList() error {
	tasks, err := a.DB.ListTasks()
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks. Run: harness new \"description\"")
		return nil
	}

	fmt.Printf("%-10s %-20s %s\n", "ID", "State", "Description")
	for _, t := range tasks {
		// Check queue position if QUEUED
		queueInfo := ""
		if t.State == StateQueued {
			pos, err := a.DB.GetQueuePosition(t.ID)
			if err == nil {
				queueInfo = fmt.Sprintf(" (#%d)", pos)
			}
		}
		fmt.Printf("%-10s %-20s %s%s\n", t.ID, t.State, t.Description, queueInfo)
	}
	return nil
}
```

- [ ] **Step 5: Implement cmd_status.go**

```go
// internal/cmd_status.go
package internal

import (
	"fmt"
	"strings"
)

func (a *App) CmdStatus(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}

	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	fmt.Printf("Task: %s %q\n", task.ID, task.Description)
	fmt.Printf("Branch: %s\n", task.Branch)
	fmt.Printf("Worktree: %s\n", task.WorktreePath)
	fmt.Printf("State: %s\n", task.State)

	// Show last check run
	runs, err := a.DB.GetLatestCheckRuns(task.ID)
	if err == nil && len(runs) > 0 {
		fmt.Println("\nLast check run:")
		for _, r := range runs {
			if r.Skipped {
				fmt.Printf("  %-12s SKIP\n", r.CheckName)
				continue
			}
			status := "PASS"
			if r.ExitCode != 0 {
				status = "FAIL"
			}
			dur := r.FinishedAt.Sub(r.StartedAt)
			fmt.Printf("  %-12s %-6s (%.1fs)\n", r.CheckName, status, dur.Seconds())
			if r.ExitCode != 0 && r.Stderr != "" {
				for _, line := range strings.Split(strings.TrimSpace(r.Stderr), "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
		}
	}

	// Show latest review
	reviews, err := a.DB.GetReviews(task.ID)
	if err == nil && len(reviews) > 0 {
		latest := reviews[len(reviews)-1]
		fmt.Printf("\nLatest review: %s\n", strings.ToUpper(latest.Decision))
		if latest.Comment != "" {
			fmt.Printf("  %s\n", latest.Comment)
		}
	} else {
		fmt.Println("\nReview: not requested")
	}

	// Show merge requirements
	fmt.Println("\nMerge requirements:")
	checksOK := task.State == StateChecksPassed || task.State == StateReviewRequested || task.State == StateApproved || task.State == StateQueued
	reviewOK := task.State == StateApproved || task.State == StateQueued
	inWorktree := task.WorktreePath != ""

	mark := func(ok bool) string {
		if ok {
			return "[x]"
		}
		return "[ ]"
	}
	fmt.Printf("  %s Work in worktree\n", mark(inWorktree))
	fmt.Printf("  %s All checks pass\n", mark(checksOK))
	if a.Config.Merge.Require.Review == "approved" {
		fmt.Printf("  %s Review approved\n", mark(reviewOK))
	}

	// Next action hint
	fmt.Println()
	switch task.State {
	case StateInProgress:
		fmt.Println("Next: run harness check")
	case StateChecksFailed, StateMergeFailed:
		fmt.Println("Next: fix the failures, then run: harness check")
	case StateChecksPassed:
		fmt.Println("Next: run harness request-review")
	case StateRequestChanges:
		fmt.Println("Next: address review comments, then run: harness check")
	case StateReviewRequested:
		if a.Config.Review.Type == "human" {
			fmt.Println("Next: waiting for human review.")
			fmt.Printf("      In another terminal: harness record-review approve %s\n", task.ID)
		} else {
			fmt.Println("Next: spawn a reviewer subagent, then: harness record-review approve/request-changes")
		}
	case StateApproved:
		fmt.Println("Next: run harness merge")
	case StateQueued:
		pos, _ := a.DB.GetQueuePosition(task.ID)
		fmt.Printf("Queued at position #%d. harness merge is blocking.\n", pos)
	case StateMerged:
		fmt.Println("Task is complete.")
	}

	return nil
}
```

- [ ] **Step 6: Wire commands into main.go**

```go
// main.go
package main

import (
	"fmt"
	"os"

	"github.com/marqueewinq/harness/internal"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "harness",
		Short: "Local workflow enforcement for AI coding agents",
	}

	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(newCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(checkCmd())
	rootCmd.AddCommand(diffCmd())
	rootCmd.AddCommand(requestReviewCmd())
	rootCmd.AddCommand(recordReviewCmd())
	rootCmd.AddCommand(reviewHistoryCmd())
	rootCmd.AddCommand(mergeCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize harness in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := os.Getwd()
			return internal.RunInit(dir)
		},
	}
}

func withApp(fn func(*internal.App, *cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		root, err := internal.FindRepoRoot()
		if err != nil {
			return err
		}
		app, err := internal.LoadApp(root)
		if err != nil {
			return err
		}
		defer app.Close()
		return fn(app, cmd, args)
	}
}

func newCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <description>",
		Short: "Create a new task with branch and worktree",
		Args:  cobra.ExactArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			return app.CmdNew(args[0])
		}),
	}
}

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tasks",
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			return app.CmdList()
		}),
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [task-id]",
		Short: "Show task status",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdStatus(taskID)
		}),
	}
}

// Stubs for commands implemented in later tasks
func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check [task-id]",
		Short: "Run checks",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdCheck(taskID)
		}),
	}
}

func diffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [task-id]",
		Short: "Show diff against main",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdDiff(taskID)
		}),
	}
}

func requestReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "request-review [task-id]",
		Short: "Request review for task",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdRequestReview(taskID)
		}),
	}
}

func recordReviewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "record-review <approve|request-changes> [task-id]",
		Short: "Record a review decision",
		Args:  cobra.RangeArgs(1, 2),
	}

	var comment string
	cmd.Flags().StringVar(&comment, "comment", "", "Review comment (required for request-changes)")

	cmd.RunE = withApp(func(app *internal.App, c *cobra.Command, args []string) error {
		decision := args[0]
		taskID := ""
		if len(args) > 1 {
			taskID = args[1]
		}
		return app.CmdRecordReview(taskID, decision, comment)
	})
	return cmd
}

func reviewHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "review-history [task-id]",
		Short: "Show review history",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdReviewHistory(taskID)
		}),
	}
}

func mergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge [task-id]",
		Short: "Merge task branch into main",
		Args:  cobra.MaximumNArgs(1),
		RunE: withApp(func(app *internal.App, cmd *cobra.Command, args []string) error {
			taskID := ""
			if len(args) > 0 {
				taskID = args[0]
			}
			return app.CmdMerge(taskID)
		}),
	}
}
```

- [ ] **Step 7: Create stub command files so it compiles**

Create stubs for commands not yet implemented (check, diff, review, merge) so the binary compiles:

`internal/cmd_check.go`:
```go
package internal

import "fmt"

func (a *App) CmdCheck(taskID string) error {
	return fmt.Errorf("not implemented yet")
}
```

`internal/cmd_diff.go`:
```go
package internal

import "fmt"

func (a *App) CmdDiff(taskID string) error {
	return fmt.Errorf("not implemented yet")
}
```

`internal/cmd_request_review.go`:
```go
package internal

import "fmt"

func (a *App) CmdRequestReview(taskID string) error {
	return fmt.Errorf("not implemented yet")
}
```

`internal/cmd_record_review.go`:
```go
package internal

import "fmt"

func (a *App) CmdRecordReview(taskID, decision, comment string) error {
	return fmt.Errorf("not implemented yet")
}
```

`internal/cmd_review_history.go`:
```go
package internal

import "fmt"

func (a *App) CmdReviewHistory(taskID string) error {
	return fmt.Errorf("not implemented yet")
}
```

`internal/cmd_merge.go`:
```go
package internal

import "fmt"

func (a *App) CmdMerge(taskID string) error {
	return fmt.Errorf("not implemented yet")
}
```

- [ ] **Step 8: Verify build and test**

Run: `make build && make test`
Expected: binary builds, all tests pass.

- [ ] **Step 9: Commit**

```bash
git add main.go internal/app.go internal/cmd_*.go
git commit -m "cli: init, new, list, status commands + cobra wiring"
```

---

### Task 9: CLI Commands — check

**Files:**
- Modify: `internal/cmd_check.go`

- [ ] **Step 1: Implement cmd_check.go**

Replace the stub:

```go
// internal/cmd_check.go
package internal

import (
	"fmt"
)

func (a *App) CmdCheck(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}

	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	// Validate state transition
	checkableStates := map[TaskState]bool{
		StateInProgress:    true,
		StateChecksFailed:  true,
		StateRequestChanges: true,
		StateMergeFailed:   true,
	}
	if !checkableStates[task.State] {
		return fmt.Errorf("cannot run checks in state %s (allowed: IN_PROGRESS, CHECKS_FAILED, REQUEST_CHANGES, MERGE_FAILED)", task.State)
	}

	// Transition to CHECKS_RUNNING
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

	// Record results
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
```

- [ ] **Step 2: Verify build**

Run: `make build && make test`
Expected: builds, tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd_check.go
git commit -m "cli: check command with DAG execution"
```

---

### Task 10: CLI Commands — diff, request-review, record-review, review-history

**Files:**
- Modify: `internal/cmd_diff.go`, `internal/cmd_request_review.go`, `internal/cmd_record_review.go`, `internal/cmd_review_history.go`

- [ ] **Step 1: Implement cmd_diff.go**

```go
// internal/cmd_diff.go
package internal

import "fmt"

func (a *App) CmdDiff(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}

	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	diff, err := a.Git.Diff(a.Config.MainBranch, task.Branch)
	if err != nil {
		return fmt.Errorf("getting diff: %w", err)
	}

	if diff == "" {
		fmt.Println("No changes.")
	} else {
		fmt.Println(diff)
	}
	return nil
}
```

- [ ] **Step 2: Implement cmd_request_review.go**

```go
// internal/cmd_request_review.go
package internal

import (
	"fmt"
	"strings"
)

func (a *App) CmdRequestReview(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}

	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	if task.State != StateChecksPassed {
		return fmt.Errorf("cannot request review in state %s (need CHECKS_PASSED)", task.State)
	}

	if err := a.DB.UpdateTaskState(id, StateReviewRequested); err != nil {
		return err
	}

	// Output diff
	diff, err := a.Git.Diff(a.Config.MainBranch, task.Branch)
	if err != nil {
		diff = "(error getting diff)"
	}

	// Output previous comments
	reviews, _ := a.DB.GetReviews(id)
	prevComments := "(none)"
	if len(reviews) > 0 {
		var parts []string
		for _, r := range reviews {
			if r.Comment != "" {
				parts = append(parts, fmt.Sprintf("[%s] %s", r.Decision, r.Comment))
			}
		}
		if len(parts) > 0 {
			prevComments = strings.Join(parts, "\n  ")
		}
	}

	fmt.Printf("Task %s is now REVIEW_REQUESTED.\n\n", id)
	fmt.Printf("Diff (%s..%s):\n%s\n\n", a.Config.MainBranch, task.Branch, diff)
	fmt.Printf("Task description: %s\n\n", task.Description)
	fmt.Printf("Previous review comments:\n  %s\n\n", prevComments)

	if a.Config.Review.Prompt != "" {
		prompt := a.Config.Review.Prompt
		prompt = strings.ReplaceAll(prompt, "{{previous_comments}}", prevComments)
		prompt = strings.ReplaceAll(prompt, "{{task_description}}", task.Description)
		fmt.Printf("Review prompt:\n%s\n", prompt)
	}

	fmt.Println("---")
	if a.Config.Review.Type == "agent" {
		fmt.Println("For agent review: spawn a reviewer subagent with the above context.")
		fmt.Printf("  The reviewer must call: harness record-review approve %s\n", id)
		fmt.Printf("  or: harness record-review request-changes --comment \"...\" %s\n", id)
	} else {
		fmt.Println("For human review, in another terminal run:")
		fmt.Printf("  harness diff %s\n", id)
		fmt.Printf("  harness record-review approve %s\n", id)
	}

	return nil
}
```

- [ ] **Step 3: Implement cmd_record_review.go**

```go
// internal/cmd_record_review.go
package internal

import "fmt"

func (a *App) CmdRecordReview(taskID, decision, comment string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}

	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	if task.State != StateReviewRequested {
		return fmt.Errorf("cannot record review in state %s (need REVIEW_REQUESTED)", task.State)
	}

	switch decision {
	case "approve":
		if err := a.DB.RecordReview(id, "approve", comment); err != nil {
			return err
		}
		if err := a.DB.UpdateTaskState(id, StateApproved); err != nil {
			return err
		}
		fmt.Printf("Review recorded: APPROVE\n")
		fmt.Printf("Task %s is now APPROVED.\n", id)
		fmt.Println("Next: run harness merge")

	case "request-changes":
		if comment == "" {
			return fmt.Errorf("--comment is required for request-changes")
		}
		if err := a.DB.RecordReview(id, "request_changes", comment); err != nil {
			return err
		}
		if err := a.DB.UpdateTaskState(id, StateRequestChanges); err != nil {
			return err
		}
		fmt.Printf("Review recorded: REQUEST_CHANGES\n")
		fmt.Printf("Comment: %s\n", comment)
		fmt.Printf("Task %s is now REQUEST_CHANGES.\n", id)
		fmt.Println("Next: address the comments, then run: harness check")

	default:
		return fmt.Errorf("unknown decision %q (use 'approve' or 'request-changes')", decision)
	}

	return nil
}
```

- [ ] **Step 4: Implement cmd_review_history.go**

```go
// internal/cmd_review_history.go
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
```

- [ ] **Step 5: Verify build and test**

Run: `make build && make test`
Expected: builds, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/cmd_diff.go internal/cmd_request_review.go internal/cmd_record_review.go internal/cmd_review_history.go
git commit -m "cli: diff, request-review, record-review, review-history commands"
```

---

### Task 11: CLI Command — merge (with queue)

**Files:**
- Modify: `internal/cmd_merge.go`
- Modify: `internal/git.go` (add Rebase to worktree git client)

- [ ] **Step 1: Implement cmd_merge.go**

```go
// internal/cmd_merge.go
package internal

import (
	"fmt"
	"time"
)

func (a *App) CmdMerge(taskID string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}

	task, err := a.DB.GetTask(id)
	if err != nil {
		return err
	}

	if task.State != StateApproved {
		return fmt.Errorf("cannot merge in state %s (need APPROVED)", task.State)
	}

	// Enter queue
	pos, err := a.DB.EnqueueTask(id)
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	if err := a.DB.UpdateTaskState(id, StateQueued); err != nil {
		return err
	}

	fmt.Printf("Queued for merge (position #%d).\n", pos)

	// Wait for our turn
	for {
		first, err := a.DB.PeekQueue()
		if err != nil {
			return fmt.Errorf("peek queue: %w", err)
		}
		if first == id {
			break
		}
		fmt.Printf("Waiting for %s...\n", first)
		time.Sleep(2 * time.Second)
	}

	// Our turn — process the merge
	fmt.Printf("\nNow processing merge for %s...\n", id)
	if err := a.DB.UpdateTaskState(id, StateMerging); err != nil {
		return err
	}

	// Rebase onto main
	wtGit := NewGitClient(task.WorktreePath)
	fmt.Printf("Rebasing %s onto %s...\n", task.Branch, a.Config.MainBranch)
	if err := wtGit.Rebase(a.Config.MainBranch); err != nil {
		wtGit.RebaseAbort()
		a.DB.DequeueTask(id)
		a.DB.UpdateTaskState(id, StateMergeFailed)
		fmt.Printf("Rebase conflict.\n")
		fmt.Printf("\nTask %s is now MERGE_FAILED.\n", id)
		fmt.Println("Fix the conflicts, then run: harness check")
		return nil
	}

	// Re-run checks if configured
	if a.Config.Merge.RerunChecks {
		fmt.Println("Re-running checks...")
		runner := NewCheckRunner(a.Config.Checks, task.WorktreePath)
		results, err := runner.RunAll()
		if err != nil {
			a.DB.DequeueTask(id)
			a.DB.UpdateTaskState(id, StateMergeFailed)
			return fmt.Errorf("check execution error: %w", err)
		}

		allPassed := true
		for _, r := range results {
			if r.Skipped {
				fmt.Printf("  %-12s SKIP\n", r.Name)
			} else if r.ExitCode == 0 {
				fmt.Printf("  %-12s PASS\n", r.Name)
			} else {
				fmt.Printf("  %-12s FAIL\n", r.Name)
				allPassed = false
			}
		}

		if !allPassed {
			a.DB.DequeueTask(id)
			a.DB.UpdateTaskState(id, StateMergeFailed)
			fmt.Printf("\nChecks failed after rebase.\n")
			fmt.Printf("Task %s is now MERGE_FAILED.\n", id)
			fmt.Println("Fix the failures, then run: harness check")
			return nil
		}
	}

	// Merge
	fmt.Printf("Merging %s into %s...\n", task.Branch, a.Config.MainBranch)

	// Checkout main in the main repo, then fast-forward merge
	if err := a.Git.Checkout(a.Config.MainBranch); err != nil {
		a.DB.DequeueTask(id)
		a.DB.UpdateTaskState(id, StateMergeFailed)
		return fmt.Errorf("checkout main: %w", err)
	}

	if err := a.Git.MergeFF(task.Branch); err != nil {
		a.DB.DequeueTask(id)
		a.DB.UpdateTaskState(id, StateMergeFailed)
		fmt.Printf("Merge failed: %s\n", err)
		fmt.Printf("\nTask %s is now MERGE_FAILED.\n", id)
		fmt.Println("Fix the conflicts, then run: harness check")
		return nil
	}

	// Success — clean up
	a.Git.RemoveWorktree(task.WorktreePath)
	a.Git.DeleteBranch(task.Branch)
	a.DB.DequeueTask(id)
	a.DB.UpdateTaskState(id, StateMerged)

	fmt.Println("Merge successful.")
	fmt.Printf("\nCleaned up:\n")
	fmt.Printf("  Worktree: %s (removed)\n", task.WorktreePath)
	fmt.Printf("  Branch: %s (deleted)\n", task.Branch)
	fmt.Printf("\nTask %s is now MERGED.\n", id)

	return nil
}
```

- [ ] **Step 2: Verify build**

Run: `make build && make test`
Expected: builds, tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/cmd_merge.go
git commit -m "cli: merge command with queue, rebase, and check re-run"
```

---

### Task 12: End-to-End Smoke Test

**Files:**
- Create: `internal/e2e_test.go`

- [ ] **Step 1: Write an end-to-end test that exercises the full workflow**

```go
// internal/e2e_test.go
package internal

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EWorkflow(t *testing.T) {
	// Create a temp git repo with a Makefile
	dir := initTestRepo(t)

	// Write a passing test command
	os.WriteFile(filepath.Join(dir, "Makefile"), []byte("test:\n\t@echo all tests pass\n"), 0644)
	cmd := exec.Command("git", "add", "Makefile")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add makefile")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()

	// harness init
	err := RunInit(dir)
	if err != nil {
		t.Fatal("init:", err)
	}

	// Commit the config
	cmd = exec.Command("git", "add", ".harness.yaml", ".gitignore")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()
	cmd = exec.Command("git", "commit", "-m", "add harness config")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Run()

	// Load app
	app, err := LoadApp(dir)
	if err != nil {
		t.Fatal("load app:", err)
	}
	defer app.Close()

	// harness new
	err = app.CmdNew("add hello feature")
	if err != nil {
		t.Fatal("new:", err)
	}

	// Verify task created
	tasks, _ := app.DB.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.State != StateInProgress {
		t.Errorf("state = %s, want IN_PROGRESS", task.State)
	}

	// Make a change in the worktree
	os.WriteFile(filepath.Join(task.WorktreePath, "hello.txt"), []byte("hello\n"), 0644)
	wtCmd := exec.Command("git", "add", "hello.txt")
	wtCmd.Dir = task.WorktreePath
	wtCmd.Env = gitEnv()
	wtCmd.Run()
	wtCmd = exec.Command("git", "commit", "-m", "add hello")
	wtCmd.Dir = task.WorktreePath
	wtCmd.Env = gitEnv()
	wtCmd.Run()

	// harness check
	err = app.CmdCheck(task.ID)
	if err != nil {
		t.Fatal("check:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateChecksPassed {
		t.Errorf("state after check = %s, want CHECKS_PASSED", task.State)
	}

	// harness request-review
	err = app.CmdRequestReview(task.ID)
	if err != nil {
		t.Fatal("request-review:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateReviewRequested {
		t.Errorf("state after request-review = %s, want REVIEW_REQUESTED", task.State)
	}

	// harness record-review approve
	err = app.CmdRecordReview(task.ID, "approve", "")
	if err != nil {
		t.Fatal("record-review:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state after approve = %s, want APPROVED", task.State)
	}

	// harness merge
	err = app.CmdMerge(task.ID)
	if err != nil {
		t.Fatal("merge:", err)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateMerged {
		t.Errorf("state after merge = %s, want MERGED", task.State)
	}

	// Verify file exists on main
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); os.IsNotExist(err) {
		t.Error("hello.txt should exist on main after merge")
	}
}

func gitEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test",
	)
}
```

- [ ] **Step 2: Run the E2E test**

Run: `go test ./internal/ -run TestE2EWorkflow -v`
Expected: PASS — full workflow from init through merge completes.

- [ ] **Step 3: Run all tests**

Run: `make test`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/e2e_test.go
git commit -m "e2e: full workflow smoke test"
```

---

### Task 13: Self-Hosting — Dogfood Setup

**Files:**
- Create: `.harness.yaml` (for this repo)
- Modify: `.gitignore`

- [ ] **Step 1: Create .harness.yaml for the harness repo itself**

```yaml
# .harness.yaml — harness dogfooding itself
main_branch: main
worktree_dir: .worktrees

checks:
  test:
    run: make test
  build:
    run: make build
    needs: [test]

merge:
  require:
    checks: [build]
    review: approved
  strategy: merge
  rerun_checks: true

review:
  type: agent
  prompt: |
    Review this change for: correctness, test coverage, Go conventions.
    You have access to the full codebase.

    Previous review comments:
    {{previous_comments}}

    Task description:
    {{task_description}}

    After reviewing, call one of:
      harness record-review approve
      harness record-review request-changes --comment "your comments here"
```

- [ ] **Step 2: Add .harness/ and .worktrees/ to .gitignore**

Append to `.gitignore`:
```
.harness/
.worktrees/
```

- [ ] **Step 3: Initialize harness database**

Run: `./harness init`
Expected: error "already exists" since config exists. Instead, manually create `.harness/` dir:
```bash
mkdir -p .harness
./harness list   # this triggers DB creation via LoadApp
```

- [ ] **Step 4: Install the binary**

Run: `make install`
Expected: `harness` available in PATH.

- [ ] **Step 5: Verify self-hosting works**

```bash
harness new "test self-hosting"
harness list
harness status task-001
# Make a trivial change in the worktree, commit it
harness check task-001
harness request-review task-001
harness record-review approve task-001
harness merge task-001
```

Expected: full workflow completes. The harness is now using itself.

- [ ] **Step 6: Commit**

```bash
git add .harness.yaml .gitignore
git commit -m "dogfood: self-hosting harness config"
```

---

## Plan Self-Review

**Spec coverage check:**
- [x] Config parsing (.harness.yaml) — Task 2
- [x] SQLite storage (all 4 tables) — Task 4
- [x] State machine (all states + transitions) — Task 3
- [x] Git operations (branch, worktree, diff, merge, rebase) — Task 5
- [x] Check DAG (topo sort, parallel ready detection) — Task 6
- [x] Check runner (parallel execution, output capture) — Task 7
- [x] All 10 commands — Tasks 8-11
- [x] Merge queue — Task 11
- [x] Review flow (agent + human) — Task 10
- [x] E2E smoke test — Task 12
- [x] Self-hosting setup — Task 13

**Placeholder scan:** No TBD/TODO. All code blocks are complete.

**Type consistency:** `CheckConfig`, `TaskState`, `App`, `DB`, `GitClient`, `CheckRunner`, `CheckResult` — used consistently across all tasks.

**Missing from spec but deferred:** Merge strategies (squash/rebase) — the merge command always uses `--ff-only` for now. This is fine for Phase 0; strategies can be added later.
