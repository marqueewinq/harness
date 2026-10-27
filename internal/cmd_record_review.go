package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"strings"
)

// ReviewTokenEnv carries the one-time token from `review request` to the
// reviewer process it launches. It is never printed or stored in plaintext.
const ReviewTokenEnv = "HARNESS_REVIEW_TOKEN"

// Replaced in tests, which play the human at the terminal.
var (
	confirmVerdict  = terminalConfirm
	stdinIsTerminal = func() bool {
		fi, err := os.Stdin.Stat()
		return err == nil && fi.Mode()&os.ModeCharDevice != 0
	}
)

func terminalConfirm(question, expect string) bool {
	if !stdinIsTerminal() {
		return false
	}
	fmt.Fprint(os.Stderr, question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line) == expect
}

// verdictAuth decides who records a verdict on req, or refuses.
type verdictAuth func(req *ReviewRequest, subject, decision string) (reviewer string, err error)

func terminalUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

// authorizeVerdict is the CLI's authorizer. Without the token minted for this
// request, only a human at a terminal may record one, so the implementing
// agent cannot review its own work.
func (a *App) authorizeVerdict(req *ReviewRequest, subject, decision string) (string, error) {
	if token := os.Getenv(ReviewTokenEnv); token != "" {
		if !req.TokenMatches(token) {
			return "", fmt.Errorf("%s does not match the open review request for %s", ReviewTokenEnv, subject)
		}
		return "review.cmd", nil
	}
	question := fmt.Sprintf("Record %s for %s at %s? Type %s to confirm: ", decision, subject, shortSHA(req.CommitSHA), subject)
	if confirmVerdict(question, subject) {
		return "terminal:" + terminalUser(), nil
	}
	return "", fmt.Errorf("refusing to record a verdict for %s: only the reviewer launched by `harness review request` (review.cmd) or a human at an interactive terminal may do this", subject)
}

// terminalAuth authorizes a verdict the caller has already confirmed with a
// human in its own UI (the caller owns stdin, so it cannot use
// confirmVerdict). It refuses inside a reviewer's environment, so a reviewer
// agent cannot launch the UI and record a verdict labelled as a human's.
func terminalAuth(via string) verdictAuth {
	return func(req *ReviewRequest, subject, decision string) (string, error) {
		if os.Getenv(ReviewTokenEnv) != "" {
			return "", fmt.Errorf("refusing a %s verdict while %s is set (this is a reviewer's environment)", via, ReviewTokenEnv)
		}
		if !stdinIsTerminal() {
			return "", fmt.Errorf("refusing a %s verdict for %s: stdin is not an interactive terminal", via, subject)
		}
		return fmt.Sprintf("terminal:%s (%s)", terminalUser(), via), nil
	}
}

// RecordTerminalVerdict records a verdict a human confirmed in an interactive
// UI (e.g. the dashboard) and returns the CLI's report instead of printing
// it. It runs the same checks as the CLI verdict commands: open request,
// REVIEW_REQUESTED state, and for approve the branch head must still be the
// requested commit. A non-empty expectSHA is the request commit the human
// was shown; the verdict is refused if the open request is now for another.
func (a *App) RecordTerminalVerdict(subject, decision, comment, via, expectSHA string) (string, error) {
	auth := terminalAuth(via)
	if IsEpicID(subject) {
		return a.epicVerdictReport(subject, decision, comment, expectSHA, auth)
	}
	return a.taskVerdictReport(subject, decision, comment, expectSHA, auth)
}

// openReviewRequestFor returns the open request for the subject and, when
// requireHead is set, refuses if branch moved since the request: the verdict
// must cover exactly the commit the reviewer was shown. expectSHA, when set,
// must match the open request read here, so no separate earlier read can
// race with a newer request.
func (a *App) openReviewRequestFor(taskID, epicID, branch, expectSHA string, requireHead bool) (*ReviewRequest, error) {
	subject := taskID
	hint := fmt.Sprintf("run: harness work check %s, then harness review request %s", subject, subject)
	if epicID != "" {
		subject = epicID
		hint = "run: harness review request " + subject
	}
	req, err := a.DB.GetOpenReviewRequest(taskID, epicID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("no open review request for %s (%s)", subject, hint)
	}
	if expectSHA != "" && req.CommitSHA != expectSHA {
		return nil, fmt.Errorf("review request for %s changed since you opened it: was %s, now %s; reopen the diff",
			subject, shortSHA(expectSHA), shortSHA(req.CommitSHA))
	}
	if !requireHead {
		return req, nil
	}
	head, err := a.Git.RevParse(branch)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", branch, err)
	}
	if head != req.CommitSHA {
		return nil, fmt.Errorf("%s moved since review was requested (%s -> %s); re-run checks and request review again",
			branch, shortSHA(req.CommitSHA), shortSHA(head))
	}
	return req, nil
}

// Reject is pinned to the requested commit but does not require the branch to
// still be there: feedback on the code the reviewer saw stays valid.
func (a *App) recordVerdict(taskID, epicID, branch, decision, comment, expectSHA string, auth verdictAuth) (*ReviewRequest, error) {
	subject := taskID
	if epicID != "" {
		subject = epicID
	}
	switch decision {
	case "approve":
	case "request_changes":
		if comment == "" {
			return nil, fmt.Errorf("--comment is required for reject")
		}
	default:
		return nil, fmt.Errorf("unknown review decision %q (want approve or request_changes)", decision)
	}
	req, err := a.openReviewRequestFor(taskID, epicID, branch, expectSHA, decision == "approve")
	if err != nil {
		return nil, err
	}
	reviewer, err := auth(req, subject, decision)
	if err != nil {
		return nil, err
	}
	if err := a.DB.ResolveReviewRequest(req, decision, comment, reviewer); err != nil {
		return nil, err
	}
	return req, nil
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (a *App) CmdReviewApprove(taskID, comment string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	return a.taskVerdict(id, "approve", comment, a.authorizeVerdict)
}

func (a *App) CmdReviewReject(taskID, comment string) error {
	id, err := a.ResolveTaskID(taskID)
	if err != nil {
		return err
	}
	return a.taskVerdict(id, "request_changes", comment, a.authorizeVerdict)
}

func (a *App) taskVerdict(id, decision, comment string, auth verdictAuth) error {
	report, err := a.taskVerdictReport(id, decision, comment, "", auth)
	fmt.Print(report)
	return err
}

// taskVerdictReport returns what the CLI prints on success instead of
// printing it, so a full-screen UI can record verdicts without writing to
// the terminal.
func (a *App) taskVerdictReport(id, decision, comment, expectSHA string, auth verdictAuth) (string, error) {
	task, err := a.DB.GetTask(id)
	if err != nil {
		return "", err
	}
	if task.State != StateReviewRequested {
		return "", fmt.Errorf("cannot record review in state %s (need REVIEW_REQUESTED)", task.State)
	}
	req, err := a.recordVerdict(id, "", task.Branch, decision, comment, expectSHA, auth)
	if err != nil {
		return "", err
	}
	if decision == "approve" {
		if err := a.DB.UpdateTaskState(id, StateApproved); err != nil {
			return "", err
		}
		return fmt.Sprintf("Review recorded: APPROVE (%s)\nTask %s is now APPROVED.\nNext: run harness work merge\n",
			shortSHA(req.CommitSHA), id), nil
	}
	if err := a.DB.UpdateTaskState(id, StateRequestChanges); err != nil {
		return "", err
	}
	return fmt.Sprintf("Review recorded: REQUEST_CHANGES\nComment: %s\nTask %s is now REQUEST_CHANGES.\nNext: address the comments, then run: harness work check\n",
		comment, id), nil
}
