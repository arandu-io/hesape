// Package failed mirrors Illuminate\Queue\Failed, and is deliberately empty.
//
// # The failed jobs stay where they failed
//
// In Laravel a job that gives up is copied into a second table by a
// FailedJobProvider, and there are eight of them -- database, database with
// uuids, DynamoDB, file, null, and the wrappers around those. Here the job is
// marked failed in the store it was already in: queue.Queue.Failed lists them
// and queue.Queue.Retry puts one back, and both are implemented by every
// driver.
//
// That is RULE 9 rather than a shortcut. A separate provider is a second place
// a job can be, with its own configuration, its own migration and its own
// answer to "is this job still queued" -- and the eight implementations exist
// because the second place can be a different store from the first, which is
// the same choice made twice. One store, one answer.
//
// The cost is real and worth naming: a failed job holds a row in the jobs
// table, so a queue that fails a lot has a table that grows. The pop query
// filters failed_at out, so nothing slows down; the row is the thing somebody
// eventually deletes, with Retry or by hand.
//
// The files it answers to, in the clone at laravel_illuminate/queue/Failed:
//
//	CountableFailedJobProvider.php
//	DatabaseFailedJobProvider.php
//	DatabaseUuidFailedJobProvider.php
//	DynamoDbFailedJobProvider.php
//	FailedJobProviderInterface.php
//	FileFailedJobProvider.php
//	NullFailedJobProvider.php
//	PrunableFailedJobProvider.php
//
// The directory exists so the mirror of Illuminate\Queue is complete, and so
// that somebody looking for the failed job provider finds the reason rather
// than silence.
package failed
