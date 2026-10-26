package internal

import (
	"database/sql"
	"time"
)

type QueueEntry struct {
	Position  int
	TaskID    string
	PID       int
	Heartbeat time.Time
	QueuedAt  time.Time
}

// EnqueueTask returns the 1-based rank in the queue. The row is a lease owned
// by pid and must be kept alive with Heartbeat.
func (db *DB) EnqueueTask(taskID string, pid int) (int, error) {
	_, err := db.conn.Exec(
		`INSERT INTO merge_queue (task_id, owner_pid, heartbeat_unix) VALUES (?, ?, ?)`,
		taskID, pid, time.Now().Unix(),
	)
	if err != nil {
		return 0, err
	}
	return db.GetQueuePosition(taskID)
}

func (db *DB) Heartbeat(taskID string) error {
	_, err := db.conn.Exec(`UPDATE merge_queue SET heartbeat_unix = ? WHERE task_id = ?`, time.Now().Unix(), taskID)
	return err
}

func (db *DB) DequeueTask(taskID string) error {
	_, err := db.conn.Exec(`DELETE FROM merge_queue WHERE task_id = ?`, taskID)
	return err
}

// PeekQueue returns "" when the queue is empty.
func (db *DB) PeekQueue() (string, error) {
	head, err := db.queueHead()
	if err != nil || head == nil {
		return "", err
	}
	return head.TaskID, nil
}

func (db *DB) queueHead() (*QueueEntry, error) {
	entries, err := db.listQueue(`LIMIT 1`)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	return &entries[0], nil
}

func (db *DB) ListQueue() ([]QueueEntry, error) { return db.listQueue("") }

func (db *DB) listQueue(limit string) ([]QueueEntry, error) {
	rows, err := db.conn.Query(
		`SELECT position, task_id, owner_pid, heartbeat_unix, queued_at FROM merge_queue ORDER BY position ASC ` + limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []QueueEntry
	for rows.Next() {
		var e QueueEntry
		var hb int64
		if err := rows.Scan(&e.Position, &e.TaskID, &e.PID, &hb, &e.QueuedAt); err != nil {
			return nil, err
		}
		e.Heartbeat = time.Unix(hb, 0)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// RemoveQueueEntryIfUnchanged deletes e only if its lease was not renewed
// since it was read, so a live owner that just heartbeated keeps its slot.
func (db *DB) RemoveQueueEntryIfUnchanged(e QueueEntry) (bool, error) {
	res, err := db.conn.Exec(
		`DELETE FROM merge_queue WHERE task_id = ? AND owner_pid = ? AND heartbeat_unix = ?`,
		e.TaskID, e.PID, e.Heartbeat.Unix(),
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (db *DB) GetQueuePosition(taskID string) (int, error) {
	var pos int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM merge_queue
		WHERE position <= (SELECT position FROM merge_queue WHERE task_id = ?)`,
		taskID,
	).Scan(&pos)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return pos, err
}
