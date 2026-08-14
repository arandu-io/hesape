package queue

import "time"

// Config is config/queue.php: which connection the application dispatches to,
// and how each one behaves.
//
// It is declared here, in the package that reads it, and not in a configuration
// package of its own. Without a container nothing looks a value up by key, so
// the component is handed its settings and the compiler checks the field
// (ADR 0051).
type Config struct {
	// Default is the connection a Dispatch goes to when it names none. Laravel
	// reads QUEUE_CONNECTION and defaults to "database", and so does this.
	Default string

	// Connections is every connection the application knows, by name.
	Connections map[string]ConnectionConfig

	// Failed is where a job goes when it has run out of tries.
	Failed FailedConfig
}

// ConnectionConfig is one entry of config/queue.php's connections array.
type ConnectionConfig struct {
	// Driver is "sync", "database" or "redis".
	//
	// Laravel also ships beanstalkd, sqs and null. Those are not here and are
	// not pending: beanstalkd and SQS are drivers this ecosystem does not carry
	// (ADR 0016 put the queue backends in their own module, one per protocol),
	// and "null" is a driver that silently discards work -- a thing to reach for
	// in a test, which is what "sync" and a recorder already answer without
	// pretending a job ran.
	Driver string

	// Name is the connection's own name, so that a value carries it without the
	// map key having to travel alongside.
	Name string

	// Queue is the queue a job lands on when it names none.
	Queue string

	// RetryAfter is how long a reserved job may be held before another worker
	// may take it. Laravel's default is 90 seconds.
	//
	// It has to be longer than the longest job actually takes. Shorter, and a
	// slow job is picked up a second time while the first run is still going --
	// which is at-least-once delivery arriving as a duplicate that nobody asked
	// for.
	RetryAfter time.Duration

	// Table is the table a database connection reads. Laravel's DB_QUEUE_TABLE,
	// default "jobs".
	Table string

	// Connection is the database or redis connection this queue rides on, empty
	// meaning the application's default.
	Connection string

	// AfterCommit delays the dispatch until the surrounding transaction commits.
	//
	// Laravel defaults it to false and documents the failure it causes: a job
	// dispatched inside a transaction can be picked up by a worker before the
	// transaction commits, and it then reads a row that does not exist yet.
	//
	// It is false here too, and the reason is not compatibility -- it is that
	// events already have the correct answer to this and jobs should use it. The
	// outbox writes the record in the same transaction as the change that
	// produced it, so there is no window at all. AfterCommit narrows the window;
	// the outbox closes it.
	AfterCommit bool
}

// FailedConfig is config/queue.php's failed array: where a job goes when it has
// run out of tries.
type FailedConfig struct {
	// Driver is "database" or empty for none.
	Driver string

	// Database is the connection the failed table lives on, empty meaning the
	// application's default.
	Database string

	// Table is the table itself. Laravel's default is "failed_jobs".
	Table string
}
