package internal

// RecordReview opens and resolves a request in one step, for tests that only
// need review rows to exist.
func (db *DB) RecordReview(taskID, decision, comment, commitSHA string) error {
	return db.recordTestReview(taskID, "", decision, comment, commitSHA)
}

func (db *DB) RecordEpicReview(epicID, decision, comment, commitSHA string) error {
	return db.recordTestReview("", epicID, decision, comment, commitSHA)
}

func (db *DB) recordTestReview(taskID, epicID, decision, comment, commitSHA string) error {
	if _, err := db.OpenReviewRequest(taskID, epicID, commitSHA); err != nil {
		return err
	}
	req, err := db.GetOpenReviewRequest(taskID, epicID)
	if err != nil {
		return err
	}
	return db.ResolveReviewRequest(req, decision, comment, "test")
}
