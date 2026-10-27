package internal

import "fmt"

// CmdReviewRequestDispatch dispatches review request to epic or task handler.
func (a *App) CmdReviewRequestDispatch(id string) error {
	if IsEpicID(id) {
		return a.cmdEpicReviewRequest(id)
	}
	return a.CmdReviewRequest(id)
}

// CmdReviewApproveDispatch dispatches review approval to epic or task handler.
func (a *App) CmdReviewApproveDispatch(id, comment string) error {
	if IsEpicID(id) {
		return a.cmdEpicReviewApprove(id, comment)
	}
	return a.CmdReviewApprove(id, comment)
}

// CmdReviewRejectDispatch dispatches review rejection to epic or task handler.
func (a *App) CmdReviewRejectDispatch(id, comment string) error {
	if IsEpicID(id) {
		return a.cmdEpicReviewReject(id, comment)
	}
	return a.CmdReviewReject(id, comment)
}

// CmdReviewHistoryDispatch dispatches review history to epic or task handler.
func (a *App) CmdReviewHistoryDispatch(id string) error {
	if IsEpicID(id) {
		return a.cmdEpicReviewHistory(id)
	}
	return a.CmdReviewHistory(id)
}

// CmdDiffDispatch dispatches diff to epic or task handler.
func (a *App) CmdDiffDispatch(id string) error {
	if IsEpicID(id) {
		return a.CmdEpicDiff(id)
	}
	return a.CmdWorkDiff(id)
}

// CmdEpicDiff shows diff of epic branch vs main.
func (a *App) CmdEpicDiff(epicID string) error {
	e, err := a.DB.GetEpic(epicID)
	if err != nil {
		return err
	}
	if e.Branch == "" {
		return fmt.Errorf("epic %s has no branch", epicID)
	}
	diff, err := a.Git.Diff(a.Config.MainBranch, e.Branch)
	if err != nil {
		return err
	}
	fmt.Println(diff)
	return nil
}
