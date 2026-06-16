package internal

import (
	"path/filepath"
	"testing"
)

func TestDepCRUD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "", "")
	t2, _ := db.CreatePlannedTask("task two", "", "")

	err = db.AddDependency(t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal("add:", err)
	}

	deps, err := db.GetBlockers(t2.ID)
	if err != nil {
		t.Fatal("get blockers:", err)
	}
	if len(deps) != 1 || deps[0].FromTaskID != t1.ID {
		t.Errorf("blockers = %v, want [%s]", deps, t1.ID)
	}

	err = db.RemoveDependency(t1.ID, t2.ID)
	if err != nil {
		t.Fatal("remove:", err)
	}
	deps, _ = db.GetBlockers(t2.ID)
	if len(deps) != 0 {
		t.Errorf("blockers after remove = %v, want empty", deps)
	}
}

func TestDepSelfDependency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "", "")
	err = db.AddDependency(t1.ID, t1.ID, "blocks")
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}

func TestDepCycleDetection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "", "")
	t2, _ := db.CreatePlannedTask("task two", "", "")
	t3, _ := db.CreatePlannedTask("task three", "", "")

	db.AddDependency(t1.ID, t2.ID, "blocks")
	db.AddDependency(t2.ID, t3.ID, "blocks")

	err = db.AddDependency(t3.ID, t1.ID, "blocks")
	if err == nil {
		t.Fatal("expected cycle detection error")
	}

	// "related" should NOT trigger cycle detection
	err = db.AddDependency(t3.ID, t1.ID, "related")
	if err != nil {
		t.Fatal("related deps should allow cycles:", err)
	}
}

func TestDepIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	t1, _ := db.CreatePlannedTask("task one", "", "")
	t2, _ := db.CreatePlannedTask("task two", "", "")

	err = db.AddDependency(t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal(err)
	}
	err = db.AddDependency(t1.ID, t2.ID, "blocks")
	if err != nil {
		t.Fatal("duplicate should be idempotent:", err)
	}
}
