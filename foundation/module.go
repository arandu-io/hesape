package foundation

import (
	"context"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// A module is a directory. It registers its own routes, its own migrations and
// its own dependency graph. There is no injection container and no reflection
// based resolution: the wiring is explicit, and the CLI generates the file that
// instantiates everything.
//
// The Module interface itself -- Name plus Routes -- is not declared yet,
// because Routes names the router and hesape/routing is empty. See the package
// comment. Everything below is optional and independent of it: a module
// implements as many of these as it has reasons to.

// Bootable is optional: implement it when the module needs to prepare state at
// boot -- open a pool, warm a cache, register codecs.
//
// Boot wires; it does not run. A module that needs a loop of its own implements
// [Background] instead, and validates in Boot whatever would make that loop
// fail.
type Bootable interface {
	Boot(ctx context.Context) error
}

// Background is optional: the module runs a loop of its own -- the scheduler
// and the outbox relay do.
//
// Start is called by Run, never by Boot, and that distinction is the difference
// between a process that serves and a process that does something else. Every
// command boots: `aru work`, `aru routes`, `aru schedule:list`, `aru migrate`.
// Starting the loops at boot meant every worker replica also ran a scheduler
// and a relay, and `aru schedule:run`, which exists to run one task by hand,
// started the loop that runs all of them. Found by audit.
//
// One process, one job. The lock in the scheduler makes the duplicate harmless
// rather than correct, and "harmless because something else catches it" is not
// a design.
type Background interface {
	Start(ctx context.Context) error
}

// Closable is optional: implement it to release resources on shutdown.
type Closable interface {
	Close(ctx context.Context) error
}

// Diagnostic is optional: the module reports what it knows about the state of
// the system, in sentences a person can act on.
//
// It feeds the error page. The most useful hint is often about something that
// happened outside the failing request -- the outbox stuck for four minutes, a
// job that has not run -- and a page that only looks at the request cannot see
// any of it.
//
// Return nothing when there is nothing wrong. A diagnosis that always says
// something is a diagnosis nobody reads.
type Diagnostic interface {
	Diagnose(ctx context.Context) []string
}

// Scope says whether a task runs once or once per tenant.
type Scope int

const (
	// Global runs the task once for the whole instance.
	//
	// It gets the zero Grant, because [auth.SystemGrant] refuses an empty
	// tenant (RULE 14) -- so a global task cannot pass any Check and cannot
	// reach a repository. That is a constraint rather than an oversight: global
	// work is cleaning temporary files, warming a cache, checking a
	// certificate. Work that reads a customer's rows is PerTenant, and having
	// to say so is the point.
	Global Scope = iota
	// PerTenant expands the task to every active tenant, each with its own
	// Grant and its own lock.
	PerTenant
)

// Task is scheduled work.
//
// The shape mirrors Migrations(): the module declares, the kernel collects, and
// nothing runs until something asks. What a module never does is start its own
// goroutine.
type Task struct {
	// ID identifies the task in logs, in `aru schedule:list` and in the lock.
	// It is stable: changing it starts a new task rather than renaming one.
	ID string
	// Spec is a five-field cron expression: minute hour day month weekday.
	Spec string
	// Scope decides Global or PerTenant.
	Scope Scope
	// Timeout bounds one run, and is also the lock TTL: a process that dies
	// holding the lock releases it when the timeout passes.
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
type Schedulable interface {
	Schedule() []Task
}

// Migratable is optional: the module declares its migrations, and the kernel
// collects them from every registered module in registration order.
type Migratable interface {
	Migrations() []Migration
}

// Migration is a versioned, immutable-once-published schema change.
//
// It is an alias, not a copy: the migration runner lives in the database
// package, and a module must be able to hand its migrations straight to it.
type Migration = database.Migration

// Health is optional and feeds `aru doctor` and the /_arandu/health endpoint.
type Health interface {
	Health(ctx context.Context) error
}
