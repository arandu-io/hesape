// Package failed is where a job goes when it gives up.
//
// It mirrors Illuminate\Queue\Failed: a [FailedJobProvider] interface and the
// implementations of it, so a dead letter list can live somewhere other than
// the queue that produced it.
//
// # The default is that it does not move
//
// A [github.com/arandu-io/hesape/queue.DatabaseQueue] marks the job failed in
// the jobs table it was already in, and queue.Queue.Failed lists them and
// queue.Queue.Retry puts one back. Every driver implements those two, so an
// application that never wires a provider still has a dead letter list, one
// store and one answer to "is this job still queued" (RULE 9).
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
// other: every method takes an auth.Grant and filters by auth.Tenant(g)
// (RULE 17). Laravel's signatures have no equivalent because Laravel has no
// tenant, and a provider that answered across tenants would leak the arguments
// of every job every customer ever queued.
//
// The files it answers to, in the clone at laravel_illuminate/queue/Failed:
//
//	CountableFailedJobProvider.php   -> CountableFailedJobProvider
//	DatabaseFailedJobProvider.php    -> DatabaseFailedJobProvider
//	DatabaseUuidFailedJobProvider.php -> DatabaseUUIDFailedJobProvider
//	DynamoDbFailedJobProvider.php    -- not coming, RULE 11
//	FailedJobProviderInterface.php   -> FailedJobProvider
//	FileFailedJobProvider.php        -> FileFailedJobProvider
//	NullFailedJobProvider.php        -> NullFailedJobProvider
//	PrunableFailedJobProvider.php    -> PrunableFailedJobProvider
//
// DatabaseUUIDFailedJobProvider is an alias of DatabaseFailedJobProvider:
// Laravel has two because its ids are integers and the uuid is a second column,
// and here the id is the uuid, so the two would run the same query.
package failed
