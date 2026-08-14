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
// # What is not ported, and why
//
// With the numbered reason from ADR 0044: (1) a PHP language feature Go does not
// have, (2) a method that only serves the container, a facade or a service
// provider, (3) a driver this ecosystem does not carry.
//
// DynamoBatchRepository -- reason 3, the whole class. A second store with a
// different consistency model is a second set of rules for when a callback
// fires, and DynamoDB is not a driver this collection carries. What is written
// here instead is [DatabaseBatchRepository], over the SQL table the migration
// creates, with [Memory] for a test:
//
//   - DynamoBatchRepository::getDynamoClient hands out the AWS SDK client the
//     repository was built with, so that a caller can reach past the repository
//     to the store. The store here is the application's own database and
//     [NewDatabaseBatchRepository] is handed it, so a caller that wants it
//     already has it.
//   - DynamoBatchRepository::getTable is the table the batches live in. It is
//     job_batches, created by [Migrations] and named nowhere else, because a
//     table the framework reads and writes is not one an application renames.
//   - DynamoBatchRepository::createAwsDynamoTable and
//     DynamoBatchRepository::deleteAwsDynamoTable are the two DDL calls that
//     make and drop that table. Schema here is [Migrations], run by `aru
//     migrate` as a pipeline step and never by the process that uses the table
//     (RULE 16).
//
// BusServiceProvider -- reason 2, the whole class. ADR 0001 removed the
// container and ADR 0002 the facade:
//
//   - BusServiceProvider::register binds Dispatcher as a singleton and aliases
//     the two dispatcher contracts onto it, then picks the batch repository off
//     a configuration string. [NewDispatcher] takes the queue and the repository
//     as arguments, so which store batches land in is a line the application
//     wrote rather than a string it configured.
//   - BusServiceProvider::provides lists the five bindings that registration is
//     deferred for. Nothing is deferred when nothing is resolved by name.
//
// Batch::jsonSerialize -- reason 1. It is PHP's JsonSerializable, a language
// interface that tells json_encode what an object looks like. Go's counterpart
// is json.Marshaler, and [Batch] is a struct with json tags, so encoding/json
// reads it directly and there is no hook to implement.
//
// Chain has no file of its own in illuminate/bus: Laravel's pending chain lives
// in Foundation\Bus\PendingChain, which is the skeleton and not the library.
// It is here because a chain is half of what this package is about, and
// Bus::dispatchChain -- the facade method that builds one and dispatches it in
// the same expression -- is [Dispatcher.DispatchChain].
//
// # Bus::fake, and what a test writes instead
//
// Bus::fake is reason 2 of ADR 0044 -- a method that only serves the container,
// a facade or a service provider. It is Facade::swap putting a BusFake where
// the dispatcher contract was bound, so that everything dispatched anywhere in
// the application is recorded and nothing runs. There is neither container
// (ADR 0001) nor facade (ADR 0002), so there is no binding to swap, and a
// package-level dispatcher a test could swap would be shared mutable state
// that two tests calling t.Parallel would fight over (ADR 0045).
//
// A test builds the dispatcher it wants, in one line, and the handler it maps
// is the recording:
//
//	var ran []string
//	d := bus.NewDispatcher(nil, bus.NewMemory()).Map(map[string]bus.Handler{
//		"invoice.email": func(context.Context, auth.Grant, []byte) error {
//			ran = append(ran, "invoice.email")
//			return nil
//		},
//	})
//
//	importInvoices(ctx, g, d)
//
//	if len(ran) != 1 {
//		t.Fatalf("ran %d jobs, want 1", len(ran))
//	}
//
// A nil queue runs every job in this process, which is the half of Bus::fake a
// test usually wants: assert what the work did, not that it was scheduled. The
// other half -- assert it was queued and did not run -- is a [Queue] whose one
// Push method appends to a slice, handed to [NewDispatcher] in place of the
// nil. That interface has exactly one method for this reason, and [Memory] is
// the batch repository that needs no table, so a batch is exercised end to end
// with nothing installed and nothing shared between tests.
package bus
