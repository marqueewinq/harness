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
	StatePlanned         TaskState = "PLANNED"
)

type EpicStatus string

const (
	EpicStatusOpen   EpicStatus = "open"
	EpicStatusClosed EpicStatus = "closed"
)

type ProjectStatus string

const (
	ProjectStatusOpen   ProjectStatus = "open"
	ProjectStatusClosed ProjectStatus = "closed"
)

var validTransitions = map[TaskState][]TaskState{
	StatePlanned:         {StateInProgress, StateClosed},
	StateNew:             {StateInProgress},
	StateInProgress:      {StateChecksRunning, StateClosed},
	StateChecksRunning:   {StateChecksRunning, StateChecksPassed, StateChecksFailed},
	StateChecksPassed:    {StateReviewRequested, StateChecksRunning, StateClosed},
	StateChecksFailed:    {StateChecksRunning, StateClosed},
	StateReviewRequested: {StateApproved, StateRequestChanges, StateChecksRunning, StateClosed},
	StateApproved:        {StateQueued, StateClosed, StateInProgress},
	StateRequestChanges:  {StateChecksRunning, StateClosed},
	StateQueued:          {StateMerging, StateMergeFailed, StateClosed},
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
