// Package jobs is the job itself: what Push takes, and what the worker holds
// while the work runs.
//
// It mirrors Illuminate\Queue\Jobs. In Laravel that directory holds one class
// per driver -- DatabaseJob, RedisJob, SyncJob -- each overriding the three
// things a running job does about itself: release, delete and fail. Here there
// is one [Job] and a [Driver] interface, because the three subclasses differed
// only in which store they wrote to and a type per store is a second way to say
// the same thing (RULE 9). The queue that popped the job supplies the Driver.
//
// The split between this package and [github.com/arandu-io/hesape/queue] is
// Laravel's: Illuminate\Contracts\Queue\Job is the job, Illuminate\Queue\Queue
// is the thing that holds jobs. It is also what keeps the import graph
// one-directional -- queue imports jobs, jobs imports nothing of queue -- so a
// driver in its own module can build a Job without depending on the drivers
// that ship in the collection.
//
// The files it answers to, in the clone at laravel_illuminate/queue/Jobs:
//
//	BeanstalkdJob.php
//	DatabaseJob.php
//	DatabaseJobRecord.php
//	FakeJob.php
//	Job.php
//	JobName.php
//	RedisJob.php
//	SqsJob.php
//	SyncJob.php
//
// Job.php, DatabaseJobRecord.php and the release/delete/fail half of
// DatabaseJob.php, RedisJob.php and SyncJob.php have an answer here. There is
// no Beanstalkd and no SQS: RULE 11 names the stores this collection speaks to,
// and neither is one of them.
package jobs
