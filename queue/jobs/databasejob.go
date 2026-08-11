package jobs

import "time"

// DatabaseJob is a job that came off a DatabaseQueue.
//
// It answers Illuminate\Queue\Jobs\DatabaseJob, and it is an alias rather than
// a type: there is one [Job], and which store it came off is the [Driver] it
// carries, not its class. In Laravel the three subclasses -- DatabaseJob,
// RedisJob, SyncJob -- differ only in where release and delete write, and a
// type per store would be a second way to say what the Driver already says
// (RULE 9).
//
// The name is here because it is the name a Laravel developer looks for, and
// finding it say so is better than finding nothing.
type DatabaseJob = Job

// SyncJob is a job that ran the moment it was pushed.
//
// It answers Illuminate\Queue\Jobs\SyncJob, and it is an alias for the same
// reason [DatabaseJob] is.
type SyncJob = Job

// DatabaseJobRecord is the jobs table row while the driver is holding it.
//
// It answers Illuminate\Queue\Jobs\DatabaseJobRecord: the bookkeeping the
// database driver does to a row between reading it and handing the job over --
// counting the delivery and stamping the reservation.
//
// It is a separate type from [Job] because it holds what only the table has.
// ReservedUntil is not on the job: a handler has no use for it, and a field a
// handler can read is a field a handler will one day write.
type DatabaseJobRecord struct {
	// Job is the record as the worker will see it.
	*Job

	// ReservedUntil is when this row becomes visible to other workers again.
	// Zero means it is not reserved.
	ReservedUntil time.Time
}

// GetJobRecord is the table row behind a job.
//
// It answers DatabaseJob::getJobRecord(). In Laravel the record is a separate
// object the job holds; here the job is the record, so this wraps it in the
// type that carries the reservation and returns that.
func (j *Job) GetJobRecord() *DatabaseJobRecord { return &DatabaseJobRecord{Job: j} }

// Increment counts this delivery and returns the new count.
//
// It answers increment(). Attempts includes the current delivery, so the first
// call returns 1 and the worker comparing it against MaxAttempts is comparing
// deliveries to deliveries.
func (r *DatabaseJobRecord) Increment() int {
	r.Attempts++
	return r.Attempts
}

// Touch stamps the reservation forward by lease and returns the new deadline.
//
// It answers touch(), which in Laravel refreshes reserved_at so a job that is
// still running is not handed to a second worker. A non-positive lease leaves
// the deadline alone rather than expiring the reservation, because a caller
// that asked for no lease meant "do not change it" and unreserving a running
// job is the one outcome nobody wants.
func (r *DatabaseJobRecord) Touch(lease time.Duration) time.Time {
	if lease > 0 {
		r.ReservedUntil = time.Now().UTC().Add(lease)
	}
	return r.ReservedUntil
}
