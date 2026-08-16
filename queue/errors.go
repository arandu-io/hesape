package queue

import (
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/queue/jobs"
)

// MaxAttemptsExceeded is why a job was parked: it ran out of deliveries.
//
// It keeps the job, so a listener on the failed-job event can say which one and
// on which queue without decoding anything.
type MaxAttemptsExceeded struct {
	// Job is the job that ran out. It is never nil on an error built by
	// [MaxAttemptsExceeded.ForJob].
	Job *jobs.Job
	// Attempts is how many deliveries it had, and Max is how many it was
	// allowed. Both are on the error because "attempted too many times" without
	// the two numbers is the least useful sentence a log can carry.
	Attempts int
	Max      int
}

// ForJob returns the error for a job that ran out of deliveries.
//
// It is a method on the zero value rather than a package function, so
// `queue.MaxAttemptsExceeded{}.ForJob(j, 5)` names the error and the reason for
// it in one phrase.
func (MaxAttemptsExceeded) ForJob(j *jobs.Job, max int) *MaxAttemptsExceeded {
	e := &MaxAttemptsExceeded{Job: j, Max: max}
	if j != nil {
		e.Attempts = j.Attempts
	}
	return e
}

// Error is the message.
func (e *MaxAttemptsExceeded) Error() string {
	name := "the job"
	if e.Job != nil {
		name = e.Job.ResolveName()
	}
	return fmt.Sprintf("queue: %s has been attempted too many times: %d of %d allowed",
		name, e.Attempts, e.Max)
}

// TimeoutExceeded is why a job was parked: the handler ran past its timeout.
//
// It wraps [ErrMaxAttemptsExceeded], so errors.Is against that still matches: a
// timed-out job is one that used up a delivery without finishing, and code that
// treats the two the same can.
type TimeoutExceeded struct {
	// Job is the job that ran too long.
	Job *jobs.Job
}

// ForJob returns the error for a job whose handler ran too long.
func (TimeoutExceeded) ForJob(j *jobs.Job) *TimeoutExceeded { return &TimeoutExceeded{Job: j} }

// Error is the message.
func (e *TimeoutExceeded) Error() string {
	name := "the job"
	if e.Job != nil {
		name = e.Job.ResolveName()
	}
	return fmt.Sprintf("queue: %s has timed out", name)
}

// Unwrap makes a timeout match [ErrMaxAttemptsExceeded].
func (e *TimeoutExceeded) Unwrap() error { return ErrMaxAttemptsExceeded }

// ErrMaxAttemptsExceeded is what [MaxAttemptsExceeded] and [TimeoutExceeded]
// both match under errors.Is, for a caller that only wants to know the job gave
// up rather than why.
var ErrMaxAttemptsExceeded = errors.New("queue: the job has been attempted too many times")

// Is makes errors.Is(err, ErrMaxAttemptsExceeded) true for this error.
func (e *MaxAttemptsExceeded) Is(target error) bool { return target == ErrMaxAttemptsExceeded }

// ErrManuallyFailed is what a handler returns to park its own job.
//
// A handler that knows the work can never succeed -- the customer is gone, the
// file is malformed -- wraps it, and the worker parks the job on the first
// delivery instead of retrying four more times on the way to the same place.
//
//	return fmt.Errorf("%w: the invoice was voided", queue.ErrManuallyFailed)
var ErrManuallyFailed = errors.New("queue: the job failed and will not be retried")

// ErrInvalidPayload is returned when a job's arguments cannot be encoded.
//
// The value that failed to encode is not carried on it: the wrapped json error
// already names the type and the field.
var ErrInvalidPayload = errors.New("queue: the job payload cannot be encoded")
