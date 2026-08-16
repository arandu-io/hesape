// Package queue is work that happens after the response: the [Queue] contract,
// the drivers that ship with the collection, and the [Worker] that drains them.
//
// The surface is Illuminate\Queue's -- Push, PushOn, Later, Bulk, Pop, Size,
// Clear -- and the job itself is [github.com/arandu-io/hesape/queue/jobs].
//
// The clone this was written against is laravel_illuminate/queue.
//
// The contract lives in the collection and so do the drivers that need nothing
// installed, for the same reason as database.Repository: Push takes an
// auth.Grant, and the tenant comes from it. Moving that into an optional
// package would make the guarantee optional, and an optional guarantee is not
// one.
//
//	DatabaseQueue    the application's own database (the default)
//	SyncQueue        runs the job at Push, for tests and for a laptop
//	DeferredQueue    runs the job after the response, in this process
//	BackgroundQueue  runs the job in another process
//	FailoverQueue    writes to the first connection that accepts
//	NullQueue        accepts everything and keeps nothing
//	RedisQueue       github.com/arandu-io/hesape/queue/connectors/redis
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
// the fallback one. The relay that drains it is the [Worker].
//
// The name does not change because of it. This is DatabaseQueue because that is
// what Laravel calls the queue that lives in the application's database (ADR
// 0044); naming it Outbox would name the mechanism instead of the thing, and
// hide it from everyone who came looking for the driver. The mechanism is
// what is underneath; the surface is the one a Laravel developer types.
//
// # At-least-once
//
// A handler that cannot run twice safely is a handler with a bug. The process
// can die between doing the work and deleting the job, and no queue anywhere
// solves that.
//
// # Where the rest of it lives
//
//	queue/attributes  the per-job settings: tries, backoff, timeout
//	queue/connectors  opening a connection lazily
//	queue/console     queue:work, queue:retry, queue:pause and the rest
//	queue/events      what the queue announces about itself
//	queue/failed      a dead letter list that outlives the queue
//	queue/jobs        the job itself
//	queue/middleware  what wraps the handling of one job
//
// # What it answers to in Laravel
//
// The files, in the clone at laravel_illuminate/queue:
//
//	BackgroundQueue.php       -> BackgroundQueue
//	BeanstalkdQueue.php       -- not coming, RULE 11
//	CallQueuedClosure.php     -> CallQueuedClosure
//	CallQueuedHandler.php     -> CallQueuedHandler
//	DatabaseQueue.php         -> DatabaseQueue
//	DeferredQueue.php         -> DeferredQueue
//	FailoverQueue.php         -> FailoverQueue
//	InteractsWithQueue.php    -> InteractsWithQueue
//	InvalidPayloadException.php    -> ErrInvalidPayload
//	Listener.php              -> Listener
//	ListenerOptions.php       -> ListenerOptions
//	LuaScripts.php            -- see below
//	ManuallyFailedException.php    -> ErrManuallyFailed
//	MaxAttemptsExceededException.php -> MaxAttemptsExceeded
//	NullQueue.php             -> NullQueue
//	Queue.php                 -> the connection struct, CreatePayload, LaterOn
//	QueueManager.php          -> QueueManager
//	QueueRoutes.php           -> QueueRoutes
//	QueueServiceProvider.php  -- not coming, ADR 0002
//	RedisQueue.php            -> connectors/redis
//	SerializesModels.php      -> SerializesModels
//	SerializesAndRestoresModelIdentifiers.php -> SerializesModels
//	SqsQueue.php              -- not coming, RULE 11
//	SyncQueue.php             -> SyncQueue
//	TimeoutExceededException.php -> TimeoutExceeded
//	Worker.php                -> Worker
//	WorkerOptions.php         -> WorkerOptions
//	WorkerStopReason.php      -> WorkerStopReason
//
// Beanstalkd and SQS are not coming: RULE 11 names the stores this collection
// speaks to, and neither is one of them. QueueServiceProvider is the container
// wiring ADR 0002 refused: the application constructs its queues in
// bootstrap/app.go.
//
// # Method by method, so the list can be checked rather than believed
//
// Twenty-six public methods of the component have no name here. Each one, with
// the ADR 0056 reason number:
//
//	BeanstalkdQueue::deleteMessage, ::getPheanstalk, Jobs\BeanstalkdJob::bury,
//	    ::getPheanstalk, ::getPheanstalkJob, SqsQueue::getSqs, Jobs\SqsJob::getSqs
//	    and ::getSqsJob -- reason 3, all eight. Beanstalkd and SQS are two stores
//	    this collection does not speak to (RULE 11), and these are the driver
//	    handles and the one operation -- bury -- that only Beanstalkd has. A job
//	    that stops being delivered here is queue/failed, which every driver has.
//	RedisQueue::getRedis, ::migrateExpiredJobs, Jobs\RedisJob::getRedisQueue and
//	    ::getReservedJob -- reason 3: the RESP driver is queue/connectors/redis,
//	    with the driver it needs. migrateExpiredJobs moves the reserved and
//	    delayed sets back onto the ready list, which is LuaScripts, and the
//	    paragraph below says why that is a gap rather than a decision.
//	QueueServiceProvider::register, ::registerConnectors, ::provides,
//	    QueueManager::getApplication, ::setApplication, Queue::getConfig,
//	    ::setConfig, ::getContainer, ::setContainer and Jobs\Job::getContainer --
//	    reason 2, all ten. They bind the manager, the connectors and the failed
//	    job provider into the container, and hand the container and the raw
//	    config array back so that a driver closure can resolve out of them. An
//	    application constructs its queues in bootstrap/app.go and passes them
//	    (ADR 0002); the configuration is a typed struct read at boot, not an
//	    array a driver reaches into at push time.
//	Capsule\Manager::addConnection, ::getQueueManager and ::registerConnectors --
//	    reason 2: the capsule is Illuminate's way of using the queue outside a
//	    Laravel application, which is a container built on the caller's behalf.
//	    Outside an Arandu application the queue is the same [QueueManager] built
//	    the same way, so there is nothing for a capsule to do (RULE 9).
//	Jobs\Job::fire and ::getResolvedJob -- reason 2: fire() parses the class and
//	    method out of the payload, resolves the class from the container and
//	    calls it, and getResolvedJob hands back what it resolved. Here the name
//	    routes to a handler registered by [Worker], which is the same lookup
//	    written once and checkable at boot.
//	Jobs\Job::uuid -- reason 1 for the shape, not for the name: PHP reads it
//	    through an accessor because the property is protected. Here it is the
//	    exported field jobs.Job.UUID, which is the same value getJobId() answers
//	    and the reason the two are one field.
//	Queue::fake -- reason 2: it is Facade::swap putting a QueueFake where the
//	    'queue' binding was, so that everything the application pushes is
//	    recorded and nothing is worked. There is no container (ADR 0001) and no
//	    facade (ADR 0002), so there is no binding to swap, and a package-level
//	    queue a test could swap would be shared mutable state that two tests
//	    calling t.Parallel would fight over (ADR 0045). What a test writes
//	    instead is the sync connection over a registry whose handler records
//	    and does nothing else:
//
//	        w := queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
//
//	        var worked []string
//	        w.HandleFunc("invoice.email", func(_ context.Context, _ auth.Grant, j *jobs.Job) error {
//	            worked = append(worked, j.Name)
//	            return nil
//	        })
//
//	        emailInvoice(ctx, g, queue.NewSyncQueue(w))
//
//	        if len(worked) != 1 {
//	            t.Fatalf("worked %d jobs, want 1", len(worked))
//	        }
//
//	    The payload is encoded on the way in and decoded on the way out, which
//	    is the round trip QueueFake only makes when serializeAndRestore was
//	    asked for -- so a payload that does not survive the queue is found by
//	    the test that pushed it rather than in production. [NullQueue] is the
//	    variant for a test that only needs the push to be accepted, and the
//	    registry is a local value: two tests hold two of them.
//	CallQueuedClosure::create -- it is here, as [NewCallQueuedClosure]. PHP has
//	    both a constructor taking an already-wrapped SerializableClosure and a
//	    create() taking the raw Closure; Go has no closure serialization and
//	    therefore only the one form (RULE 9).
//
// LuaScripts.php has no answer here and it is not a decision, it is a gap. The
// seven scripts it holds are RedisQueue's storage layout expressed in Lua, and
// the RESP driver in connectors/redis keeps its jobs in a different shape --
// writing the scripts now would be seven functions with the right names and the
// wrong behaviour, which is worse than seven that are missing.
package queue
