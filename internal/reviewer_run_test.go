package internal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func latestRun(t *testing.T, app *App, taskID string) *ReviewRequest {
	t.Helper()
	req, err := app.DB.GetLatestReviewRequest(taskID, "")
	if err != nil || req == nil {
		t.Fatalf("latest review request: %v, %v", req, err)
	}
	return req
}

// assertFailedClosed checks the request is closed with no verdict, so the
// reviewer's token is dead and the user must re-check and re-request.
func assertFailedClosed(t *testing.T, app *App, task *Task, reqErr error, outcome string) {
	t.Helper()
	if reqErr == nil || !strings.Contains(reqErr.Error(), "outcome: "+outcome) {
		t.Fatalf("request err = %v, want reviewer failure with outcome %s", reqErr, outcome)
	}
	if !strings.Contains(reqErr.Error(), "harness work check") {
		t.Errorf("request err = %v, want hint to re-run harness work check", reqErr)
	}
	if task.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", task.State)
	}
	if open, _ := app.DB.GetOpenReviewRequest(task.ID, ""); open != nil {
		t.Errorf("request still open after reviewer failure: %+v", open)
	}
	if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 0 {
		t.Errorf("reviews = %+v, want none", reviews)
	}
	err := app.CmdReviewApprove(task.ID, "")
	if err == nil || !strings.Contains(err.Error(), "no open review request") {
		t.Errorf("approve after failed reviewer: err = %v, want 'no open review request'", err)
	}
	run := latestRun(t, app, task.ID)
	if run.ReviewerOutcome != outcome {
		t.Errorf("outcome = %q, want %q", run.ReviewerOutcome, outcome)
	}
	if run.ReviewerFinished.IsZero() {
		t.Error("reviewer_finished not recorded")
	}
}

func TestReviewerTimeoutKillsProcessGroup(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	cmd := fmt.Sprintf(`sh -c "echo \$\$ > %s; sleep 30" & sleep 30`, pidFile)
	start := time.Now()
	app, task, err := requestWithReviewerConfig(t, cmd, "  timeout: 1s\n")
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("review request took %s; reviewer timeout not enforced", elapsed)
	}
	assertFailedClosed(t, app, task, err, ReviewerTimeout)
	if run := latestRun(t, app, task.ID); run.ReviewerPID <= 0 {
		t.Errorf("reviewer pid not recorded: %+v", run)
	}

	assertPIDFileDead(t, pidFile)
}

func assertPIDFileDead(t *testing.T, pidFile string) {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal("grandchild never started:", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(5 * time.Second)
	for pidAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if pidAlive(pid) {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Errorf("grandchild %d survived; reviewer process group not killed", pid)
	}
}

func TestReviewerCrashRecordsFailed(t *testing.T) {
	t.Parallel()
	app, task, err := requestWithReviewer(t, "exit 3")
	assertFailedClosed(t, app, task, err, ReviewerFailed)
	if run := latestRun(t, app, task.ID); run.ReviewerExitCode != 3 {
		t.Errorf("exit code = %d, want 3", run.ReviewerExitCode)
	}
}

func TestReviewerNoVerdictFailsClosed(t *testing.T) {
	t.Parallel()
	app, task, err := requestWithReviewer(t, "true")
	assertFailedClosed(t, app, task, err, ReviewerNoVerdict)
}

func TestReviewerVerdictWinsOverNonZeroExit(t *testing.T) {
	t.Parallel()
	app, task, err := requestWithReviewer(t, `%s review approve {{task_id}} < /dev/null; exit 5`)
	if err != nil {
		t.Fatal("request:", err)
	}
	if task.State != StateApproved {
		t.Errorf("state = %s, want APPROVED", task.State)
	}
	run := latestRun(t, app, task.ID)
	if run.ReviewerOutcome != ReviewerVerdict || run.ReviewerExitCode != 5 {
		t.Errorf("run = %+v, want outcome verdict, exit 5", run)
	}
}

func TestEpicReviewerNoVerdictReturnsError(t *testing.T) {
	t.Parallel()
	app, _ := setupAppWithReviewCmd(t, "true")
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false); err != nil {
		t.Fatal(err)
	}
	err := app.cmdEpicReviewRequest("epic-001")
	if err == nil || !strings.Contains(err.Error(), "outcome: no_verdict") || !strings.Contains(err.Error(), "harness review request epic-001") {
		t.Fatalf("epic request: err = %v, want no_verdict error with re-request hint", err)
	}
	if open, _ := app.DB.GetOpenReviewRequest("", "epic-001"); open != nil {
		t.Errorf("epic request still open: %+v", open)
	}
}

// startRequestSubprocess prepares a CHECKS_PASSED task whose review.cmd is
// reviewCmd (%s = harness binary) and starts `harness review request` as a
// real process with the given stdin.
func startRequestSubprocess(t *testing.T, reviewCmd, extraReview string, stdin io.Reader) (*App, *Task, *exec.Cmd, *syncBuffer) {
	t.Helper()
	bin := buildHarnessBinary(t)
	if strings.Contains(reviewCmd, "%s") {
		reviewCmd = fmt.Sprintf(reviewCmd, bin)
	}
	app, dir := setupAppWithReviewConfig(t, reviewCmd, extraReview)
	t.Cleanup(func() { app.Close() })
	if err := app.CmdNew("subprocess", "", "", ""); err != nil {
		t.Fatal(err)
	}
	task, _ := app.DB.GetTask("task-001")
	gitCommitFile(t, task.WorktreePath, "f.txt", "f\n", "add f")
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "review", "request", task.ID)
	cmd.Dir = dir
	cmd.Stdin = stdin
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	return app, task, cmd, out
}

type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitExit(t *testing.T, cmd *exec.Cmd, out *syncBuffer, within time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		cmd.Process.Kill()
		t.Fatalf("review request still running after %s; output:\n%s", within, out)
		return nil
	}
}

func TestReviewerKilledOnSignal(t *testing.T) {
	t.Parallel()
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			tmp := t.TempDir()
			marker := filepath.Join(tmp, "started")
			pidFile := filepath.Join(tmp, "grandchild.pid")
			reviewCmd := fmt.Sprintf(`sh -c "echo \$\$ > %s; sleep 30" & touch %s; sleep 30`, pidFile, marker)
			app, task, cmd, out := startRequestSubprocess(t, reviewCmd, "", nil)

			deadline := time.Now().Add(20 * time.Second)
			for {
				_, errM := os.Stat(marker)
				_, errP := os.Stat(pidFile)
				if errM == nil && errP == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("reviewer never started; output:\n%s", out)
				}
				time.Sleep(50 * time.Millisecond)
			}
			cmd.Process.Signal(sig)
			if err := waitExit(t, cmd, out, 15*time.Second); err == nil {
				t.Errorf("review request exited 0 after %s; output:\n%s", sig, out)
			}
			if !strings.Contains(out.String(), "outcome: interrupted") {
				t.Errorf("output lacks interrupted outcome:\n%s", out)
			}
			if run := latestRun(t, app, task.ID); run.ReviewerOutcome != ReviewerInterrupted {
				t.Errorf("outcome = %q, want interrupted", run.ReviewerOutcome)
			}
			if open, _ := app.DB.GetOpenReviewRequest(task.ID, ""); open != nil {
				t.Errorf("request still open after %s: %+v", sig, open)
			}
			assertPIDFileDead(t, pidFile)
		})
	}
}

// The reviewer must not inherit harness's stdin: from a terminal it would be
// stopped by SIGTTIN (own process group); from a pipe it could block forever.
func TestReviewerDoesNotInheritStdin(t *testing.T) {
	t.Parallel()
	// The write end stays open, so a reviewer reading this stdin never gets EOF.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	app, task, cmd, out := startRequestSubprocess(t, `cat > /dev/null; %s review approve {{task_id}}`, "  timeout: 60s\n", pr)
	if err := waitExit(t, cmd, out, 30*time.Second); err != nil {
		t.Fatalf("review request: %v; output:\n%s", err, out)
	}
	task, _ = app.DB.GetTask(task.ID)
	if task.State != StateApproved {
		t.Errorf("state = %s, want APPROVED; output:\n%s", task.State, out)
	}
}

func TestOrphanedReviewerRunShownInStatus(t *testing.T) {
	app, _, task := setupTask(t)
	defer app.Close()
	if err := app.CmdWorkCheck(task.ID); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequest(task.ID); err != nil {
		t.Fatal(err)
	}
	req, _ := app.DB.GetOpenReviewRequest(task.ID, "")
	if err := app.DB.StartReviewerRun(req.ID, deadPID(t), 4242); err != nil {
		t.Fatal(err)
	}

	run := latestRun(t, app, task.ID)
	if got := run.RunStatus(time.Now()); got != ReviewerOrphaned {
		t.Errorf("RunStatus = %q, want orphaned", got)
	}
	out, err := captureStdout(t, func() error { return app.CmdWorkStatus(task.ID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Reviewer run:") || !strings.Contains(out, "orphaned") || !strings.Contains(out, "4242") {
		t.Errorf("status output lacks orphaned reviewer run:\n%s", out)
	}
	if !strings.Contains(out, "harness work check") {
		t.Errorf("status output lacks recovery hint:\n%s", out)
	}
}

func TestLiveReviewerRunNotOrphaned(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()
	task, err := db.CreateTask("t", "", "b", "/tmp/x", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.OpenReviewRequest(task.ID, "", "abc"); err != nil {
		t.Fatal(err)
	}
	req, _ := db.GetOpenReviewRequest(task.ID, "")
	// A live process other than this one, so the own-pid shortcut is not taken.
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { sleeper.Process.Kill(); sleeper.Wait() }()
	if err := db.StartReviewerRun(req.ID, sleeper.Process.Pid, 1); err != nil {
		t.Fatal(err)
	}
	run, _ := db.GetLatestReviewRequest(task.ID, "")
	if got := run.RunStatus(time.Now()); got != ReviewerRunning {
		t.Errorf("RunStatus = %q, want running", got)
	}
	if got := run.RunStatus(time.Now().Add(ReviewerHeartbeatTTL + time.Minute)); got != ReviewerOrphaned {
		t.Errorf("RunStatus with stale heartbeat = %q, want orphaned", got)
	}
}

func TestReviewHistoryShowsFailedReviewerRun(t *testing.T) {
	app, task, err := requestWithReviewer(t, "exit 3")
	if err == nil {
		t.Fatal("request: want error")
	}
	out, err := captureStdout(t, func() error { return app.CmdReviewHistory(task.ID) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "failed") || !strings.Contains(out, "exit 3") {
		t.Errorf("history lacks failed reviewer run:\n%s", out)
	}
	out, _ = captureStdout(t, func() error { return app.CmdWorkStatus(task.ID) })
	if !strings.Contains(out, "Reviewer run:") || !strings.Contains(out, "failed") {
		t.Errorf("status lacks failed reviewer run:\n%s", out)
	}
}

func TestReviewTimeoutConfig(t *testing.T) {
	t.Parallel()
	cfg, err := ParseConfig([]byte("review:\n  type: agent\n  cmd: x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Review.TimeoutDuration(); got != DefaultReviewTimeout {
		t.Errorf("default review timeout = %s, want %s", got, DefaultReviewTimeout)
	}
	cfg, err = ParseConfig([]byte("review:\n  timeout: 90s\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Review.TimeoutDuration(); got != 90*time.Second {
		t.Errorf("review timeout = %s, want 90s", got)
	}
	for _, bad := range []string{"soon", "0s", "-1m"} {
		_, err := ParseConfig([]byte("review:\n  timeout: " + bad + "\n"))
		if err == nil || !strings.Contains(err.Error(), "review.timeout") {
			t.Errorf("timeout %q: err = %v, want review.timeout error", bad, err)
		}
	}
}
