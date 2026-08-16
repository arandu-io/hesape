package view

// TaskResult is how a task ended.
//
// The Task component switches on it, and a task callback returns one.
type TaskResult int

const (
	// TaskSuccess prints DONE.
	TaskSuccess TaskResult = 1
	// TaskFailure prints FAIL.
	TaskFailure TaskResult = 2
	// TaskSkipped prints SKIPPED.
	TaskSkipped TaskResult = 3
)
