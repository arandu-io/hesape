package console_test

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/queue"
	queueconsole "github.com/arandu-io/hesape/queue/console"
	"github.com/arandu-io/hesape/queue/failed"
	"github.com/arandu-io/hesape/queue/jobs"
)

// The provider and the repository an application wires satisfy what the
// aggregator asks for, checked by the compiler rather than at the first run.
var (
	_ queueconsole.FailedJobs     = (*failed.DatabaseFailedJobProvider)(nil)
	_ bus.PrunableBatchRepository = (*bus.DatabaseBatchRepository)(nil)
)

// poppingQueue records what each Pop asked for and hands out the jobs it holds,
// once each.
type poppingQueue struct {
	queue.NullQueue

	mu      sync.Mutex
	asked   []popRequest
	pending []*jobs.Job
}

type popRequest struct {
	queue string
	count int
	lease time.Duration
}

func (q *poppingQueue) Pop(_ context.Context, name string, count int, lease time.Duration) ([]*jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.asked = append(q.asked, popRequest{queue: name, count: count, lease: lease})
	popped := q.pending
	q.pending = nil
	return popped, nil
}

func (q *poppingQueue) requests() []popRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]popRequest(nil), q.asked...)
}

// configuredDeps are the queue's collaborators over store, with one handler
// registered and the options a configuration would state.
func configuredDeps(store queue.Queue) queueconsole.Deps {
	manager := queue.NewQueueManager().SetCache(cache.NewArrayStore()).Extend("database", store)
	return queueconsole.Deps{
		Manager:  manager,
		Failures: failed.NewDatabaseFailedJobProvider(nil, ""),
		Batches:  bus.NewDatabaseBatchRepository(nil),
		Worker: queueconsole.WorkerDeps{
			Options: queue.WorkerOptions{
				Queue:       "reports",
				Concurrency: 7,
				Lease:       90 * time.Second,
				Sleep:       time.Millisecond,
			},
			Register: func(w *queue.Worker) {
				w.HandleFunc("report.build", func(context.Context, auth.Grant, *jobs.Job) error { return nil })
			},
		},
	}
}

// workCommand finds queue:work in what the aggregator answered, and returns
// what runs it with arguments.
func workCommand(t *testing.T, deps queueconsole.Deps) func(...string) (string, error) {
	t.Helper()
	for _, c := range queueconsole.Commands(deps) {
		if c.Name == "queue:work" {
			return func(args ...string) (string, error) { return run(t, c, args...) }
		}
	}
	t.Fatal("Commands answered no queue:work")
	return nil
}

// TestCommandsAnswerEveryQueueCommand: the aggregator is the one slice the
// dispatch and the listing read, so a command missing from it is a command
// `aru` forwards and the binary calls unknown -- which is how thirteen of them
// went unanswered while each application wrote its own list.
func TestCommandsAnswerEveryQueueCommand(t *testing.T) {
	want := []string{
		"queue:clear", "queue:failed", "queue:flush", "queue:forget", "queue:listen",
		"queue:monitor", "queue:pause", "queue:prune-batches", "queue:prune-failed",
		"queue:restart", "queue:resume", "queue:retry", "queue:retry-batch", "queue:work",
	}

	var got []string
	seen := map[string]bool{}
	for _, c := range queueconsole.Commands(configuredDeps(&poppingQueue{})) {
		if seen[c.Name] {
			t.Fatalf("%s is answered twice", c.Name)
		}
		seen[c.Name] = true
		got = append(got, c.Name)
	}
	sort.Strings(got)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Commands answered\n  %v\nwant\n  %v", got, want)
	}
}

// TestWorkTakesItsDefaultsFromTheConfiguration: with no flag, the worker drains
// the configured queue, as wide as configured, under the configured lease --
// not a number of the command's own that silently wins when a flag is left out.
func TestWorkTakesItsDefaultsFromTheConfiguration(t *testing.T) {
	store := &poppingQueue{}
	work := workCommand(t, configuredDeps(store))

	if _, err := work("--once"); err != nil {
		t.Fatalf("queue:work --once: %v", err)
	}

	asked := store.requests()
	if len(asked) != 1 || asked[0] != (popRequest{queue: "reports", count: 7, lease: 90 * time.Second}) {
		t.Fatalf("the worker asked for %+v, want the configured queue, width and lease", asked)
	}
}

// TestWorkersAndQueueFlagsOverrideTheConfigurationForOneRun: --workers and
// --queue are this invocation's, and the lease, which has no flag, stays the
// configuration's.
func TestWorkersAndQueueFlagsOverrideTheConfigurationForOneRun(t *testing.T) {
	store := &poppingQueue{}
	work := workCommand(t, configuredDeps(store))

	if _, err := work("--once", "--workers=2", "--queue=mail"); err != nil {
		t.Fatalf("queue:work: %v", err)
	}

	asked := store.requests()
	if len(asked) != 1 || asked[0] != (popRequest{queue: "mail", count: 2, lease: 90 * time.Second}) {
		t.Fatalf("the worker asked for %+v, want mail, two at once, the configured lease", asked)
	}
}

// TestWorkRefusesANegativeWorkerCount: there is no number of jobs below none.
func TestWorkRefusesANegativeWorkerCount(t *testing.T) {
	store := &poppingQueue{}
	work := workCommand(t, configuredDeps(store))

	if _, err := work("--once", "--workers=-1"); err == nil {
		t.Fatal("queue:work ran with --workers=-1")
	}
	if got := store.requests(); len(got) != 0 {
		t.Fatalf("a refused worker still asked the queue for %+v", got)
	}
}

// TestWorkRefusesAWorkerWithNoHandlers: a worker that knows no job by name
// takes each job off the queue only to fail it, so it is not started.
func TestWorkRefusesAWorkerWithNoHandlers(t *testing.T) {
	store := &poppingQueue{}
	deps := configuredDeps(store)
	deps.Worker.Register = nil
	work := workCommand(t, deps)

	_, err := work("--once")
	if err == nil || !strings.Contains(err.Error(), "no job handlers are registered") {
		t.Fatalf("queue:work with no handlers answered %v", err)
	}
	if got := store.requests(); len(got) != 0 {
		t.Fatalf("a worker with no handlers still asked the queue for %+v", got)
	}
}

// TestWorkHandsEveryFinishedJobToTheRecorder: the recorder is asked for when
// the command runs, and a finished job lands on the debug console like a
// request does.
func TestWorkHandsEveryFinishedJobToTheRecorder(t *testing.T) {
	job, err := jobs.New(auth.SystemGrant("report.build", "tenant-1"), "reports", "report.build", map[string]string{"month": "2026-10"})
	if err != nil {
		t.Fatalf("jobs.New: %v", err)
	}
	store := &poppingQueue{pending: []*jobs.Job{&job}}

	recorder := log.NewRecorder(8)
	deps := configuredDeps(store)
	deps.Worker.Recorder = func() *log.Recorder { return recorder }
	work := workCommand(t, deps)

	if _, err := work("--once"); err != nil {
		t.Fatalf("queue:work --once: %v", err)
	}
	if recorder.Len() != 1 {
		t.Fatalf("the recorder holds %d entries, want the one job that finished", recorder.Len())
	}
}

// TestWorkDrainsTheManagersDefaultConnection: with no store named, the worker
// drains the connection jobs are pushed onto when they name none.
func TestWorkDrainsTheManagersDefaultConnection(t *testing.T) {
	store := &poppingQueue{}
	deps := configuredDeps(store)
	deps.Worker.Queue = nil
	work := workCommand(t, deps)

	if _, err := work("--once"); err != nil {
		t.Fatalf("queue:work --once: %v", err)
	}
	if got := store.requests(); len(got) != 1 {
		t.Fatalf("the default connection was asked %d time(s), want 1", len(got))
	}
}

// TestDepsMigrationsAreTheTablesOfTheValuesTheCommandsUse: the failed jobs
// table and the batches table arrive with nothing else, and are asked of the
// same provider and repository the commands read, so a table name given to
// one is the table the other creates.
func TestDepsMigrationsAreTheTablesOfTheValuesTheCommandsUse(t *testing.T) {
	deps := configuredDeps(&poppingQueue{})
	deps.Failures = failed.NewDatabaseFailedJobProvider(nil, "dead_letters")

	var names []string
	for _, migration := range deps.Migrations() {
		names = append(names, migration.GetName())
	}
	joined := strings.Join(names, ",")

	for _, want := range []string{"2026_08_10_000001_create_job_batches_table"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Migrations() = %v, missing %s", names, want)
		}
	}
	want := len(failed.NewDatabaseFailedJobProvider(nil, "dead_letters").Migrations()) + len(bus.Migrations())
	if len(names) != want {
		t.Fatalf("Migrations() answered %d migrations, want %d: the failed jobs and the batches tables, once each", len(names), want)
	}

	var created bool
	for _, migration := range deps.Migrations() {
		if table, ok := migration.(failed.CreateFailedJobsTable); ok {
			created = table.Table == "dead_letters"
		}
	}
	if !created {
		t.Fatal("the failed jobs table was not asked of the provider the commands read, so it would be created under another name")
	}
}
