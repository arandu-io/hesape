package concerns

import "strings"

// ParsesQueue splits a "connection:queue" argument into its two halves.
//
// The pause and resume commands use it, so `aru queue:pause redis:reports`
// means what it looks like it means.
//
// It is a struct with no fields because it has no state: the type is a name for
// the parsing, and `concerns.ParsesQueue{}.ParseQueue(arg)` is all there is to
// it.
type ParsesQueue struct{}

// ParseQueue splits arg into a connection and a queue.
//
// An argument with no colon is a queue on the default connection, and an empty
// queue is the default queue.
//
//	"redis:reports" -> "redis", "reports"
//	"reports"       -> "",      "reports"
//	"redis:"        -> "redis", "default"
//	""              -> "",      "default"
//
// An empty connection means the default rather than being filled in here,
// because the default is the manager's to decide.
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
