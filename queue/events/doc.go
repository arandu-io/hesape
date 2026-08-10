// Package events mirrors Illuminate\Queue\Events.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Events:
//
//	JobAttempted.php
//	JobExceptionOccurred.php
//	JobFailed.php
//	JobPopped.php
//	JobPopping.php
//	JobProcessed.php
//	JobProcessing.php
//	JobQueued.php
//	JobQueueing.php
//	JobReleasedAfterException.php
//	JobRetryRequested.php
//	JobTimedOut.php
//	Looping.php
//	QueueBusy.php
//	QueueFailedOver.php
//	QueuePaused.php
//	QueueResumed.php
//	WorkerStarting.php
//	WorkerStopping.php
//
// Nothing is implemented here yet. docs/31-reorganizacao-hesape.md says what
// moves in, from where, and in which phase.
package events
