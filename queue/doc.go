// Package queue is work that happens after the response: the [Queue] contract,
// the drivers that ship with the collection, and the [Worker] that drains them.
//
// The surface is Illuminate\Queue's -- Push, PushOn, Later, Bulk, Pop, Size,
// Clear -- and the job itself is [github.com/arandu-io/hesape/queue/jobs].
//
// The contract lives in the collection and so do three of the drivers, for the
// same reason as database.Repository: Push takes an auth.Grant, and the tenant
// comes from it. Moving that into an optional package would make the guarantee
// optional, and an optional guarantee is not one.
//
//	DatabaseQueue  the application's own database (the default)
//	SyncQueue      runs the job at Push, for tests and for a laptop
//	NullQueue      accepts everything and keeps nothing
//	RedisQueue     github.com/arandu-io/hesape/queue/connectors/redis
//
// RedisQueue is a separate module because in Go there is no optional dependency
// and a collection that carried a Redis client would put it in every project's
// go.sum (ADR 0048).
//
// # The outbox is the mechanism, not the name
//
// A job pushed inside database.Transaction is committed by the same transaction
// as the row it is about: it exists if and only if the write did. That is the
// outbox guarantee -- the one the events package uses for events -- applied to
// work, and it is the reason [DatabaseQueue] is the default driver rather than
// the fallback one.
//
// The name does not change because of it. This is DatabaseQueue because that is
// what Laravel calls the queue that lives in the application's database (ADR
// 0044); naming it Outbox would name the mechanism instead of the thing, and
// hide it from everyone who came looking for the driver. The mechanism is
// documented on the type, where somebody choosing a driver will read it.
//
// # At-least-once
//
// A handler that cannot run twice safely is a handler with a bug. The process
// can die between doing the work and deleting the job, and no queue anywhere
// solves that.
//
// # What it answers to in Laravel
//
// The files, in the clone at laravel_illuminate/queue:
//
//	DatabaseQueue.php    -> DatabaseQueue
//	NullQueue.php        -> NullQueue
//	Queue.php            -> Queue
//	QueueManager.php     -> Manager
//	RedisQueue.php       -> connectors/redis
//	SyncQueue.php        -> SyncQueue
//	Worker.php           -> Worker
//	WorkerOptions.php    -> WorkerOptions
//	CallQueuedHandler.php -> Handler, and the Worker's registry
//
// Still to arrive, with the phase each is named in by
// docs/31-reorganizacao-hesape.md:
//
//	BackgroundQueue.php
//	BeanstalkdQueue.php
//	CallQueuedClosure.php
//	DeferredQueue.php
//	FailoverQueue.php
//	InteractsWithQueue.php
//	InvalidPayloadException.php
//	Listener.php
//	ListenerOptions.php
//	LuaScripts.php
//	ManuallyFailedException.php
//	MaxAttemptsExceededException.php
//	QueueRoutes.php
//	QueueServiceProvider.php
//	SerializesAndRestoresModelIdentifiers.php
//	SerializesModels.php
//	SqsQueue.php
//	TimeoutExceededException.php
//	WorkerStopReason.php
//
// Beanstalkd and SQS are not on that list because they are not coming: RULE 11
// names the stores this collection speaks to, and neither is one of them. The
// two SerializesModels traits answer a problem Go does not have -- a job
// carries JSON, not a rehydrated object graph.
package queue
