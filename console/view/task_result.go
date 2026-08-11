package view

// TaskResult is how a task ended.
//
// It answers Illuminate\Console\View\TaskResult, values included, because the
// Task component switches on them and a task callback returns one.
type TaskResult int

const (
	// TaskSuccess prints DONE.
	TaskSuccess TaskResult = 1
	// TaskFailure prints FAIL.
	TaskFailure TaskResult = 2
	// TaskSkipped prints SKIPPED.
	TaskSkipped TaskResult = 3
)
