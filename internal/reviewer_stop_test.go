package internal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type liveReview struct {
	app       *App
	task      *Task
	cmd       *exec.Cmd
	out       *syncBuffer
	bin, dir  string
	pidFile   string
	tokenFile string
	req       *ReviewRequest
}

// startLiveReviewer runs `harness review request` as a subprocess whose
// reviewer saves its token, starts a grandchild in its process group and
// sleeps, and returns once the run is recorded as running.
func startLiveReviewer(t *testing.T) *liveReview {
	t.Helper()
	tmp := t.TempDir()
	lr := &liveReview{pidFile: filepath.Join(tmp, "grandchild.pid"), tokenFile: filepath.Join(tmp, "token")}
	marker := filepath.Join(tmp, "started")
	reviewCmd := fmt.Sprintf(`echo "$%s" > %s; sh -c "echo \$\$ > %s; sleep 30" & touch %s; sleep 30`,
		ReviewTokenEnv, lr.tokenFile, lr.pidFile, marker)
	lr.app, lr.task, lr.cmd, lr.out, lr.bin, lr.dir = startRequestSubprocessBin(t, reviewCmd, "", nil)

	deadline := time.Now().Add(30 * time.Second)
	for {
		_, errM := os.Stat(marker)
		_, errP := os.Stat(lr.pidFile)
		req, _ := lr.app.DB.GetOpenReviewRequest(lr.task.ID, "")
		if errM == nil && errP == nil && req != nil && req.ReviewerOutcome == ReviewerRunning {
			lr.req = req
			return lr
		}
		if time.Now().After(deadline) {
			t.Fatalf("reviewer never started; output:\n%s", lr.out)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (lr *liveReview) grandchildPID(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(lr.pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

func TestStopReviewApproveKillsLiveReviewer(t *testing.T) {
	t.Parallel()
	lr := startLiveReviewer(t)
	app, task := lr.app, lr.task

	report, err := app.RecordTerminalStop(task.ID, StopApprove, "dashboard", lr.req.CommitSHA)
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("Reviewer pid %d killed", lr.req.ReviewerPID); !strings.Contains(report, want) {
		t.Errorf("report = %q, want %q", report, want)
	}
	if err := waitExit(t, lr.cmd, lr.out, 20*time.Second); err != nil {
		t.Errorf("review request exited with %v; output:\n%s", err, lr.out)
	}
	if !strings.Contains(lr.out.String(), "approved by a human") {
		t.Errorf("output lacks human approval:\n%s", lr.out)
	}
	if strings.Contains(lr.out.String(), "reviewer failed") {
		t.Errorf("output reports a failure:\n%s", lr.out)
	}

	if got, _ := app.DB.GetTask(task.ID); got.State != StateApproved {
		t.Errorf("state = %s, want APPROVED", got.State)
	}
	reviews, _ := app.DB.GetReviews(task.ID)
	if len(reviews) != 1 || reviews[0].Decision != "approve" || reviews[0].CommitSHA != lr.req.CommitSHA ||
		!strings.HasPrefix(reviews[0].Reviewer, "terminal:") || !strings.HasSuffix(reviews[0].Reviewer, "(dashboard, pre-approved)") {
		t.Errorf("reviews = %+v, want one approve by terminal:<user> (dashboard, pre-approved)", reviews)
	}
	if reviews[0].CreatedAt.IsZero() {
		t.Error("approval time not recorded")
	}
	run := latestRun(t, app, task.ID)
	if run.ID != lr.req.ID || run.Resolution != resolutionApprovedByHuman || run.ReviewerOutcome != ReviewerStopped {
		t.Errorf("run = %+v, want request #%d resolved %s, outcome %s", run, lr.req.ID, resolutionApprovedByHuman, ReviewerStopped)
	}
	assertPIDFileDead(t, lr.pidFile)
}

func TestStopReviewRevokeKillsLiveReviewer(t *testing.T) {
	t.Parallel()
	lr := startLiveReviewer(t)
	app, task := lr.app, lr.task

	report, err := app.RecordTerminalStop(task.ID, StopRevoke, "dashboard", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("Reviewer pid %d killed", lr.req.ReviewerPID); !strings.Contains(report, want) {
		t.Errorf("report = %q, want %q", report, want)
	}
	if err := waitExit(t, lr.cmd, lr.out, 20*time.Second); err != nil {
		t.Errorf("review request exited with %v; output:\n%s", err, lr.out)
	}
	if !strings.Contains(lr.out.String(), "revoked by a human") {
		t.Errorf("output lacks revocation:\n%s", lr.out)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateChecksPassed {
		t.Errorf("state = %s, want CHECKS_PASSED", got.State)
	}
	if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 0 {
		t.Errorf("reviews = %+v, want none", reviews)
	}
	run := latestRun(t, app, task.ID)
	if run.Resolution != resolutionRevokedByHuman || run.ReviewerOutcome != ReviewerStopped {
		t.Errorf("run = %+v, want resolved %s, outcome %s", run, resolutionRevokedByHuman, ReviewerStopped)
	}
	assertPIDFileDead(t, lr.pidFile)

	// A leftover reviewer holding the token cannot record a verdict.
	token, err := os.ReadFile(lr.tokenFile)
	if err != nil || strings.TrimSpace(string(token)) == "" {
		t.Fatalf("reviewer token not saved: %v", err)
	}
	late := exec.Command(lr.bin, "review", "approve", task.ID)
	late.Dir = lr.dir
	late.Env = append(os.Environ(), ReviewTokenEnv+"="+strings.TrimSpace(string(token)))
	if out, err := late.CombinedOutput(); err == nil {
		t.Errorf("late verdict with the revoked token accepted:\n%s", out)
	}
	if err := app.DB.ResolveReviewRequest(lr.req, "approve", "", "review.cmd"); err == nil || !strings.Contains(err.Error(), "already resolved") {
		t.Errorf("late verdict on the revoked request: err = %v, want already resolved", err)
	}
	if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 0 {
		t.Errorf("reviews after late verdict = %+v, want none", reviews)
	}
}

func TestStopReviewRefusesChangedRequest(t *testing.T) {
	t.Parallel()
	lr := startLiveReviewer(t)
	app, task := lr.app, lr.task

	stale := strings.Repeat("0", 40)
	for _, outcome := range []StopOutcome{StopApprove, StopRevoke} {
		_, err := app.RecordTerminalStop(task.ID, outcome, "dashboard", stale)
		if err == nil || !strings.Contains(err.Error(), "changed since you opened it") {
			t.Errorf("%s: err = %v, want changed since you opened it", outcome, err)
		}
	}
	if open, _ := app.DB.GetOpenReviewRequest(task.ID, ""); open == nil || open.ID != lr.req.ID {
		t.Errorf("open request = %+v, want #%d left open", open, lr.req.ID)
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", got.State)
	}
	if !pidAlive(lr.req.ReviewerPID) || !pidAlive(lr.grandchildPID(t)) {
		t.Error("reviewer killed although the stop was refused")
	}

	if _, err := app.RecordTerminalStop(task.ID, StopRevoke, "dashboard", lr.req.CommitSHA); err != nil {
		t.Fatal(err)
	}
	waitExit(t, lr.cmd, lr.out, 20*time.Second)
	assertPIDFileDead(t, lr.pidFile)
}

// fakeReviewerRun records a running reviewer on the subject's open request
// owned by ownerPID, with a sleep (this process's child) as the reviewer pid.
func fakeReviewerRun(t *testing.T, app *App, taskID string, ownerPID int, ownGroup bool, startShift time.Duration) (*exec.Cmd, <-chan error) {
	t.Helper()
	sleeper := exec.Command("sleep", "30")
	sleeper.SysProcAttr = &syscall.SysProcAttr{Setpgid: ownGroup}
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- sleeper.Wait() }()
	t.Cleanup(func() { sleeper.Process.Kill() })

	req, _ := app.DB.GetOpenReviewRequest(taskID, "")
	if err := app.DB.StartReviewerRun(req.ID, ownerPID, sleeper.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.conn.Exec(`UPDATE review_requests SET reviewer_started_unix = ? WHERE id = ?`,
		time.Now().Add(startShift).Unix(), req.ID); err != nil {
		t.Fatal(err)
	}
	return sleeper, done
}

func TestStopReviewKillsOnlyVerifiedReviewer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		deadOwner  bool
		ownGroup   bool
		startShift time.Duration
		linuxOnly  bool
		wantKilled bool
	}{
		{"matching group leader", false, true, 0, false, true},
		{"start time mismatch (pid reused)", false, true, -time.Hour, true, false},
		{"not a process group leader", false, false, 0, false, false},
		{"owner dead (orphaned run)", true, true, 0, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.linuxOnly && runtime.GOOS != "linux" {
				t.Skip("process start time is read from /proc")
			}
			app, task := requestedTask(t)
			owner := os.Getpid()
			if tc.deadOwner {
				owner = deadPID(t)
			}
			sleeper, done := fakeReviewerRun(t, app, task.ID, owner, tc.ownGroup, tc.startShift)

			report, err := app.RecordTerminalStop(task.ID, StopRevoke, "dashboard", "")
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := app.DB.GetTask(task.ID); got.State != StateChecksPassed {
				t.Errorf("state = %s, want CHECKS_PASSED", got.State)
			}
			if tc.wantKilled {
				if want := fmt.Sprintf("Reviewer pid %d killed", sleeper.Process.Pid); !strings.Contains(report, want) {
					t.Errorf("report = %q, want %q", report, want)
				}
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("verified reviewer not killed")
				}
				return
			}
			if !strings.Contains(report, "No live reviewer") {
				t.Errorf("report = %q, want No live reviewer", report)
			}
			select {
			case <-done:
				t.Error("unverified pid was killed")
			case <-time.After(500 * time.Millisecond):
			}
		})
	}
}

func TestStopReviewWithoutLiveReviewer(t *testing.T) {
	t.Parallel()
	t.Run("approve", func(t *testing.T) {
		t.Parallel()
		app, task := requestedTask(t)
		report, err := app.RecordTerminalStop(task.ID, StopApprove, "dashboard", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(report, "No live reviewer") || !strings.Contains(report, "Task "+task.ID+" is now APPROVED") {
			t.Errorf("report = %q", report)
		}
		if got, _ := app.DB.GetTask(task.ID); got.State != StateApproved {
			t.Errorf("state = %s, want APPROVED", got.State)
		}
		if reviews, _ := app.DB.GetReviews(task.ID); len(reviews) != 1 || reviews[0].Decision != "approve" {
			t.Errorf("reviews = %+v", reviews)
		}
		if _, err := app.RecordTerminalStop(task.ID, StopApprove, "dashboard", ""); err == nil {
			t.Error("second stop: want error")
		}
	})
	t.Run("revoke", func(t *testing.T) {
		t.Parallel()
		app, task := requestedTask(t)
		report, err := app.RecordTerminalStop(task.ID, StopRevoke, "dashboard", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(report, "No live reviewer") || !strings.Contains(report, "Task "+task.ID+" is now CHECKS_PASSED") {
			t.Errorf("report = %q", report)
		}
		if open, _ := app.DB.GetOpenReviewRequest(task.ID, ""); open != nil {
			t.Errorf("request still open: %+v", open)
		}
		if err := app.CmdReviewRequest(task.ID); err != nil {
			t.Errorf("review request after revoke: %v", err)
		}
	})
}

func TestStopReviewEpic(t *testing.T) {
	t.Parallel()
	app, _ := setupApp(t)
	defer app.Close()
	if err := app.CmdProjectNew("P", ""); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdEpicNew("E", "", "proj-001", "", false); err != nil {
		t.Fatal(err)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordTerminalStop("epic-001", StopRevoke, "dashboard", ""); err != nil {
		t.Fatal(err)
	}
	if reviews, _ := app.DB.GetEpicReviews("epic-001"); len(reviews) != 0 {
		t.Errorf("reviews after revoke = %+v", reviews)
	}
	if err := app.CmdReviewRequestDispatch("epic-001"); err != nil {
		t.Fatal(err)
	}
	report, err := app.RecordTerminalStop("epic-001", StopApprove, "dashboard", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "epic-001") {
		t.Errorf("report = %q", report)
	}
	approval, _ := app.DB.GetLatestEpicApproval("epic-001")
	if approval == nil || !strings.HasSuffix(approval.Reviewer, "(dashboard, pre-approved)") {
		t.Errorf("epic approval = %+v", approval)
	}
}

func TestStopReviewRejectsUnknownOutcome(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	if _, err := app.RecordTerminalStop(task.ID, StopOutcome("reject"), "dashboard", ""); err == nil {
		t.Fatal("want error")
	}
	if open, _ := app.DB.GetOpenReviewRequest(task.ID, ""); open == nil {
		t.Error("request must stay open")
	}
}

// Not parallel: swaps a package-level hook.
func TestStopReviewRefusedWithoutTerminal(t *testing.T) {
	app, task := requestedTask(t)
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	defer func() { stdinIsTerminal = orig }()
	for _, outcome := range []StopOutcome{StopApprove, StopRevoke} {
		if _, err := app.RecordTerminalStop(task.ID, outcome, "dashboard", ""); err == nil || !strings.Contains(err.Error(), "not an interactive terminal") {
			t.Errorf("%s: err = %v", outcome, err)
		}
	}
	if got, _ := app.DB.GetTask(task.ID); got.State != StateReviewRequested {
		t.Errorf("state = %s, want REVIEW_REQUESTED", got.State)
	}
	if open, _ := app.DB.GetOpenReviewRequest(task.ID, ""); open == nil {
		t.Error("request must stay open")
	}
}

func TestFinishReviewerRunAfterHumanStop(t *testing.T) {
	t.Parallel()
	app, task := requestedTask(t)
	req, _ := app.DB.GetOpenReviewRequest(task.ID, "")
	if err := app.DB.StartReviewerRun(req.ID, os.Getpid(), 1); err != nil {
		t.Fatal(err)
	}
	if err := app.DB.StopReviewRequest(req, "", StateChecksPassed); err != nil {
		t.Fatal(err)
	}
	stored, err := app.DB.FinishReviewerRun(req.ID, ReviewerFailed, -1)
	if err != nil {
		t.Fatal(err)
	}
	if stored != ReviewerStopped {
		t.Errorf("stored outcome = %q, want %q", stored, ReviewerStopped)
	}
	if run := latestRun(t, app, task.ID); run.Resolution != resolutionRevokedByHuman {
		t.Errorf("resolution = %q, want %q", run.Resolution, resolutionRevokedByHuman)
	}
}
