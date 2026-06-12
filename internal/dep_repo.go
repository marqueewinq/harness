package internal

import "fmt"

type TaskDependency struct {
	FromTaskID string
	ToTaskID   string
	DepType    string // "blocks" or "related"
}

func (db *DB) AddDependency(fromTaskID, toTaskID, depType string) error {
	if fromTaskID == toTaskID {
		return fmt.Errorf("a task cannot depend on itself")
	}
	if _, err := db.GetTask(fromTaskID); err != nil {
		return err
	}
	if _, err := db.GetTask(toTaskID); err != nil {
		return err
	}
	if depType == "blocks" {
		if err := db.detectCycle(fromTaskID, toTaskID); err != nil {
			return err
		}
	}
	_, err := db.conn.Exec(
		`INSERT OR IGNORE INTO task_dependencies (from_task_id, to_task_id, dep_type) VALUES (?, ?, ?)`,
		fromTaskID, toTaskID, depType,
	)
	if err != nil {
		return fmt.Errorf("adding dependency: %w", err)
	}
	return nil
}

func (db *DB) RemoveDependency(fromTaskID, toTaskID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM task_dependencies WHERE from_task_id = ? AND to_task_id = ?`,
		fromTaskID, toTaskID,
	)
	return err
}

func (db *DB) GetBlockers(toTaskID string) ([]TaskDependency, error) {
	rows, err := db.conn.Query(
		`SELECT from_task_id, to_task_id, dep_type FROM task_dependencies WHERE to_task_id = ? AND dep_type = 'blocks'`, toTaskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []TaskDependency
	for rows.Next() {
		var d TaskDependency
		if err := rows.Scan(&d.FromTaskID, &d.ToTaskID, &d.DepType); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}

func (db *DB) GetDependencies(taskID string) ([]TaskDependency, error) {
	rows, err := db.conn.Query(
		`SELECT from_task_id, to_task_id, dep_type FROM task_dependencies WHERE from_task_id = ? OR to_task_id = ?`,
		taskID, taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deps []TaskDependency
	for rows.Next() {
		var d TaskDependency
		if err := rows.Scan(&d.FromTaskID, &d.ToTaskID, &d.DepType); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}

// detectCycle checks if adding "fromTaskID blocks toTaskID" would create a cycle.
// A cycle exists if toTaskID already transitively blocks fromTaskID.
func (db *DB) detectCycle(fromTaskID, toTaskID string) error {
	visited := map[string]bool{}
	queue := []string{toTaskID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		rows, err := db.conn.Query(
			`SELECT to_task_id FROM task_dependencies WHERE from_task_id = ? AND dep_type = 'blocks'`, current,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var next string
			rows.Scan(&next)
			if next == fromTaskID {
				rows.Close()
				return fmt.Errorf("circular dependency detected")
			}
			if !visited[next] {
				queue = append(queue, next)
			}
		}
		rows.Close()
	}
	return nil
}
