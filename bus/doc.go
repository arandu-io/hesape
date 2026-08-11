// Package bus groups jobs: a batch that knows when all of it is done, and a
// chain that runs one job after another.
//
// It is the shape of every CSV import and every bulk export. Ten thousand rows
// are ten thousand jobs, because one job that loops over ten thousand rows is a
// job that dies on row nine thousand and starts again from one. Splitting them
// is easy; knowing when the last one finished is not, and that is what a batch
// is: a counter that survives the workers, plus the callbacks fired the moment
// it reaches zero.
//
// A batch is dispatched, tracked and finished:
//
//	b, err := bus.NewBatch("import-invoices").
//	    Add("invoice.import", row1).
//	    Add("invoice.import", row2).
//	    Then("import.done", report).
//	    Catch("import.failed", report).
//	    OnQueue("imports").
//	    Dispatch(ctx, g, repository, queue)
//
// The worker side is two calls, whatever the job:
//
//	m, err := bus.Batched(j.Payload, &row)
//	...
//	err = bus.Handled(ctx, g, repository, queue, m, doTheWork(ctx, g, row))
//
// Handled is Batch.RecordSuccessfulJob or Batch.RecordFailedJob plus
// Batchable.DispatchNextJobInChain or Batchable.InvokeChainCatchCallbacks, in
// one call: it moves the counter, fires Then, Catch, Failure, Progress and
// Finally exactly once each, and pushes the next link of a chain. A handler
// that never calls it leaves its batch pending forever, which is the one
// failure mode worth remembering.
//
// # The counters are Illuminate's
//
// PendingJobs falls by one for every job that *succeeded*; FailedJobs rises for
// every job that failed and leaves PendingJobs alone. So "every job succeeded"
// is PendingJobs reaching zero -- which is what finishes a batch and fires Then
// -- and "every job has reported" is PendingJobs minus FailedJobs reaching
// zero, which is UpdatedBatchJobCounts.AllJobsHaveRanExactlyOnce and what fires
// Finally.
//
// # No closures
//
// Then, Catch and Finally name a job; they do not take a function. A callback
// runs in a different process from the one that dispatched the batch -- often a
// different build -- and Go cannot serialize a closure across that gap. Laravel
// can, by serializing the closure's bytecode, and it is listed in
// docs/31-reorganizacao-hesape.md as impossible in Go rather than unwanted. A
// named job is the same thing with the indirection made visible.
//
// It is also why each moment names one job where Illuminate takes a list: a
// list of closures is cheap to register and a list of pushes is not, and one
// job that fans out is the same thing with a name somebody can grep.
//
// # No stored chain
//
// A chain needs no table. The remaining links travel inside the payload of the
// link currently running, so a chain is exactly as durable as the queue is and
// nothing has to be cleaned up when one is abandoned. A batch does need a table,
// because a counter shared by N workers cannot live in any one of their
// payloads.
//
// # The files it answers to
//
// The clone at laravel_illuminate/bus (Laravel 13, illuminate/bus ^13.0):
//
//	Batch.php                     -> Batch
//	BatchFactory.php              -> BatchFactory
//	BatchRepository.php           -> BatchRepository
//	Batchable.php                 -> Batchable, Batched
//	BusServiceProvider.php        -> nothing (ADR 0001, ADR 0002)
//	ChainedBatch.php              -> ChainedBatch, PrepareNestedBatches
//	DatabaseBatchRepository.php   -> DatabaseBatchRepository
//	Dispatcher.php                -> Dispatcher
//	DynamoBatchRepository.php     -> nothing, and never will
//	Events/                       -> bus/events
//	PendingBatch.php              -> PendingBatch
//	PrunableBatchRepository.php   -> PrunableBatchRepository
//	Queueable.php                 -> Queueable
//	UniqueLock.php                -> UniqueLock, PushUnique, ReleaseUnique
//	UpdatedBatchJobCounts.php     -> UpdatedBatchJobCounts
//
// DynamoBatchRepository has no equivalent and will not get one: a second store
// with a different consistency model is a second set of rules for when a
// callback fires, and DynamoDB is not a driver this collection carries.
// BusServiceProvider has none either -- there is no container to register into
// (ADR 0001).
//
// Chain has no file of its own in illuminate/bus: Laravel's pending chain lives
// in Foundation\Bus\PendingChain, which is the skeleton and not the library.
// It is here because a chain is half of what this package is about.
package bus
