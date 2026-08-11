package events

import "time"

// CallQueuedListener is the job that runs a listener on the queue.
//
// The dispatcher builds one whenever a listener implements ShouldQueue, and the
// worker calls Handle. Everything the listener declared about how it wants to
// be run -- the connection, the queue, the tries, the delay -- is copied onto
// the job before it is pushed, because the worker has the job and not the
// listener.
type CallQueuedListener struct {
	// Class is the listener. The PHP keeps its class name and resolves it out
	// of the container; there is no container here (ADR 0001), so the value
	// travels.
	Class any
	// Method is the listener method to call.
	Method string
	// Data is what the listener is called with.
	Data []any

	// Connection, Queue, Delay, MessageGroup and AfterCommit come from the
	// Queueable trait in the PHP, which CallQueuedListener uses.
	Connection   string
	Queue        string
	Delay        time.Duration
	MessageGroup string
	AfterCommit  bool

	// Tries is how many times the job may be attempted.
	Tries int
	// MaxExceptions is the ceiling on exceptions, whatever the attempts.
	MaxExceptions int
	// Backoff is how long to wait before retrying after an uncaught failure.
	Backoff time.Duration
	// RetryUntil is when the job stops being retried.
	RetryUntil time.Time
	// Timeout is how long the job may run.
	Timeout time.Duration
	// FailOnTimeout says the job fails rather than retries when it times out.
	FailOnTimeout bool
	// ShouldBeEncrypted says the payload is encrypted on the way to the queue.
	ShouldBeEncrypted bool
	// DeleteWhenMissingModels deletes the job when its records are gone.
	DeleteWhenMissingModels bool

	// The four below are public properties in the PHP and unexported here: Go
	// cannot have a field and a method of the same name, and the methods are the
	// names ADR 0044 keeps. The dispatcher fills them in this package.
	shouldBeUnique                bool
	shouldBeUniqueUntilProcessing bool
	uniqueID                      any
	uniqueFor                     time.Duration

	deduplicator func() string
}

// NewCallQueuedListener creates a new job instance.
func NewCallQueuedListener(class any, method string, data []any) *CallQueuedListener {
	return &CallQueuedListener{Class: class, Method: method, Data: data}
}

// Handle handles the queued job: it calls the listener's method with the data.
//
// The PHP takes the container, because all it has is the listener's class name.
// This has the listener, so it takes nothing.
func (j *CallQueuedListener) Handle() any {
	return callMethod(j.Class, j.Method, j.Data)
}

// ShouldBeUnique reports whether the listener should be unique.
func (j *CallQueuedListener) ShouldBeUnique() bool { return j.shouldBeUnique }

// ShouldBeUniqueUntilProcessing reports whether the listener should be unique
// only until processing begins.
func (j *CallQueuedListener) ShouldBeUniqueUntilProcessing() bool {
	return j.shouldBeUniqueUntilProcessing
}

// UniqueID returns the unique ID for the listener.
//
// The PHP spells it uniqueId; Go spells an initialism in capitals, which is the
// one change ADR 0044 asks to be said out loud.
func (j *CallQueuedListener) UniqueID() any { return j.uniqueID }

// UniqueFor returns how long the unique lock is held.
//
// The PHP returns seconds as an int, which is the same thing a duration says
// without the unit having to be remembered.
func (j *CallQueuedListener) UniqueFor() time.Duration { return j.uniqueFor }

// UniqueVia returns the cache store the unique lock is kept in, or nil when the
// listener does not name one.
//
// The PHP types the return as Illuminate\Contracts\Cache\Repository. There is
// no contracts package in this collection and this one must not depend on the
// cache, so the return is whatever the listener's own UniqueVia returned: the
// worker that acquires the lock is the only thing that reads it, and it knows
// the concrete store.
func (j *CallQueuedListener) UniqueVia() any {
	via, ok := j.Class.(interface{ UniqueVia() any })
	if !ok {
		return nil
	}
	return via.UniqueVia()
}

// WithDeduplicator sets the callback that generates the deduplication ID.
func (j *CallQueuedListener) WithDeduplicator(deduplicator func() string) *CallQueuedListener {
	j.deduplicator = deduplicator
	return j
}

// Deduplicator returns the deduplication ID, or the empty string when the job
// has no deduplicator.
func (j *CallQueuedListener) Deduplicator() string {
	if j.deduplicator == nil {
		return ""
	}
	return j.deduplicator()
}

// Failed calls the failed method on the listener, with the event and the
// failure, when the listener has one.
func (j *CallQueuedListener) Failed(err error) {
	if !hasMethod(j.Class, "Failed") {
		return
	}
	callMethod(j.Class, "Failed", append(append([]any(nil), j.Data...), err))
}

// DisplayName returns the display name for the queued job, which is the
// listener's type.
func (j *CallQueuedListener) DisplayName() string { return typeName(j.Class) }
