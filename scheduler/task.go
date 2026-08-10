package scheduler

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// Scope says whether a task runs once or once per tenant.
type Scope int

const (
	// Global runs the task once for the whole instance.
	//
	// It gets the zero Grant, because SystemGrant refuses an empty tenant
	// (RULE 14) -- so a global task cannot pass any Check and cannot reach a
	// repository. That is a constraint rather than an oversight: global work is
	// cleaning temporary files, warming a cache, checking a certificate. Work
	// that reads a customer's rows is PerTenant, and having to say so is the
	// point.
	Global Scope = iota
	// PerTenant expands the task to every active tenant, each with its own
	// Grant and its own lock.
	PerTenant
)

// Task is scheduled work.
//
// The shape mirrors a module's migrations: the module declares, the kernel
// collects, and nothing runs until something asks. What a module never does is
// start its own goroutine.
type Task struct {
	// ID identifies the task in logs, in `aru schedule:list` and in the lock.
	// It is stable: changing it starts a new task rather than renaming one.
	ID string
	// Spec is a five-field cron expression: minute hour day month weekday.
	Spec string
	// Scope decides Global or PerTenant.
	Scope Scope
	// Timeout bounds one run, and is also the TTL of the window lock. Zero
	// means five minutes, which outlasts the minute a window covers -- set it
	// shorter than the interval between two ticks of the same window and two
	// replicas can each run it.
	Timeout time.Duration
	// Singleton takes the distributed lock, so exactly one replica runs it.
	// Set it false only for work that is harmless to do N times.
	Singleton bool
	// Action is what the run is authorized for. It becomes the SystemGrant the
	// task receives, so a task reaches repositories the same way a request
	// does -- there is no unauthorized path from the scheduler either.
	Action auth.Action
	// Run does the work. It gets the Grant built from Action and the tenant.
	Run func(ctx context.Context, g auth.Grant) error
}

// Schedulable is optional: the module declares its scheduled work.
//
// It lives here rather than with the other optional module interfaces because
// it is the only one whose signature is a type of this package, and a contract
// stated next to the type it carries is the one people find.
type Schedulable interface {
	Schedule() []Task
}
