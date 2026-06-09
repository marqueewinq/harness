package internal

type TaskState string

const (
	StateNew             TaskState = "NEW"
	StateInProgress      TaskState = "IN_PROGRESS"
	StateChecksRunning   TaskState = "CHECKS_RUNNING"
	StateChecksPassed    TaskState = "CHECKS_PASSED"
	StateChecksFailed    TaskState = "CHECKS_FAILED"
	StateReviewRequested TaskState = "REVIEW_REQUESTED"
	StateApproved        TaskState = "APPROVED"
	StateRequestChanges  TaskState = "REQUEST_CHANGES"
	StateQueued          TaskState = "QUEUED"
	StateMerging         TaskState = "MERGING"
	StateMerged          TaskState = "MERGED"
	StateMergeFailed     TaskState = "MERGE_FAILED"
	StateClosed          TaskState = "CLOSED"
)

var validTransitions = map[TaskState][]TaskState{
	StateNew:             {StateInProgress},
	StateInProgress:      {StateChecksRunning, StateClosed},
	StateChecksRunning:   {StateChecksPassed, StateChecksFailed},
	StateChecksPassed:    {StateReviewRequested, StateClosed},
	StateChecksFailed:    {StateChecksRunning, StateClosed},
	StateReviewRequested: {StateApproved, StateRequestChanges, StateChecksRunning, StateClosed},
	StateApproved:        {StateQueued, StateClosed},
	StateRequestChanges:  {StateChecksRunning, StateClosed},
	StateQueued:          {StateMerging, StateClosed},
	StateMerging:         {StateMerged, StateMergeFailed},
	StateMergeFailed:     {StateChecksRunning, StateClosed},
	StateClosed:          {},
}

func ValidTransition(from, to TaskState) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}
