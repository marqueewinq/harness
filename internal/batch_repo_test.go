package internal

import (
	"reflect"
	"testing"
	"time"
)

func TestBatchReadsMatchPerTaskReads(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	defer db.Close()
	a, _ := db.CreateTask("a", "", "a", "/tmp/a", "main", "", "")
	b, _ := db.CreateTask("b", "", "b", "/tmp/b", "main", "", "")
	for _, s := range []TaskState{StateChecksRunning, StateChecksFailed, StateChecksRunning, StateChecksFailed} {
		if err := db.UpdateTaskState(a.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	db.RecordCheckRun(a.ID, "test", 1, "out", "err", 1, start, start, "sha1")
	db.RecordCheckRun(a.ID, "lint", 0, "", "", 2, start, start, "sha2")
	db.RecordCheckRun(a.ID, "test", 0, "", "", 2, start.Add(time.Second), start.Add(time.Second), "sha2")
	db.OpenReviewRequest(b.ID, "", "old")
	db.OpenReviewRequest(b.ID, "", "new")
	db.OpenReviewRequest("", "epic-001", "e")
	db.RecordReview(b.ID, "request_changes", "no", "old")
	db.RecordReview(b.ID, "approve", "", "new")
	db.RecordEpicReview("epic-001", "approve", "", "e")

	entered, err := db.StateEnteredAt()
	if err != nil {
		t.Fatal(err)
	}
	trs, _ := db.GetTaskTransitions(a.ID)
	if got, want := entered[a.ID], trs[len(trs)-1].CreatedAt; !got.Equal(want) {
		t.Errorf("entered %s = %s, want last transition %s", a.ID, got, want)
	}
	if _, ok := entered[b.ID]; !ok {
		t.Errorf("no entry for %s", b.ID)
	}

	runs, err := db.LatestCheckRunsByTask()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range runs[a.ID] {
		names = append(names, r.CheckName+"@"+r.CommitSHA)
	}
	if !reflect.DeepEqual(names, []string{"lint@sha2", "test@sha2"}) || len(runs[b.ID]) != 0 {
		t.Errorf("latest runs = %v", runs)
	}

	tasks, epics, err := db.LatestReviewDecisions()
	if err != nil {
		t.Fatal(err)
	}
	if tasks[b.ID] != "approve" || epics["epic-001"] != "approve" || len(tasks) != 1 {
		t.Errorf("decisions = %v %v", tasks, epics)
	}

	reqs, err := db.LatestTaskReviewRequests()
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[b.ID].CommitSHA != "new" {
		t.Errorf("latest requests = %+v", reqs)
	}
}
