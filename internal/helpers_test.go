package internal

import "testing"

func TestCountDone(t *testing.T) {
	tasks := []Task{
		{State: StateMerged},
		{State: StateClosed},
		{State: StateInProgress},
		{State: StatePlanned},
		{State: StateMerged},
	}
	done, total := CountDone(tasks)
	if done != 3 {
		t.Errorf("done = %d, want 3", done)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
}

func TestCountDoneEmpty(t *testing.T) {
	done, total := CountDone(nil)
	if done != 0 || total != 0 {
		t.Errorf("CountDone(nil) = %d, %d, want 0, 0", done, total)
	}
}

func TestFormatPreviousComments(t *testing.T) {
	reviews := []Review{
		{Decision: "approve", Comment: "looks good"},
		{Decision: "request_changes", Comment: "fix tests"},
		{Decision: "approve", Comment: ""},
	}
	got := FormatPreviousComments(reviews)
	if got == "(none)" {
		t.Error("expected non-empty comments")
	}
}

func TestFormatPreviousCommentsEmpty(t *testing.T) {
	got := FormatPreviousComments(nil)
	if got != "(none)" {
		t.Errorf("FormatPreviousComments(nil) = %q, want %q", got, "(none)")
	}
}

func TestFormatPreviousCommentsNoComments(t *testing.T) {
	reviews := []Review{
		{Decision: "approve", Comment: ""},
	}
	got := FormatPreviousComments(reviews)
	if got != "(none)" {
		t.Errorf("expected (none) for reviews with empty comments, got %q", got)
	}
}
