package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListOpenReviewRequests(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()
	task, err := db.CreateTask("t", "", "b", "/tmp/x", "main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	db.OpenReviewRequest(task.ID, "", "old")
	db.OpenReviewRequest(task.ID, "", "new")
	db.OpenReviewRequest("", "epic-001", "epic")
	open, err := db.ListOpenReviewRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 || open[0].CommitSHA != "new" || open[1].EpicID != "epic-001" {
		t.Errorf("open requests = %+v, want the superseding task request and the epic request", open)
	}
}

func TestCreateLogStartsFreshFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("previous run\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Inode numbers may be reused, so a link to the old file tells a new
	// file from a truncated one.
	link := filepath.Join(dir, "old.link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	f, err := createLog(dir, "test")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if old, _ := os.ReadFile(link); string(old) != "previous run\n" {
		t.Error("createLog truncated the previous run's file; its birth time would not mark this run")
	}
	if after, _ := os.Stat(path); after.Size() != 0 {
		t.Errorf("log size = %d, want 0", after.Size())
	}
}

func TestCommittedReviewConfigIgnoresWorkingCopy(t *testing.T) {
	app, dir := setupApp(t)
	defer app.Close()
	committed, err := app.CommittedReviewConfig()
	if err != nil {
		t.Fatal(err)
	}
	app.Config.Review.Type = "something-local"
	got, err := app.CommittedReviewConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got != committed {
		t.Errorf("CommittedReviewConfig followed app.Config: %+v (repo %s)", got, dir)
	}
}
