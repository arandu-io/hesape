// Package jobs is the job itself: what Push takes, and what the worker holds
// while the work runs.
//
// It mirrors Illuminate\Queue\Jobs. In Laravel that directory holds one class
// per driver -- DatabaseJob, RedisJob, SyncJob -- each overriding the three
// things a running job does about itself: release, delete and fail. Here there
// is one [Job] and a [Driver] interface, because the three subclasses differed
// only in which store they wrote to and a type per store is a second way to say
// the same thing (RULE 9). The queue that popped the job supplies the Driver,
// and [DatabaseJob] and [SyncJob] are aliases so the names still resolve.
//
// The split between this package and [github.com/arandu-io/hesape/queue] is
// Laravel's: Illuminate\Contracts\Queue\Job is the job, Illuminate\Queue\Queue
// is the thing that holds jobs. It is also what keeps the import graph
// one-directional -- queue imports jobs, jobs imports nothing of queue -- so a
// driver in its own module can build a Job without depending on the drivers
// that ship in the collection.
//
// # A field is an accessor
//
// Laravel reads a job through methods -- $job->uuid(), $job->attempts() --
// because the values live in a JSON payload it has to decode. Here they are
// fields, and Job.UUID is the same sentence as uuid() with the parentheses
// removed. The methods that remain are the ones that compute something:
// [Job.Backoff] picks the entry for this attempt, [Job.ResolveName] prefers the
// display name, [Job.IsDeletedOrReleased] reads two flags.
//
// The files it answers to, in the clone at laravel_illuminate/queue/Jobs:
//
//	BeanstalkdJob.php     -- not coming, RULE 11
//	DatabaseJob.php       -> DatabaseJob, and the Driver half of DatabaseQueue
//	DatabaseJobRecord.php -> DatabaseJobRecord
//	FakeJob.php           -> FakeJob
//	Job.php               -> Job
//	JobName.php           -> JobName
//	RedisJob.php          -> the Driver half of connectors/redis
//	SqsJob.php            -- not coming, RULE 11
//	SyncJob.php           -> SyncJob, and the Driver half of SyncQueue
//
// There is no Beanstalkd and no SQS: RULE 11 names the stores this collection
// speaks to, and neither is one of them.
package jobs
