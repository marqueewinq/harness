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
	var pos int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM merge_queue
		WHERE position <= (SELECT position FROM merge_queue WHERE task_id = ?)`,
		taskID,
	).Scan(&pos)
	return pos, err
}
