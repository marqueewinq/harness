package internal

import "testing"

func TestValidTransitions(t *testing.T) {
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
	invalid := []struct {
		from, to TaskState
	}{
		{StateNew, StateChecksRunning},
		{StateInProgress, StateMerged},
		{StateChecksPassed, StateMerged},
		{StateMerged, StateInProgress},
		{StateApproved, StateInProgress},
		{StateQueued, StateChecksPassed},
	}

	for _, tt := range invalid {
		if ValidTransition(tt.from, tt.to) {
			t.Errorf("expected %s -> %s to be invalid", tt.from, tt.to)
		}
	}
}

func TestCheckableStates(t *testing.T) {
	checkable := []TaskState{
		StateInProgress, StateChecksFailed, StateRequestChanges, StateMergeFailed,
	}
	for _, s := range checkable {
		if !ValidTransition(s, StateChecksRunning) {
			t.Errorf("expected %s -> CHECKS_RUNNING to be valid", s)
		}
	}
}
