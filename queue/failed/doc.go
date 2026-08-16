// Package failed is where a job goes when it gives up.
//
// It is a [FailedJobProvider] interface and the implementations of it, so a
// dead letter list can live somewhere other than the queue that produced it.
//
// # The default is that it does not move
//
// A [github.com/arandu-io/hesape/queue.DatabaseQueue] marks the job failed in
// the jobs table it was already in, and queue.Queue.Failed lists them and
// queue.Queue.Retry puts one back. Every driver implements those two, so an
// application that never wires a provider still has a dead letter list, one
// store and one answer to "is this job still queued".
//
// What this package is for is the case that arrangement cannot serve: a queue
// whose store is not durable enough to keep failures, or one that is flushed by
// something other than the application. A RESP queue is both. The provider is
// then a deliberate second place, wired on purpose, and the cost -- two stores,
// two retentions -- is paid knowingly.
//
// # Every read takes a Grant
//
// A failed job carries a customer's payload, so listing them is a read like any
// other: every method takes an auth.Grant and filters by auth.Tenant(g). A
// provider that answered across tenants would leak the arguments of every job
// every customer ever queued.
//
// [DatabaseUUIDFailedJobProvider] is an alias of [DatabaseFailedJobProvider],
// because the id is the uuid and the two would run the same query.
package failed
