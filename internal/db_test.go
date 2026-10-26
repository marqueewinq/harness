package internal

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	if err := db.conn.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
	var timeout int
	if err := db.conn.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout == 0 {
		t.Error("busy_timeout should be set")
	}
}

func TestNextIDPast999(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	for _, id := range []string{"task-999", "task-1000", "task-002"} {
		if _, err := db.conn.Exec(`INSERT INTO tasks (id, title, state) VALUES (?, 't', 'PLANNED')`, id); err != nil {
			t.Fatal(err)
		}
	}
	task, err := db.CreatePlannedTask("next", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "task-1001" {
		t.Errorf("ID = %q, want task-1001", task.ID)
	}
}

func TestConcurrentCreateTask(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "harness.db")
	first, err := OpenDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	const n = 20
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			db, err := OpenDB(path)
			if err != nil {
				errs <- err
				return
			}
			defer db.Close()
			_, err = db.CreatePlannedTask("t", "", "")
			errs <- err
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent create: %v", err)
		}
	}
	tasks, _ := first.ListTasks()
	if len(tasks) != n {
		t.Errorf("tasks = %d, want %d", len(tasks), n)
	}
}

func TestTaskCRUD(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	task, err := db.CreateTask("fix bug", "", "fix-bug", ".worktrees/fix-bug", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "task-001" {
		t.Errorf("ID = %q, want %q", task.ID, "task-001")
	}
	if task.State != StateInProgress {
		t.Errorf("State = %q, want %q", task.State, StateInProgress)
	}

	task2, err := db.CreateTask("add feature", "", "add-feature", ".worktrees/add-feature", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if task2.ID != "task-002" {
		t.Errorf("ID = %q, want %q", task2.ID, "task-002")
	}

	got, err := db.GetTask("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "fix bug" {
		t.Errorf("Title = %q", got.Title)
	}

	tasks, err := db.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("len(tasks) = %d, want 2", len(tasks))
	}

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
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("fix bug", "", "fix-bug", ".worktrees/fix-bug", "main", "", "")
	got, err := db.GetTaskByBranch("fix-bug")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "task-001" {
		t.Errorf("ID = %q", got.ID)
	}
}

func TestCheckRunCRUD(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	err := db.RecordCheckRun("task-001", "test", 0, "ok\n", "", 1, time.Now(), time.Now(), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	err = db.RecordCheckRun("task-001", "lint", 1, "", "fail\n", 1, time.Now(), time.Now(), "abc123")
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
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	for _, decision := range []string{"request_changes", "approve"} {
		token, err := db.OpenReviewRequest("task-001", "", "abc")
		if err != nil {
			t.Fatal(err)
		}
		req, err := db.GetOpenReviewRequest("task-001", "")
		if err != nil {
			t.Fatal(err)
		}
		if !req.TokenMatches(token) || req.TokenMatches("wrong") || req.TokenMatches("") {
			t.Fatal("token must match only its own request")
		}
		if err := db.ResolveReviewRequest(req, decision, "c", "review.cmd"); err != nil {
			t.Fatal(err)
		}
	}

	reviews, err := db.GetReviews("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 {
		t.Fatalf("len(reviews) = %d, want 2", len(reviews))
	}
	if reviews[0].Decision != "request_changes" || reviews[1].Reviewer != "review.cmd" || reviews[1].CommitSHA != "abc" {
		t.Errorf("reviews = %+v", reviews)
	}
}

func TestReviewTokenNotStoredInPlaintext(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	token, err := db.OpenReviewRequest("task-001", "", "abc")
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM review_requests WHERE token_hash = ? OR commit_sha = ?`, token, token).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("raw token must not be stored")
	}
}

func TestMergeQueue(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("task A", "", "branch-a", ".worktrees/a", "main", "", "")
	db.CreateTask("task B", "", "branch-b", ".worktrees/b", "main", "", "")

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

func TestGetTaskNotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	_, err := db.GetTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestGetTaskByBranchNotFound(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	_, err := db.GetTaskByBranch("nope")
	if err == nil {
		t.Error("expected error for nonexistent branch")
	}
}

func TestGetSubtasks(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	// Create parent
	parent, err := db.CreateTask("parent", "", "parent-branch", ".worktrees/parent", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}

	// Create 2 children
	_, err = db.CreateTask("child 1", "", "child-1", ".worktrees/child-1", parent.Branch, parent.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateTask("child 2", "", "child-2", ".worktrees/child-2", parent.Branch, parent.ID, "")
	if err != nil {
		t.Fatal(err)
	}

	subtasks, err := db.GetSubtasks(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subtasks) != 2 {
		t.Errorf("len(subtasks) = %d, want 2", len(subtasks))
	}
	if subtasks[0].Title != "child 1" {
		t.Errorf("subtasks[0].Title = %q, want %q", subtasks[0].Title, "child 1")
	}
	if subtasks[1].Title != "child 2" {
		t.Errorf("subtasks[1].Title = %q, want %q", subtasks[1].Title, "child 2")
	}
}

func TestGetSubtasksNone(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	parent, err := db.CreateTask("parent", "", "parent-branch", ".worktrees/parent", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}

	subtasks, err := db.GetSubtasks(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subtasks) != 0 {
		t.Errorf("len(subtasks) = %d, want 0", len(subtasks))
	}
}

func TestNextRunGroup(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	// No runs yet -> group 1
	group, err := db.NextRunGroup("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if group != 1 {
		t.Errorf("NextRunGroup (no runs) = %d, want 1", group)
	}

	// Record a run in group 1
	db.RecordCheckRun("task-001", "lint", 0, "", "", 1, time.Now(), time.Now(), "def456")

	// Next group should be 2
	group, err = db.NextRunGroup("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if group != 2 {
		t.Errorf("NextRunGroup (after group 1) = %d, want 2", group)
	}
}

func TestGetLatestCheckRunsEmpty(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	runs, err := db.GetLatestCheckRuns("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if runs != nil {
		t.Errorf("GetLatestCheckRuns (empty) = %v, want nil", runs)
	}
}

func TestPeekQueueEmpty(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	_, err := db.PeekQueue()
	if err == nil {
		t.Error("expected error for empty queue")
	}
}

func TestGetQueuePositionMultiple(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("task A", "", "branch-a", ".worktrees/a", "main", "", "")
	db.CreateTask("task B", "", "branch-b", ".worktrees/b", "main", "", "")
	db.CreateTask("task C", "", "branch-c", ".worktrees/c", "main", "", "")

	db.EnqueueTask("task-001")
	db.EnqueueTask("task-002")
	db.EnqueueTask("task-003")

	pos1, err := db.GetQueuePosition("task-001")
	if err != nil {
		t.Fatal(err)
	}
	pos2, err := db.GetQueuePosition("task-002")
	if err != nil {
		t.Fatal(err)
	}
	pos3, err := db.GetQueuePosition("task-003")
	if err != nil {
		t.Fatal(err)
	}

	if pos1 != 1 {
		t.Errorf("position of task-001 = %d, want 1", pos1)
	}
	if pos2 != 2 {
		t.Errorf("position of task-002 = %d, want 2", pos2)
	}
	if pos3 != 3 {
		t.Errorf("position of task-003 = %d, want 3", pos3)
	}
}

func TestRecordReviewWithCommitSHA(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	sha := "abc123def456abc123def456abc123def456abc1"
	err := db.RecordReview("task-001", "approve", "", sha)
	if err != nil {
		t.Fatal(err)
	}

	reviews, err := db.GetReviews("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 {
		t.Fatalf("len(reviews) = %d, want 1", len(reviews))
	}
	if reviews[0].CommitSHA != sha {
		t.Errorf("CommitSHA = %q, want %q", reviews[0].CommitSHA, sha)
	}
}

func TestGetLatestApproval(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	// Record request_changes, then approve with SHA
	db.RecordReview("task-001", "request_changes", "fix it", "")
	sha := "abc123def456abc123def456abc123def456abc1"
	db.RecordReview("task-001", "approve", "", sha)

	approval, err := db.GetLatestApproval("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if approval == nil {
		t.Fatal("expected non-nil approval")
	}
	if approval.Decision != "approve" {
		t.Errorf("Decision = %q, want %q", approval.Decision, "approve")
	}
	if approval.CommitSHA != sha {
		t.Errorf("CommitSHA = %q, want %q", approval.CommitSHA, sha)
	}
}

func TestGetLatestApprovalNone(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")

	approval, err := db.GetLatestApproval("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if approval != nil {
		t.Errorf("expected nil approval, got %+v", approval)
	}
}

func TestDequeueNonexistent(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	// DELETE is idempotent - should not error
	err := db.DequeueTask("nonexistent-task")
	if err != nil {
		t.Errorf("DequeueTask nonexistent should not error, got: %v", err)
	}
}

func TestSchemaVersion(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	var version int
	err := db.conn.QueryRow("SELECT version FROM schema_version").Scan(&version)
	if err != nil {
		t.Fatal("schema_version table missing:", err)
	}
	if version < 6 {
		t.Errorf("schema version = %d, want >= 6", version)
	}
}

func TestPlanningTablesExist(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	tables := []string{"projects", "epics", "task_dependencies"}
	for _, table := range tables {
		var name string
		err := db.conn.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestProjectCRUD(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	p, err := db.CreateProject("Auth redesign", "Redesign the auth system")
	if err != nil {
		t.Fatal("create:", err)
	}
	if p.ID != "proj-001" {
		t.Errorf("id = %q, want proj-001", p.ID)
	}
	if p.Status != ProjectStatusOpen {
		t.Errorf("status = %q, want open", p.Status)
	}

	p2, err := db.GetProject("proj-001")
	if err != nil {
		t.Fatal("get:", err)
	}
	if p2.Title != "Auth redesign" {
		t.Errorf("title = %q", p2.Title)
	}

	db.CreateProject("Second project", "")
	projects, err := db.ListProjects()
	if err != nil {
		t.Fatal("list:", err)
	}
	if len(projects) != 2 {
		t.Fatalf("len = %d, want 2", len(projects))
	}

	db.UpdateProjectStatus("proj-001", ProjectStatusClosed)
	p3, _ := db.GetProject("proj-001")
	if p3.Status != ProjectStatusClosed {
		t.Errorf("status = %q, want closed", p3.Status)
	}
}

func TestEpicCRUD(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	proj, _ := db.CreateProject("Test project", "")

	e, err := db.CreateEpic("OAuth2 support", "", proj.ID, "")
	if err != nil {
		t.Fatal("create:", err)
	}
	if e.ID != "epic-001" {
		t.Errorf("id = %q, want epic-001", e.ID)
	}
	if e.ProjectID != proj.ID {
		t.Errorf("project_id = %q, want %q", e.ProjectID, proj.ID)
	}

	e2, err := db.GetEpic("epic-001")
	if err != nil {
		t.Fatal("get:", err)
	}
	if e2.Title != "OAuth2 support" {
		t.Errorf("title = %q", e2.Title)
	}

	db.CreateEpic("Rate limiting", "", proj.ID, "")
	epics, _ := db.ListEpicsByProject(proj.ID)
	if len(epics) != 2 {
		t.Fatalf("len = %d, want 2", len(epics))
	}

	all, _ := db.ListEpics()
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}

	db.UpdateEpicStatus("epic-001", EpicStatusClosed)
	e3, _ := db.GetEpic("epic-001")
	if e3.Status != EpicStatusClosed {
		t.Errorf("status = %q, want closed", e3.Status)
	}
}

func TestEpicRequiresProject(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	_, err := db.CreateEpic("Orphan epic", "", "proj-999", "")
	if err == nil {
		t.Fatal("expected error for nonexistent project")
	}
}

func TestEpicBranchCRUD(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	proj, _ := db.CreateProject("Test project", "")

	e, err := db.CreateEpic("With branch", "", proj.ID, "epic/with-branch")
	if err != nil {
		t.Fatal("create:", err)
	}
	if e.Branch != "epic/with-branch" {
		t.Errorf("branch = %q, want epic/with-branch", e.Branch)
	}

	e2, _ := db.GetEpic(e.ID)
	if e2.Branch != "epic/with-branch" {
		t.Errorf("get branch = %q, want epic/with-branch", e2.Branch)
	}

	e3, _ := db.CreateEpic("No branch", "", proj.ID, "")
	if e3.Branch != "" {
		t.Errorf("branch = %q, want empty", e3.Branch)
	}

	db.UpdateEpicBranch(e.ID, "new-branch")
	e4, _ := db.GetEpic(e.ID)
	if e4.Branch != "new-branch" {
		t.Errorf("updated branch = %q, want new-branch", e4.Branch)
	}

	db.UpdateEpicBranch(e.ID, "")
	e5, _ := db.GetEpic(e.ID)
	if e5.Branch != "" {
		t.Errorf("cleared branch = %q, want empty", e5.Branch)
	}

	epics, _ := db.ListEpics()
	if len(epics) != 2 {
		t.Fatalf("len = %d, want 2", len(epics))
	}
}

func TestEpicReviewCRUD(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	proj, _ := db.CreateProject("Test project", "")
	db.CreateEpic("Test epic", "", proj.ID, "")

	err := db.RecordEpicReview("epic-001", "request_changes", "fix it", "")
	if err != nil {
		t.Fatal("record:", err)
	}
	sha := "abc123def456"
	err = db.RecordEpicReview("epic-001", "approve", "", sha)
	if err != nil {
		t.Fatal("record approve:", err)
	}

	reviews, err := db.GetEpicReviews("epic-001")
	if err != nil {
		t.Fatal("get:", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("len = %d, want 2", len(reviews))
	}
	if reviews[0].Decision != "request_changes" {
		t.Errorf("decision[0] = %q", reviews[0].Decision)
	}
	if reviews[1].Decision != "approve" {
		t.Errorf("decision[1] = %q", reviews[1].Decision)
	}

	approval, err := db.GetLatestEpicApproval("epic-001")
	if err != nil {
		t.Fatal("latest:", err)
	}
	if approval == nil {
		t.Fatal("expected approval")
	}
	if approval.CommitSHA != sha {
		t.Errorf("sha = %q, want %q", approval.CommitSHA, sha)
	}

	none, _ := db.GetLatestEpicApproval("epic-999")
	if none != nil {
		t.Errorf("expected nil for nonexistent")
	}
}

func TestTaskStateTransitions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("fix bug", "", "fix-bug", ".worktrees/fix-bug", "main", "", "")
	transitions, err := db.GetTaskTransitions("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("len(transitions) = %d, want 1", len(transitions))
	}
	if transitions[0].FromState != "" || transitions[0].ToState != "IN_PROGRESS" {
		t.Errorf("initial transition = %q -> %q, want '' -> 'IN_PROGRESS'", transitions[0].FromState, transitions[0].ToState)
	}

	db.UpdateTaskState("task-001", StateChecksRunning)
	db.UpdateTaskState("task-001", StateChecksPassed)
	transitions, _ = db.GetTaskTransitions("task-001")
	if len(transitions) != 3 {
		t.Fatalf("len(transitions) = %d, want 3", len(transitions))
	}
	if transitions[1].FromState != "IN_PROGRESS" || transitions[1].ToState != "CHECKS_RUNNING" {
		t.Errorf("transition[1] = %q -> %q", transitions[1].FromState, transitions[1].ToState)
	}
	if transitions[2].FromState != "CHECKS_RUNNING" || transitions[2].ToState != "CHECKS_PASSED" {
		t.Errorf("transition[2] = %q -> %q", transitions[2].FromState, transitions[2].ToState)
	}
}

func TestPlannedTaskTransitions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreatePlannedTask("plan it", "", "")
	transitions, _ := db.GetTaskTransitions("task-001")
	if len(transitions) != 1 {
		t.Fatalf("len = %d, want 1", len(transitions))
	}
	if transitions[0].ToState != "PLANNED" {
		t.Errorf("to = %q, want PLANNED", transitions[0].ToState)
	}

	db.StartTask("task-001", "branch", "/tmp/wt", "main", "")
	transitions, _ = db.GetTaskTransitions("task-001")
	if len(transitions) != 2 {
		t.Fatalf("len = %d, want 2", len(transitions))
	}
	if transitions[1].FromState != "PLANNED" || transitions[1].ToState != "IN_PROGRESS" {
		t.Errorf("transition[1] = %q -> %q", transitions[1].FromState, transitions[1].ToState)
	}
}

func TestEpicStateTransitions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateProject("proj", "")
	db.CreateEpic("epic", "", "proj-001", "")

	transitions, _ := db.GetEpicTransitions("epic-001")
	if len(transitions) != 1 {
		t.Fatalf("len = %d, want 1", len(transitions))
	}
	if transitions[0].ToState != "open" {
		t.Errorf("to = %q, want open", transitions[0].ToState)
	}

	db.UpdateEpicStatus("epic-001", EpicStatusClosed)
	transitions, _ = db.GetEpicTransitions("epic-001")
	if len(transitions) != 2 {
		t.Fatalf("len = %d, want 2", len(transitions))
	}
	if transitions[1].FromState != "open" || transitions[1].ToState != "closed" {
		t.Errorf("transition[1] = %q -> %q", transitions[1].FromState, transitions[1].ToState)
	}
}

func TestProjectStateTransitions(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateProject("proj", "")
	transitions, _ := db.GetProjectTransitions("proj-001")
	if len(transitions) != 1 {
		t.Fatalf("len = %d, want 1", len(transitions))
	}
	if transitions[0].ToState != "open" {
		t.Errorf("to = %q, want open", transitions[0].ToState)
	}

	db.UpdateProjectStatus("proj-001", ProjectStatusClosed)
	transitions, _ = db.GetProjectTransitions("proj-001")
	if len(transitions) != 2 {
		t.Fatalf("len = %d, want 2", len(transitions))
	}
	if transitions[1].FromState != "open" || transitions[1].ToState != "closed" {
		t.Errorf("transition[1] = %q -> %q", transitions[1].FromState, transitions[1].ToState)
	}
}

func TestCheckRunCommitSHA(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	db.CreateTask("test", "", "test-branch", ".worktrees/test", "main", "", "")
	sha := "abc123def456"
	db.RecordCheckRun("task-001", "lint", 0, "ok", "", 1, time.Now(), time.Now(), sha)

	runs, err := db.GetLatestCheckRuns("task-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("len = %d, want 1", len(runs))
	}
	if runs[0].CommitSHA != sha {
		t.Errorf("CommitSHA = %q, want %q", runs[0].CommitSHA, sha)
	}
}

func TestTransitionTablesExist(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	tables := []string{"task_state_transitions", "epic_state_transitions", "project_state_transitions"}
	for _, table := range tables {
		var name string
		err := db.conn.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestTaskNullableBranch(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	_, err := db.conn.Exec(
		`INSERT INTO tasks (id, title, branch, worktree_path, state, created_at, updated_at)
		 VALUES ('task-001', 'test', NULL, NULL, 'PLANNED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	)
	if err != nil {
		t.Fatal("inserting task with NULL branch:", err)
	}

	_, err = db.conn.Exec(
		`INSERT INTO tasks (id, title, branch, worktree_path, state, created_at, updated_at)
		 VALUES ('task-002', 'test2', NULL, NULL, 'PLANNED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
	)
	if err != nil {
		t.Fatal("second NULL branch should be allowed:", err)
	}
}

func TestUpdateTaskStateRejectsInvalidTransition(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	task, err := db.CreateTask("test task", "", "test-branch", "/tmp/wt", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Task starts as IN_PROGRESS. Trying to go directly to MERGED should fail.
	err = db.UpdateTaskState(task.ID, StateMerged)
	if err == nil {
		t.Fatal("expected error for invalid transition IN_PROGRESS -> MERGED")
	}
	// Verify state is unchanged
	got, _ := db.GetTask(task.ID)
	if got.State != StateInProgress {
		t.Errorf("state should still be IN_PROGRESS, got %s", got.State)
	}
}

func TestUpdateTaskStateAcceptsValidTransition(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	task, err := db.CreateTask("test task", "", "test-branch", "/tmp/wt", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// IN_PROGRESS -> CHECKS_RUNNING should succeed
	err = db.UpdateTaskState(task.ID, StateChecksRunning)
	if err != nil {
		t.Fatalf("expected valid transition to succeed: %v", err)
	}
	got, _ := db.GetTask(task.ID)
	if got.State != StateChecksRunning {
		t.Errorf("state = %s, want CHECKS_RUNNING", got.State)
	}
}

func TestStartTaskRejectsNonPlannedState(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()

	// Create an IN_PROGRESS task (not PLANNED)
	task, err := db.CreateTask("active task", "", "branch", "/tmp/wt", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// StartTask should fail because task is IN_PROGRESS, not PLANNED
	err = db.StartTask(task.ID, "new-branch", "/tmp/wt2", "main", "")
	if err == nil {
		t.Fatal("expected error starting non-PLANNED task")
	}
}
