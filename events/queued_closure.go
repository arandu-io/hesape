package events

import "time"

// QueuedClosure is a closure listener that runs on the queue.
//
//	d.Listen("invoice.paid", events.Queueable(func(e Stored) {
//		// ...
//	}).OnQueue("billing").Delay(10*time.Second))
//
// Every setter returns the receiver, which is the PHP's `return $this`.
type QueuedClosure struct {
	// Closure is the underlying function. It is public because the dispatcher
	// reads it to work out which event the closure listens for, which is what
	// the PHP does with $events->closure.
	Closure any

	// The rest are public properties in the PHP and unexported here, because Go
	// cannot have both a field and a method of the same name and the methods
	// are the names ADR 0044 keeps.
	connection   string
	queue        string
	messageGroup string
	deduplicator func() string
	delay        time.Duration

	catchCallbacks []any
}

// Queueable creates a new queued closure event listener.
//
// It answers the queueable() function in the PHP's functions.php, which is the
// only thing that file holds.
func Queueable(closure any) *QueuedClosure {
	return &QueuedClosure{Closure: closure}
}

// OnConnection sets the desired connection for the job.
func (q *QueuedClosure) OnConnection(connection string) *QueuedClosure {
	q.connection = connection
	return q
}

// OnQueue sets the desired queue for the job.
func (q *QueuedClosure) OnQueue(queue string) *QueuedClosure {
	q.queue = queue
	return q
}

// OnGroup sets the desired job "group".
//
// Only some queues support it, which is what the PHP says as well.
func (q *QueuedClosure) OnGroup(group string) *QueuedClosure {
	q.messageGroup = group
	return q
}

// WithDeduplicator sets the callback that generates the deduplication ID.
//
// The PHP wraps a Closure in a SerializableClosure so it survives the trip
// through the queue. Nothing here is serialized -- a Go job is a value the
// worker already has the type of -- so the callback is kept as it was given.
func (q *QueuedClosure) WithDeduplicator(deduplicator func() string) *QueuedClosure {
	q.deduplicator = deduplicator
	return q
}

// Delay sets the delay before the job becomes available.
//
// The PHP takes DateTimeInterface|DateInterval|int; a Go duration is the one of
// the three that means the same thing everywhere.
func (q *QueuedClosure) Delay(delay time.Duration) *QueuedClosure {
	q.delay = delay
	return q
}

// Catch registers a callback invoked if the queued listener job fails.
func (q *QueuedClosure) Catch(closure any) *QueuedClosure {
	q.catchCallbacks = append(q.catchCallbacks, closure)
	return q
}

// Resolve returns the listener that puts the closure on the queue.
//
// The PHP calls the global dispatch() helper here. There are no global helpers
// in this collection (ADR 0002), so the queue comes from the dispatcher the
// listener was registered on, and Resolve is handed it. That is the whole
// difference.
func (q *QueuedClosure) Resolve(d *Dispatcher) Listener {
	return func(_ string, payload []any) any {
		job := NewCallQueuedListener(InvokeQueuedClosure{}, "Handle", []any{
			q.Closure,
			payload,
			q.catchCallbacks,
		})
		job.Connection = q.connection
		job.Queue = q.queue
		job.Delay = q.delay
		job.MessageGroup = q.messageGroup
		job.WithDeduplicator(q.deduplicator)

		return d.push(job)
	}
}
