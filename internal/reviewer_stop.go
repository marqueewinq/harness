package internal

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StopOutcome is how a human ends an in-flight review.
type StopOutcome string

const (
	// StopApprove closes the request with an approve verdict on its commit.
	StopApprove StopOutcome = "approve"
	// StopRevoke closes the request without a verdict; a task returns to CHECKS_PASSED.
	StopRevoke StopOutcome = "revoke"
)

// RecordTerminalStop stops the subject's in-flight review on a decision a
// human confirmed in an interactive UI (e.g. the dashboard), authorized like
// RecordTerminalVerdict. StopApprove records the approval as
// "terminal:<user> (<via>, pre-approved)". The request is closed (its token
// dead) before the reviewer's process group is killed, and the kill only
// happens if the recorded pid is still that reviewer. A non-empty expectSHA
// is the request commit the human was shown; the stop is refused if the open
// request is now for another. The report says what was done, including
// whether a reviewer was killed.
func (a *App) RecordTerminalStop(subject string, outcome StopOutcome, via, expectSHA string) (string, error) {
	if outcome == StopApprove {
		via += ", pre-approved"
	}
	return a.stopReview(subject, outcome, expectSHA, terminalAuth(via))
}

func (a *App) stopReview(subject string, outcome StopOutcome, expectSHA string, auth verdictAuth) (string, error) {
	if outcome != StopApprove && outcome != StopRevoke {
		return "", fmt.Errorf("unknown review stop %q (want approve or revoke)", outcome)
	}
	var taskID, epicID, branch string
	var target TaskState
	if isEpicID(subject) {
		e, err := a.DB.GetEpic(subject)
		if err != nil {
			return "", err
		}
		if outcome == StopApprove && e.Branch == "" {
			return "", fmt.Errorf("epic %s has no branch", subject)
		}
		epicID, branch = subject, e.Branch
	} else {
		task, err := a.DB.GetTask(subject)
		if err != nil {
			return "", err
		}
		if task.State != StateReviewRequested {
			return "", fmt.Errorf("cannot stop review of %s in state %s (need REVIEW_REQUESTED)", subject, task.State)
		}
		taskID, branch = subject, task.Branch
		target = StateChecksPassed
		if outcome == StopApprove {
			target = StateApproved
		}
	}

	req, err := a.openReviewRequestFor(taskID, epicID, branch, expectSHA, outcome == StopApprove)
	if err != nil {
		return "", err
	}
	who, err := auth(req, subject, string(outcome))
	if err != nil {
		return "", err
	}
	approver := ""
	if outcome == StopApprove {
		approver = who
	}
	if err := a.DB.StopReviewRequest(req, approver, target); err != nil {
		return "", err
	}

	var report string
	switch {
	case outcome == StopApprove && taskID != "":
		report = fmt.Sprintf("Review stopped: APPROVE recorded (%s) by %s.\nTask %s is now APPROVED.\nNext: run harness work merge\n",
			shortSHA(req.CommitSHA), who, taskID)
	case outcome == StopApprove:
		report = fmt.Sprintf("Review stopped: APPROVE recorded for epic %s (%s) by %s.\nNext: run harness epic merge %s\n",
			epicID, shortSHA(req.CommitSHA), who, epicID)
	case taskID != "":
		report = fmt.Sprintf("Review request revoked by %s; no verdict recorded.\nTask %s is now CHECKS_PASSED.\n", who, taskID)
	default:
		report = fmt.Sprintf("Review request for epic %s revoked by %s; no verdict recorded.\n", epicID, who)
	}
	killed, err := a.killReviewer(req.ID)
	return report + killed, err
}

// killReviewer runs after the request is closed, so a reviewer that survives
// a refused kill still cannot record a verdict. It kills the request's
// reviewer process group only while the run is live and the recorded pid is
// still that reviewer: pids are reused, and a stale one must not be killed.
func (a *App) killReviewer(reqID int) (string, error) {
	req, err := a.DB.GetReviewRequest(reqID)
	if err != nil {
		return "", err
	}
	if req.ReviewerPID <= 0 || req.RunStatus(time.Now()) != ReviewerRunning {
		return "No live reviewer.\n", nil
	}
	if err := verifyReviewerProcess(req); err != nil {
		return fmt.Sprintf("No live reviewer (pid %d not killed: %v).\n", req.ReviewerPID, err), nil
	}
	if err := syscall.Kill(-req.ReviewerPID, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return "No live reviewer.\n", nil
		}
		return "", fmt.Errorf("review stopped, but killing reviewer pid %d failed: %w", req.ReviewerPID, err)
	}
	return fmt.Sprintf("Reviewer pid %d killed.\n", req.ReviewerPID), nil
}

// reviewerStartSlack covers the gap between starting review.cmd and
// recording its start, plus whole-second rounding on both sides.
const reviewerStartSlack = 5 * time.Second

// verifyReviewerProcess checks that req's reviewer pid still leads its own
// process group and, on Linux, is a child of the recorded owner that started
// when the run was recorded. Elsewhere the caller's live-owner check is all
// there is.
func verifyReviewerProcess(req *ReviewRequest) error {
	pid := req.ReviewerPID
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return fmt.Errorf("not running")
	}
	if pgid != pid {
		return fmt.Errorf("not a process group leader")
	}
	if runtime.GOOS != "linux" {
		return nil
	}
	ppid, start, err := procStat(pid)
	if err != nil {
		return err
	}
	if ppid != req.ReviewerOwnerPID {
		return fmt.Errorf("parent is pid %d, not the review request's pid %d", ppid, req.ReviewerOwnerPID)
	}
	if d := start.Sub(req.ReviewerStarted); d < -reviewerStartSlack || d > reviewerStartSlack {
		return fmt.Errorf("started %s, but the run was recorded at %s",
			start.Format(time.DateTime), req.ReviewerStarted.Format(time.DateTime))
	}
	return nil
}

// userHZ is the fixed unit of clock ticks in /proc on Linux.
const userHZ = 100

func procStat(pid int) (ppid int, start time.Time, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, time.Time{}, err
	}
	// comm may contain spaces and ")", so split after its last ")".
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, time.Time{}, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 20 {
		return 0, time.Time{}, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("malformed /proc/%d/stat: %w", pid, err)
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("malformed /proc/%d/stat: %w", pid, err)
	}
	boot, err := bootTime()
	if err != nil {
		return 0, time.Time{}, err
	}
	return ppid, boot.Add(time.Duration(ticks) * time.Second / userHZ), nil
}

func bootTime() (time.Time, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, fmt.Errorf("malformed btime in /proc/stat: %w", err)
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("no btime in /proc/stat")
}
