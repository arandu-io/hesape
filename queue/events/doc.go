// Package events is everything the queue announces about itself.
//
// It mirrors Illuminate\Queue\Events, one struct per PHP class, with the fields
// Laravel puts on it under the names Laravel gives them. A listener written
// against [JobFailed] reads e.ConnectionName, e.Job and e.Exception exactly as
// the PHP one reads $event->connectionName.
//
// They are values, and nothing here dispatches: queue.Dispatcher is the two
// methods the worker needs, and what is on the other side of it is the
// application's business. A worker with no dispatcher builds no events at all.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Events:
//
//	JobAttempted.php              -> JobAttempted
//	JobExceptionOccurred.php      -> JobExceptionOccurred
//	JobFailed.php                 -> JobFailed
//	JobPopped.php                 -> JobPopped
//	JobPopping.php                -> JobPopping
//	JobProcessed.php              -> JobProcessed
//	JobProcessing.php             -> JobProcessing
//	JobQueued.php                 -> JobQueued
//	JobQueueing.php               -> JobQueueing
//	JobReleasedAfterException.php -> JobReleasedAfterException
//	JobRetryRequested.php         -> JobRetryRequested
//	JobTimedOut.php               -> JobTimedOut
//	Looping.php                   -> Looping
//	QueueBusy.php                 -> QueueBusy
//	QueueFailedOver.php           -> QueueFailedOver
//	QueuePaused.php               -> QueuePaused
//	QueueResumed.php              -> QueueResumed
//	WorkerStarting.php            -> WorkerStarting
//	WorkerStopping.php            -> WorkerStopping
//
// [WorkerStopReason] is here rather than in the queue package, where its PHP
// original lives, because [WorkerStopping] carries it and the queue package is
// what dispatches these -- the queue package re-exports it under the name a
// Laravel developer types.
package events
