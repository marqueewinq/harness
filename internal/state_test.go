package internal

import "testing"

func TestValidTransitions(t *testing.T) {
	t.Parallel()
	valid := []struct {
		from, to TaskState
	}{
		{StateNew, StateInProgress},
		{StateInProgress, StateChecksRunning},
		{StateChecksRunning, StateChecksPassed},
		{StateChecksRunning, StateChecksFailed},
		{StateChecksFailed, StateChecksRunning},
		{StateChecksPassed, StateReviewRequested},
		{StateReviewRequested, StateApproved},
		{StateReviewRequested, StateRequestChanges},
		{StateRequestChanges, StateChecksRunning},
		{StateApproved, StateQueued},
		{StateApproved, StateChecksRunning},
		{StateQueued, StateMerging},
		{StateMerging, StateMerged},
		{StateMerging, StateMergeFailed},
		{StateMergeFailed, StateChecksRunning},
	}

	for _, tt := range valid {
		if !ValidTransition(tt.from, tt.to) {
			t.Errorf("expected %s -> %s to be valid", tt.from, tt.to)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	t.Parallel()
	invalid := []struct {
		from, to TaskState
	}{
		{StateNew, StateChecksRunning},
		{StateInProgress, StateMerged},
		{StateChecksPassed, StateMerged},
		{StateMerged, StateInProgress},
		{StateQueued, StateChecksPassed},
	}

	for _, tt := range invalid {
		if ValidTransition(tt.from, tt.to) {
			t.Errorf("expected %s -> %s to be invalid", tt.from, tt.to)
		}
	}
}

func TestCloseTransitions(t *testing.T) {
	t.Parallel()
	closeable := []TaskState{
		StatePlanned, StateInProgress, StateChecksPassed, StateChecksFailed,
		StateReviewRequested, StateApproved, StateRequestChanges,
		StateQueued, StateMergeFailed,
	}
	for _, s := range closeable {
		if !ValidTransition(s, StateClosed) {
			t.Errorf("expected %s -> CLOSED to be valid", s)
		}
	}

	notCloseable := []TaskState{
		StateNew, StateChecksRunning, StateMerging, StateMerged, StateClosed,
	}
	for _, s := range notCloseable {
		if ValidTransition(s, StateClosed) {
			t.Errorf("expected %s -> CLOSED to be invalid", s)
		}
	}
}

func TestClosedIsTerminal(t *testing.T) {
	t.Parallel()
	targets, ok := validTransitions[StateClosed]
	if !ok {
		t.Fatal("StateClosed not in validTransitions map")
	}
	if len(targets) != 0 {
		t.Errorf("StateClosed should have no outgoing transitions, got %v", targets)
	}
}

func TestApprovedToInProgress(t *testing.T) {
	t.Parallel()
	if !ValidTransition(StateApproved, StateInProgress) {
		t.Error("expected APPROVED -> IN_PROGRESS to be valid")
	}
}

func TestPlannedTransitions(t *testing.T) {
	t.Parallel()
	if !ValidTransition(StatePlanned, StateInProgress) {
		t.Error("PLANNED -> IN_PROGRESS should be valid")
	}
	if !ValidTransition(StatePlanned, StateClosed) {
		t.Error("PLANNED -> CLOSED should be valid")
	}
	if ValidTransition(StatePlanned, StateChecksRunning) {
		t.Error("PLANNED -> CHECKS_RUNNING should be invalid")
	}
}

func TestCheckableStates(t *testing.T) {
	t.Parallel()
	checkable := []TaskState{
		StateInProgress, StateChecksRunning, StateChecksPassed, StateChecksFailed, StateRequestChanges, StateReviewRequested, StateMergeFailed, StateApproved,
	}
	for _, s := range checkable {
		if !ValidTransition(s, StateChecksRunning) {
			t.Errorf("expected %s -> CHECKS_RUNNING to be valid", s)
		}
	}
}
