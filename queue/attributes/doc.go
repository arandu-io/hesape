// Package attributes is the per-job settings a job carries: how many tries,
// how long to wait between them, how long the handler may run.
//
// It mirrors Illuminate\Queue\Attributes. In PHP each setting is a class used
// as a PHP attribute on the job class and read back by reflection; in Go they
// are fields of one [Attributes] struct. Which of the two shapes to use was the
// open question, and [Attributes] documents the answer and the reason on the
// type itself, where somebody reading the field will find it.
//
// The files it answers to, in the clone at
// laravel_illuminate/queue/Attributes:
//
//	Backoff.php                 -> Attributes.Backoff
//	Connection.php              -> Attributes.Connection
//	DeleteWhenMissingModels.php -> Attributes.DeleteWhenMissingModels
//	FailOnTimeout.php           -> Attributes.FailOnTimeout
//	MaxExceptions.php           -> Attributes.MaxExceptions
//	Queue.php                   -> Attributes.Queue
//	ReadsQueueAttributes.php    -> ReadsQueueAttributes, Of
//	Timeout.php                 -> Attributes.Timeout
//	Tries.php                   -> Attributes.Tries
//	UniqueFor.php               -> Attributes.UniqueFor
//	WithoutRelations.php        -> Attributes.WithoutRelations
//
// The package depends on nothing but time, so the job record, the drivers and
// the worker can all read it without importing each other.
package attributes
