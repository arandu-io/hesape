package concerns

import "strings"

// ParsesQueue splits a "connection:queue" argument into its two halves.
//
// It answers Illuminate\Queue\Console\Concerns\ParsesQueue, the trait the pause
// and resume commands use so `aru queue:pause redis:reports` means what it
// looks like it means.
//
// It is a struct with no fields because the PHP is a trait with one method and
// no state: `concerns.ParsesQueue{}.ParseQueue(arg)` is the same sentence as
// `$this->parseQueue($arg)`, and the type keeps the name a Laravel developer
// searches for.
type ParsesQueue struct{}

// ParseQueue splits arg into a connection and a queue.
//
// It answers parseQueue(). An argument with no colon is a queue on the default
// connection, and an empty queue is the default queue -- both of which are what
// the PHP's array_pad and `?:` do, said in a language that has two return
// values.
//
//	"redis:reports" -> "redis", "reports"
//	"reports"       -> "",      "reports"
//	"redis:"        -> "redis", "default"
//	""              -> "",      "default"
//
// An empty connection means the default rather than being filled in here: the
// default is the manager's, and a trait that read it out of a config array is
// the one thing this collection does not have (ADR 0001).
func (ParsesQueue) ParseQueue(arg string) (connection, queue string) {
	if before, after, found := strings.Cut(arg, ":"); found {
		connection, queue = before, after
	} else {
		queue = arg
	}
	if queue == "" {
		queue = "default"
	}
	return connection, queue
}
