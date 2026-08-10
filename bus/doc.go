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
//	    Dispatch(ctx, g, store, queue)
//
// The worker side is two calls, whatever the job:
//
//	m, err := bus.Batching(j.Payload, &row)
//	...
//	err = bus.Handled(ctx, g, store, queue, m, doTheWork(ctx, g, row))
//
// Handled is what decrements the counter, fires Then, Catch and Finally exactly
// once each, and pushes the next link of a chain. A handler that never calls it
// leaves its batch pending forever, which is the one failure mode worth
// remembering.
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
// The clone at laravel_illuminate/bus:
//
//	Batch.php
//	BatchFactory.php
//	BatchRepository.php
//	Batchable.php
//	BusServiceProvider.php
//	ChainedBatch.php
//	DatabaseBatchRepository.php
//	Dispatcher.php
//	DynamoBatchRepository.php
//	PendingBatch.php
//	PrunableBatchRepository.php
//	Queueable.php
//	UniqueLock.php
//	UpdatedBatchJobCounts.php
//
// DynamoBatchRepository has no equivalent and will not get one: a second store
// with a different consistency model is a second set of rules for when a
// callback fires. BusServiceProvider has none either -- there is no container
// to register into (ADR 0001).
package bus
