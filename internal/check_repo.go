package internal

import "time"

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
	Skipped    bool
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
