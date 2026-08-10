// Package queue is the contract for work that happens after the response:
// Job, Handler, Queue and the Worker that drains them.
//
// The contract lives in the collection and the drivers do not, for the same
// reason as database.Repository: Push takes an auth.Grant, and the tenant comes
// from it. Moving that into an optional package would make the guarantee
// optional, and an optional guarantee is not one.
//
// A driver is a separate module under github.com/arandu-io/queue, because in Go
// there is no optional dependency and a collection that carried a Redis client
// would put it in every project's go.sum.
//
// Delivery is at-least-once. A handler that cannot run twice safely is a handler
// with a bug -- the process can die between doing the work and acknowledging it,
// and no queue anywhere solves that.
//
// It mirrors Illuminate\Queue. The files it answers to, in the clone at
// laravel_illuminate/queue:
//
//	BackgroundQueue.php
//	BeanstalkdQueue.php
//	CallQueuedClosure.php
//	CallQueuedHandler.php
//	DatabaseQueue.php
//	DeferredQueue.php
//	FailoverQueue.php
//	InteractsWithQueue.php
//	InvalidPayloadException.php
//	Listener.php
//	ListenerOptions.php
//	LuaScripts.php
//	ManuallyFailedException.php
//	MaxAttemptsExceededException.php
//	NullQueue.php
//	Queue.php
//	QueueManager.php
//	QueueRoutes.php
//	QueueServiceProvider.php
//	RedisQueue.php
//	SerializesAndRestoresModelIdentifiers.php
//	SerializesModels.php
//	SqsQueue.php
//	SyncQueue.php
//	TimeoutExceededException.php
//	Worker.php
//	WorkerOptions.php
//	WorkerStopReason.php
//
// Of those, Queue.php, Worker.php and WorkerOptions.php have an answer here.
// The rest -- the drivers, the payload serialization, the manager -- are named
// in docs/31-reorganizacao-hesape.md with the phase they arrive in.
package queue
