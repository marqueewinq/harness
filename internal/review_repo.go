package internal

import "time"

type Review struct {
	ID        int
	TaskID    string
	Decision  string
	Comment   string
	CreatedAt time.Time
}

func (db *DB) RecordReview(taskID, decision, comment string) error {
	_, err := db.conn.Exec(
		`INSERT INTO reviews (task_id, decision, comment) VALUES (?, ?, ?)`,
		taskID, decision, comment,
	)
	return err
}

func (db *DB) GetReviews(taskID string) ([]Review, error) {
	rows, err := db.conn.Query(
		`SELECT id, task_id, decision, comment, created_at
		 FROM reviews WHERE task_id = ? ORDER BY created_at ASC`, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reviews []Review
	for rows.Next() {
		var r Review
		if err := rows.Scan(&r.ID, &r.TaskID, &r.Decision, &r.Comment, &r.CreatedAt); err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, nil
}
