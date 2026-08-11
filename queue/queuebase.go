package queue

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/jobs"
)

// connection is the half of Illuminate\Queue\Queue every driver shares: the
// name it was registered under.
//
// It is an unexported embedded struct rather than an exported base type,
// because Go promotes the methods either way and an exported one would be a
// second thing called Queue in a package that already has the contract by that
// name. Every driver in this package embeds it, which is what makes
// GetConnectionName answerable on all of them without six copies of two lines.
//
// The container and the config block that Laravel's base class also carries are
// not here: the application constructs its queues in bootstrap/app.go and hands
// them to [QueueManager] (ADR 0001), so there is no config array to stash and
// nothing to resolve out of a container.
type connection struct {
	name string
}

// GetConnectionName is the name this queue was registered under.
//
// It answers getConnectionName(). It is empty on a queue built directly and
// never handed to a [QueueManager], which is what a test does.
func (c *connection) GetConnectionName() string { return c.name }

// SetConnectionName names the connection this queue answers to.
//
// It answers setConnectionName(). It returns nothing where PHP returns $this:
// the fluent form is there so a connector can chain it onto the queue it just
// built, and an embedded struct in Go cannot return the type that embeds it.
func (c *connection) SetConnectionName(name string) { c.name = name }

// LaterOn adds a job to a named queue, eligible after delay.
//
// It answers Illuminate\Queue\Queue::laterOn(), which in PHP is inherited by
// every driver from the abstract base. Go has no inheritance, so the two
// methods that are pure rearrangements of their arguments -- this one and
// PushOn -- are a package function and an interface method respectively:
// PushOn is on the contract because a driver can do it in one round trip,
// and this one cannot be anything but Later with the queue overwritten.
//
//	queue.LaterOn(ctx, q, g, "reports", time.Hour, j)
func LaterOn(ctx context.Context, q Queue, g auth.Grant, name string, delay time.Duration, j jobs.Job) error {
	j.Queue = name
	return q.Later(ctx, g, delay, j)
}
