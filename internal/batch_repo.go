package internal

import (
	"database/sql"
	"time"
)

// Whole-table reads for views that show every task at once (the dashboard
// redraws every few seconds), instead of one query per task.

// StateEnteredAt maps each task with transition history to when it last
// entered its current state.
func (db *DB) StateEnteredAt() (map[string]time.Time, error) {
	rows, err := db.conn.Query(
		`SELECT tr.task_id, tr.created_at FROM task_state_transitions tr
		 JOIN tasks t ON t.id = tr.task_id AND tr.to_state = t.state
		 ORDER BY tr.id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var id string
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, err
		}
		out[id] = at
	}
	return out, rows.Err()
}

// LatestCheckRunsByTask is GetLatestCheckRuns for every task, without the
// captured stdout/stderr.
func (db *DB) LatestCheckRunsByTask() (map[string][]CheckRun, error) {
	rows, err := db.conn.Query(
		`SELECT c.id, c.task_id, c.check_name, c.exit_code, c.started_at, c.finished_at, c.run_group, c.commit_sha
		 FROM check_runs c
		 WHERE c.run_group = (SELECT MAX(run_group) FROM check_runs WHERE task_id = c.task_id)
		 ORDER BY c.task_id, c.started_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]CheckRun{}
	for rows.Next() {
		var r CheckRun
		if err := rows.Scan(&r.ID, &r.TaskID, &r.CheckName, &r.ExitCode, &r.StartedAt, &r.FinishedAt, &r.RunGroup, &r.CommitSHA); err != nil {
			return nil, err
		}
		out[r.TaskID] = append(out[r.TaskID], r)
	}
	return out, rows.Err()
}

// LatestReviewDecisions maps each task and epic to its most recent verdict.
func (db *DB) LatestReviewDecisions() (tasks, epics map[string]string, err error) {
	rows, err := db.conn.Query(`SELECT task_id, epic_id, decision FROM reviews ORDER BY id ASC`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tasks, epics = map[string]string{}, map[string]string{}
	for rows.Next() {
		var taskID, epicID sql.NullString
		var decision string
		if err := rows.Scan(&taskID, &epicID, &decision); err != nil {
			return nil, nil, err
		}
		if epicID.Valid {
			epics[epicID.String] = decision
		} else if taskID.Valid {
			tasks[taskID.String] = decision
		}
	}
	return tasks, epics, rows.Err()
}

// LatestTaskReviewRequests maps each task ever requested for review to its newest request.
func (db *DB) LatestTaskReviewRequests() (map[string]ReviewRequest, error) {
	reqs, err := db.queryReviewRequests(`SELECT ` + reviewRequestColumns + ` FROM review_requests
		 WHERE id IN (SELECT MAX(id) FROM review_requests WHERE task_id IS NOT NULL GROUP BY task_id)`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ReviewRequest, len(reqs))
	for _, r := range reqs {
		out[r.TaskID] = r
	}
	return out, nil
}
