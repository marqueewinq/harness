package internal

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// refuseReviewOfNone stops `review request` on a subject whose review type
// is none: it has no review step to request.
func refuseReviewOfNone(subject, mergeCmd string) error {
	return fmt.Errorf("%s has review type none: there is no review step; merge directly with %s", subject, mergeCmd)
}

// openReviewFor opens the review request for subject at head and returns the
// review config to deliver it with. For review type human no agent reviewer
// may run and no token is handed out, whatever review.type is committed on
// main, so only a terminal verdict can resolve the request.
func (a *App) openReviewFor(taskID, epicID, head string, rt ReviewType, review ReviewConfig) (ReviewConfig, string, int, error) {
	token, reqID, err := a.DB.OpenReviewRequestID(taskID, epicID, head)
	if err != nil {
		return review, "", 0, err
	}
	if rt != ReviewHuman {
		return review, token, reqID, nil
	}
	// The token was never handed out; dropping its hash also makes every
	// token verdict on this request fail TokenMatches.
	if err := a.DB.clearReviewToken(reqID); err != nil {
		return review, "", 0, err
	}
	subject := taskID + epicID
	fmt.Printf("Review type human: no agent reviewer is launched and no review token is issued.\n")
	fmt.Printf("A human must decide: harness review approve|reject %s at an interactive terminal, or a/r in harness dashboard.\n\n", subject)
	review.Type = "human"
	review.Cmd = ""
	return review, "", reqID, nil
}

func (db *DB) clearReviewToken(reqID int) error {
	_, err := db.conn.Exec(`UPDATE review_requests SET token_hash = '' WHERE id = ?`, reqID)
	return err
}

func unreviewedMergeNote(summary string) string {
	return fmt.Sprintf("merged without review (review: %s)", summary)
}

// unreviewedMergeCommit pins a review-type-none merge to the checked commit,
// since there is no approval to pin to: the latest check run must be on the
// branch's current head, every check in it must have passed, and
// merge.require.checks must have passed on that commit.
func (a *App) unreviewedMergeCommit(task *Task) (string, error) {
	head, err := a.Git.RevParse(task.Branch)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", task.Branch, err)
	}
	runs, err := a.DB.GetLatestCheckRuns(task.ID)
	if err != nil {
		return "", err
	}
	if len(runs) == 0 || runs[0].CommitSHA == "" {
		return "", fmt.Errorf("task %s has no check run pinned to a commit; run: harness work check %s", task.ID, task.ID)
	}
	if runs[0].CommitSHA != head {
		return "", fmt.Errorf("%s moved since checks ran (%s -> %s); re-run checks: harness work check %s",
			task.Branch, shortSHA(runs[0].CommitSHA), shortSHA(head), task.ID)
	}
	var failed []string
	for _, r := range runs {
		if r.ExitCode != 0 {
			failed = append(failed, r.CheckName)
		}
	}
	if len(failed) > 0 {
		return "", fmt.Errorf("latest check run on %s did not pass (%s); fix it, then run: harness work check %s",
			shortSHA(head), strings.Join(failed, ", "), task.ID)
	}
	passed, err := a.DB.PassedChecksAt(task.ID, head)
	if err != nil {
		return "", err
	}
	var missing []string
	for _, name := range a.Config.Merge.Require.Checks {
		if !passed[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("required checks not passed on %s: %s; run: harness work check %s",
			shortSHA(head), strings.Join(missing, ", "), task.ID)
	}
	return head, nil
}

// QueueWithoutReview moves a task from CHECKS_PASSED to QUEUED. It is the only
// path past the review gate without an approval, and re-reads the review type
// in the same transaction so that only a task of type none can take it.
func (db *DB) QueueWithoutReview(id, note string) error {
	return db.WithTx(func(tx *sql.Tx) error {
		var state, reviewType string
		if err := tx.QueryRow(`SELECT state, review_type FROM tasks WHERE id = ?`, id).Scan(&state, &reviewType); err != nil {
			return fmt.Errorf("reading task %s: %w", id, err)
		}
		if TaskState(state) != StateChecksPassed || ReviewType(reviewType) != ReviewNone {
			return fmt.Errorf("task %s cannot be queued without review (state %s, review type %s)", id, state, reviewType)
		}
		if _, err := tx.Exec(`UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`, string(StateQueued), time.Now(), id); err != nil {
			return err
		}
		return recordTransitionNote(tx, "task_state_transitions", "task_id", id, state, string(StateQueued), note)
	})
}
