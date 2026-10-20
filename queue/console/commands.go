package console

import (
	"os"

	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/database/migrations"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/failed"
)

// FailedJobs is the dead letter list the failed job commands read, retry,
// forget, flush and prune.
//
// It is the two provider interfaces at once because queue:prune-failed is one
// of the commands: a provider that could list failures and not prune them would
// leave one command wired to nothing. failed.DatabaseFailedJobProvider is both.
type FailedJobs interface {
	failed.FailedJobProvider
	failed.PrunableFailedJobProvider
}

// WorkerDeps is what queue:work builds its worker from, when it runs.
//
// The worker is built by the command rather than handed to it, because three
// of the things it is built from only have their final value then: the flags
// of this invocation, the recorder of the assembled application, and the
// handlers, which reach services wired at boot.
type WorkerDeps struct {
	// Queue is the store the worker drains. Nil is the manager's default
	// connection, which is the one jobs are pushed onto when they name none.
	Queue queue.Queue

	// Options are the worker's defaults, as the queue configuration states
	// them: the queue to drain, how many jobs at once, the lease and the
	// attempts. --queue and --workers override the first two for one
	// invocation and default to what is here, so leaving a flag out is how
	// the configuration is asked for its answer. A zero is the component's
	// own default, as it is everywhere in queue.WorkerOptions.
	//
	// The lease and the attempts have no flag, because neither is a property
	// of one invocation: a lease shorter than the longest handler hands
	// running work to a second worker, and that is true of every worker on
	// the queue rather than of the one somebody just started.
	Options queue.WorkerOptions

	// Register adds the job handlers to the worker. It is the list of every
	// kind of work the application does in the background, and a worker it
	// leaves without a handler is refused rather than started.
	Register func(w *queue.Worker)

	// Recorder is asked for the recorder when the command runs, and what it
	// answers receives every finished job, so a job shows on the debug console
	// with its queries and its timeline exactly like a request. Nil, or a nil
	// answer, is no instrumentation -- which is what production looks like.
	//
	// It is a function rather than a value because the recorder belongs to the
	// assembled application, and the commands are composed before it is.
	Recorder func() *log.Recorder
}

// Deps is what the queue's commands need, built once by the application.
//
// Once, because two of them own a table: the failed job provider knows which
// table the failures are in and the batch repository which one the batches are
// in, and Migrations asks these same values for their schema. A second
// provider built for the migrations could be given another table, and then
// `aru migrate` would create one table and `aru queue:failed` read another.
type Deps struct {
	// Manager answers the connections by name and holds the pause flag and
	// the restart signal. Its cache has to be one every worker can see, or
	// queue:pause reports success against workers that never notice: a
	// manager with no cache answers queue.ErrNoCache instead, which is the
	// honest failure.
	Manager *queue.QueueManager

	// Failures is the dead letter list.
	Failures FailedJobs

	// Batches is where job batches are recorded.
	Batches bus.PrunableBatchRepository

	// Events is where queue:monitor announces a busy queue and queue:retry a
	// retried job. Nil announces nothing, and both commands still do the work.
	Events queue.Dispatcher

	// Listener is what queue:listen restarts after each job. Nil is this
	// binary started again with queue:work, in the working directory --
	// os.Args[0] rather than the CLI, because going back through the CLI
	// would be a second process answering the same.
	Listener *queue.Listener

	// ListenerOptions are queue:listen's options.
	ListenerOptions queue.ListenerOptions

	// Worker is what queue:work builds its worker from.
	Worker WorkerDeps
}

// Commands answers every queue command as values the console registry takes:
// queue:work, queue:listen, queue:restart, queue:pause, queue:resume,
// queue:clear, queue:monitor, queue:failed, queue:retry, queue:forget,
// queue:flush, queue:prune-failed, queue:retry-batch and queue:prune-batches.
//
// One call builds them all over the same Deps, so an application names its
// collaborators once instead of threading them through fourteen constructors,
// and a command that exists is in the slice the dispatch and the listing both
// read.
//
// Manager, Failures and Batches are required: every command but the listener
// reaches one of them.
func Commands(deps Deps) []console.Command {
	listener := deps.Listener
	if listener == nil {
		listener = queue.NewListener(".", os.Args[0], "queue:work")
	}

	return []console.Command{
		NewConfiguredWorkCommand(deps.Manager, deps.Worker).Command(),
		NewListenCommand(listener, deps.ListenerOptions).Command(),
		NewRestartCommand(deps.Manager).Command(),
		NewPauseCommand(deps.Manager).Command(),
		NewResumeCommand(deps.Manager).Command(),
		NewClearCommand(deps.Manager).Command(),
		NewMonitorCommand(deps.Manager, deps.Events).Command(),

		NewListFailedCommand(deps.Failures).Command(),
		NewRetryCommand(deps.Failures, deps.Manager, deps.Events).Command(),
		NewForgetFailedCommand(deps.Failures).Command(),
		NewFlushFailedCommand(deps.Failures).Command(),
		NewPruneFailedJobsCommand(deps.Failures).Command(),

		NewRetryBatchCommand(deps.Batches, deps.Failures, deps.Manager).Command(),
		NewPruneBatchesCommand(deps.Batches).Command(),
	}
}

// Migrations answers the tables the commands read and write that arrive with
// nothing else: the failed jobs table, from Failures, and the batches table,
// from Batches -- each asked of the value the commands use, so the table a
// migration creates is the table a command reads.
//
// A collaborator that keeps no table of its own -- one that does not answer
// Migrations() []migrations.Migration -- contributes nothing. The jobs table is
// not here: it belongs to the queue driver, and arrives with the module that
// registers it.
func (d Deps) Migrations() []migrations.Migration {
	var out []migrations.Migration
	for _, owner := range []any{d.Failures, d.Batches} {
		if schema, ok := owner.(interface{ Migrations() []migrations.Migration }); ok {
			out = append(out, schema.Migrations()...)
		}
	}
	return out
}
