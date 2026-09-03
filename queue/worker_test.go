package queue_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/queue"
	queueconsole "github.com/arandu-io/hesape/queue/console"
	"github.com/arandu-io/hesape/queue/failed"
	"github.com/arandu-io/hesape/queue/jobs"
)

// deadline is how long a test waits for the worker before it says the worker
// never got there. It is a failure deadline and never a synchronisation: every
// step these tests take is a channel.
const deadline = 5 * time.Second

// storeQueue is a driver that answers the way a store does rather than the way
// a map does: a settlement on a cancelled context fails, which is what
// db.ExecContext returns and what an in-memory fake cannot show.
//
// The whole shutdown question is invisible to a driver that ignores its
// context, because there the write that a cancelled shutdown killed still
// appears to have happened.
type storeQueue struct {
	queue.NullQueue

	mu       sync.Mutex
	ready    []jobs.Job
	deleted  []string
	released map[string]time.Duration
	parked   []string
	pushed   []rawPush
	pops     int
	// afterPop runs once a batch has been handed over, with its jobs already
	// reserved. It is where a test puts the signal that has to land in that gap.
	afterPop func()
}

// rawPush is one job put back by queue:retry.
type rawPush struct {
	name    string
	payload string
	queue   string
}

func newStoreQueue(js ...jobs.Job) *storeQueue {
	return &storeQueue{ready: js, released: map[string]time.Duration{}}
}

func (q *storeQueue) GetConnectionName() string { return "store" }

func (q *storeQueue) Pop(ctx context.Context, _ string, n int, _ time.Duration) ([]*jobs.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	q.mu.Lock()
	q.pops++
	if n > len(q.ready) {
		n = len(q.ready)
	}
	out := make([]*jobs.Job, 0, n)
	for _, j := range q.ready[:n] {
		// Like the real drivers: the delivery is counted when the job is handed
		// over, so Attempts includes this one.
		j.Attempts++
		out = append(out, jobs.Popped(q, "store", j))
	}
	q.ready = q.ready[n:]
	after := q.afterPop
	q.mu.Unlock()

	if len(out) > 0 && after != nil {
		after()
	}
	return out, nil
}

func (q *storeQueue) DeleteJob(ctx context.Context, j *jobs.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.deleted = append(q.deleted, j.UUID)
	return nil
}

func (q *storeQueue) ReleaseJob(ctx context.Context, j *jobs.Job, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.released[j.UUID] = delay
	return nil
}

func (q *storeQueue) FailJob(ctx context.Context, j *jobs.Job, _ error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.parked = append(q.parked, j.UUID)
	return nil
}

func (q *storeQueue) PushRaw(_ context.Context, _ auth.Grant, name string, payload []byte, queueName string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pushed = append(q.pushed, rawPush{name: name, payload: string(payload), queue: queueName})
	return nil
}

// settled is what became of the jobs this driver handed over.
func (q *storeQueue) settled() (deleted []string, released map[string]time.Duration, parked []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	released = map[string]time.Duration{}
	for id, delay := range q.released {
		released[id] = delay
	}
	return append([]string(nil), q.deleted...), released, append([]string(nil), q.parked...)
}

// reserved is how many jobs are still out on a lease: handed over and never
// settled. It is the number a shutdown must not leave above zero.
func (q *storeQueue) reserved(handed int) int {
	deleted, released, parked := q.settled()
	return handed - len(deleted) - len(released) - len(parked)
}

func (q *storeQueue) popCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pops
}

func (q *storeQueue) waiting() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.ready)
}

func (q *storeQueue) pushes() []rawPush {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]rawPush(nil), q.pushed...)
}

// newJob builds a job carrying something worth not printing.
func newJob(t *testing.T, name string) jobs.Job {
	t.Helper()
	j, err := jobs.New(grant(), "", name, map[string]string{"card": secretPayload})
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// secretPayload is what a payload holds: a customer's arguments, which belong
// behind a Grant and not in a log line.
const secretPayload = "4111111111111111"

// runDaemon starts the worker and returns the channel its exit arrives on.
func runDaemon(w *queue.Worker, ctx context.Context) <-chan error {
	stopped := make(chan error, 1)
	go func() { stopped <- mustDaemon(w, ctx) }()
	return stopped
}

func awaitStop(t *testing.T, stopped <-chan error) {
	t.Helper()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Daemon: %v", err)
		}
	case <-time.After(deadline):
		t.Fatal("the worker never returned after the shutdown signal")
	}
}

func await(t *testing.T, what string, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(deadline):
		t.Fatalf("%s never happened", what)
	}
}

// TestAJobInFlightIsSettledAfterTheShutdownSignal: the deploy case.
//
// SIGTERM cancels the worker's context, and the delete that says the work is
// done used to ride it. Cancelled, the delete fails, the job stays reserved
// until its lease expires -- five minutes by default -- and comes back with an
// attempt already spent. On every deploy, for every job in flight.
func TestAJobInFlightIsSettledAfterTheShutdownSignal(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)

	running, finish := make(chan struct{}), make(chan struct{})
	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep: time.Millisecond,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		close(running)
		<-finish
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()
	close(finish)
	awaitStop(t, stopped)

	deleted, released, parked := q.settled()
	if len(deleted) != 1 || deleted[0] != j.UUID {
		t.Fatalf("the job was not deleted after the shutdown: deleted=%v released=%v parked=%v",
			deleted, released, parked)
	}
	if left := q.reserved(1); left != 0 {
		t.Errorf("%d job(s) are still reserved after the shutdown", left)
	}
}

// TestAJobThatFailsDuringTheShutdownIsPutBack is the other half: the release
// after a failure rode the cancelled context too, so the job was neither
// deleted nor put back -- reserved, invisible, and waiting out its lease.
func TestAJobThatFailsDuringTheShutdownIsPutBack(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)

	running, finish := make(chan struct{}), make(chan struct{})
	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep: time.Millisecond,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		close(running)
		<-finish
		return errors.New("the payment gateway is down")
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()
	close(finish)
	awaitStop(t, stopped)

	deleted, released, parked := q.settled()
	delay, back := released[j.UUID]
	if !back {
		t.Fatalf("the job was not put back after the shutdown: deleted=%v released=%v parked=%v",
			deleted, released, parked)
	}
	if delay <= 0 {
		t.Errorf("the job came back with no backoff (%s), so a failing handler spins", delay)
	}
}

// TestTheWorkerReservesNothingAfterTheShutdownSignal: draining is finishing
// what is in flight, not taking more. A worker that popped again during the
// drain would leave the batch it took reserved when the process exits.
func TestTheWorkerReservesNothingAfterTheShutdownSignal(t *testing.T) {
	first, second := newJob(t, "invoice.send"), newJob(t, "invoice.send")
	q := newStoreQueue(first, second)

	running, finish := make(chan struct{}), make(chan struct{})
	w := queue.NewWorker(q, queue.WorkerOptions{
		Concurrency: 1,
		Sleep:       time.Millisecond,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		close(running)
		<-finish
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()
	close(finish)
	awaitStop(t, stopped)

	if pops := q.popCount(); pops != 1 {
		t.Errorf("the worker asked for jobs %d times; the signal landed after the first", pops)
	}
	if waiting := q.waiting(); waiting != 1 {
		t.Errorf("%d job(s) left on the queue, want the one that was never reserved", waiting)
	}
	deleted, _, _ := q.settled()
	if len(deleted) != 1 || deleted[0] != first.UUID {
		t.Errorf("the job in flight was not the only one settled: %v", deleted)
	}
}

// TestABatchIsNotAbandonedWhenTheSignalLandsAfterThePop: the gap between the
// reservation and the run.
//
// Every job of a batch is hidden by the same lease at the moment it is popped.
// The loop that started them used to check the context between jobs and break,
// so a signal landing in that gap left the rest of the batch reserved and
// unrun: invisible to every worker, and nothing anywhere recording that they
// were owed.
func TestABatchIsNotAbandonedWhenTheSignalLandsAfterThePop(t *testing.T) {
	first, second := newJob(t, "invoice.send"), newJob(t, "invoice.send")
	q := newStoreQueue(first, second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The signal, exactly between the reservation and the run.
	q.afterPop = cancel

	var ran atomic.Int32
	w := queue.NewWorker(q, queue.WorkerOptions{
		Concurrency: 2,
		Sleep:       time.Millisecond,
	})
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		ran.Add(1)
		return nil
	})

	awaitStop(t, runDaemon(w, ctx))

	if got := ran.Load(); got != 2 {
		t.Errorf("%d of the 2 reserved jobs ran", got)
	}
	if left := q.reserved(2); left != 0 {
		deleted, released, parked := q.settled()
		t.Errorf("%d reserved job(s) were abandoned by the shutdown: deleted=%v released=%v parked=%v",
			left, deleted, released, parked)
	}
}

// TestTheJobsOwnTimeoutEndsAHandlerThatIgnoresTheShutdown: what bounds a
// shutdown is the timeout the job already carries, and nothing else.
//
// A handler that never returns on its own would hold the process open forever,
// and the ceiling on that is the job's timeout -- the same one that ends a hung
// handler with nobody shutting anything down. The shutdown ends nothing: it
// stops the worker reserving more.
//
// Which of the two ended it is readable from the error the handler saw.
// context.DeadlineExceeded is the job's timeout; context.Canceled would be the
// shutdown reaching in and cutting work the store had already handed over.
func TestTheJobsOwnTimeoutEndsAHandlerThatIgnoresTheShutdown(t *testing.T) {
	j := newJob(t, "invoice.send")
	// The worker's own Timeout is its Lease, five minutes here, so a handler
	// that ends in a tenth of a second ended for one reason only.
	j.Attributes.Timeout = 100 * time.Millisecond
	q := newStoreQueue(j)

	running := make(chan struct{})
	ended := make(chan error, 1)
	w := queue.NewWorker(q, queue.WorkerOptions{
		Sleep: time.Millisecond,
	})
	w.HandleFunc("invoice.send", func(ctx context.Context, _ auth.Grant, _ *jobs.Job) error {
		close(running)
		<-ctx.Done()
		ended <- ctx.Err()
		return ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := runDaemon(w, ctx)

	await(t, "the handler starting", running)
	cancel()

	var why error
	select {
	case why = <-ended:
	case <-time.After(deadline):
		t.Fatal("the handler was never ended, so nothing bounds one that ignores its context")
	}
	awaitStop(t, stopped)

	if !errors.Is(why, context.DeadlineExceeded) {
		t.Errorf("the handler ended with %v, want the job's own timeout: "+
			"the shutdown cut work the store had already handed over", why)
	}
	_, released, _ := q.settled()
	if _, back := released[j.UUID]; !back {
		t.Errorf("the job was not put back when its timeout ran out; it is reserved until its lease expires")
	}
}

// recordingProvider is a dead letter list that keeps what it was told and the
// Grant it was told under.
type recordingProvider struct {
	failed.NullFailedJobProvider

	mu      sync.Mutex
	grants  []auth.Grant
	records []failed.FailedJob
	refuse  error
}

func (p *recordingProvider) Log(_ context.Context, g auth.Grant, job failed.FailedJob) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.grants = append(p.grants, g)
	if p.refuse != nil {
		return "", p.refuse
	}
	p.records = append(p.records, job)
	return job.UUID, nil
}

func (p *recordingProvider) seen() ([]auth.Grant, []failed.FailedJob) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]auth.Grant(nil), p.grants...), append([]failed.FailedJob(nil), p.records...)
}

// syncBuffer is a buffer two goroutines may write to, which is what the race
// detector requires of a log sink shared with a worker.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// capturing returns a context whose logger writes where the test can read it.
func capturing(ctx context.Context) (context.Context, *syncBuffer) {
	sink := &syncBuffer{}
	return log.Into(ctx, slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug}))), sink
}

// process runs one delivery of j through w and returns the handler's error.
func deliverOnce(t *testing.T, ctx context.Context, w *queue.Worker, q *storeQueue) error {
	t.Helper()
	popped, err := q.Pop(ctx, "", 1, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(popped) != 1 {
		t.Fatalf("the queue handed over %d jobs", len(popped))
	}
	return w.Process(ctx, popped[0])
}

// runCommand drives one console command and returns what it printed.
func runCommand(t *testing.T, c console.Command, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	io := console.NewIO(c.Name, args, &out, &errOut, strings.NewReader(""))
	err := c.Run(context.Background(), io)
	return out.String() + errOut.String(), err
}

func fileProvider(t *testing.T) *failed.FileFailedJobProvider {
	t.Helper()
	return failed.NewFileFailedJobProvider(filepath.Join(t.TempDir(), "failed.json"), 100)
}

// TestAJobThatGivesUpIsListedByTheFailedJobCommand: the dead letter list an
// operator actually reads.
//
// queue:failed, queue:retry, queue:forget and queue:flush read a
// FailedJobProvider and nothing else. A worker that parked jobs without telling
// one left an operator four commands that print nothing about work that is
// gone.
func TestAJobThatGivesUpIsListedByTheFailedJobCommand(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)
	provider := fileProvider(t)

	w := queue.NewWorker(q, queue.WorkerOptions{MaxTries: 1}).SetFailedJobs(provider)
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		return errors.New("the payment gateway is down")
	})

	if err := deliverOnce(t, context.Background(), w, q); err == nil {
		t.Fatal("the handler failed and Process reported nothing")
	}
	if _, _, parked := q.settled(); len(parked) != 1 {
		t.Fatalf("the driver did not park the job: %v", parked)
	}

	printed, err := runCommand(t, queueconsole.NewListFailedCommand(provider).Command(), "-tenant="+tenant)
	if err != nil {
		t.Fatalf("queue:failed: %v", err)
	}
	if !strings.Contains(printed, j.UUID) || !strings.Contains(printed, "invoice.send") {
		t.Errorf("queue:failed does not list the job that gave up:\n%s", printed)
	}
	if !strings.Contains(printed, "the payment gateway is down") {
		t.Errorf("queue:failed does not say why it gave up:\n%s", printed)
	}
}

// TestTheFailedJobCommandsActOnWhatTheWorkerRecorded: the record is worth
// having only if the commands over it work end to end.
func TestTheFailedJobCommandsActOnWhatTheWorkerRecorded(t *testing.T) {
	retried, forgotten := newJob(t, "invoice.send"), newJob(t, "invoice.send")
	q := newStoreQueue(retried, forgotten)
	provider := fileProvider(t)
	manager := queue.NewQueueManager().Extend("store", q)

	w := queue.NewWorker(q, queue.WorkerOptions{MaxTries: 1}).SetFailedJobs(provider)
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		return errors.New("the payment gateway is down")
	})
	for range 2 {
		if err := deliverOnce(t, context.Background(), w, q); err == nil {
			t.Fatal("the handler failed and Process reported nothing")
		}
	}

	if _, err := runCommand(t, queueconsole.NewRetryCommand(provider, manager, nil).Command(),
		"-tenant="+tenant, retried.UUID); err != nil {
		t.Fatalf("queue:retry: %v", err)
	}
	pushed := q.pushes()
	if len(pushed) != 1 || pushed[0].name != "invoice.send" {
		t.Fatalf("queue:retry put back %v", pushed)
	}
	if pushed[0].payload != string(retried.Payload) {
		t.Errorf("queue:retry pushed %q, want the payload that failed", pushed[0].payload)
	}

	if _, err := runCommand(t, queueconsole.NewForgetFailedCommand(provider).Command(),
		"-tenant="+tenant, forgotten.UUID); err != nil {
		t.Fatalf("queue:forget: %v", err)
	}

	printed, err := runCommand(t, queueconsole.NewListFailedCommand(provider).Command(), "-tenant="+tenant)
	if err != nil {
		t.Fatalf("queue:failed: %v", err)
	}
	if !strings.Contains(printed, "no failed jobs") {
		t.Errorf("the retried and the forgotten job are still listed:\n%s", printed)
	}
}

// TestRecordingTheSameFailureTwiceRecordsItOnce: a dead letter list with two
// rows for one failure is an operator retrying the work twice.
func TestRecordingTheSameFailureTwiceRecordsItOnce(t *testing.T) {
	j := newJob(t, "invoice.send")
	// The same job twice, as an expired lease hands it to a second worker.
	q := newStoreQueue(j, j)
	provider := fileProvider(t)

	w := queue.NewWorker(q, queue.WorkerOptions{MaxTries: 1}).SetFailedJobs(provider)
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		return errors.New("the payment gateway is down")
	})
	for range 2 {
		if err := deliverOnce(t, context.Background(), w, q); err == nil {
			t.Fatal("the handler failed and Process reported nothing")
		}
	}

	all, err := provider.All(context.Background(), auth.SystemGrant(failed.Action, tenant))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("the failed job list holds %d records of one failure", len(all))
	}
}

// TestADeadLetterListThatRefusesIsReportedAndNotSwallowed: the failure of the
// recording is its own news.
//
// It must not become the error the caller sees -- the job did give up, and that
// is what the handler's error says -- and it must not disappear either: a
// provider that has stopped accepting writes is an operator running
// `aru queue:failed` against an empty list after the incident it was for.
func TestADeadLetterListThatRefusesIsReportedAndNotSwallowed(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)
	provider := &recordingProvider{refuse: errors.New("the dead letter table is gone")}

	gatewayDown := errors.New("the payment gateway is down")
	w := queue.NewWorker(q, queue.WorkerOptions{MaxTries: 1}).SetFailedJobs(provider)
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		return gatewayDown
	})

	ctx, logged := capturing(context.Background())
	err := deliverOnce(t, ctx, w, q)

	if !errors.Is(err, gatewayDown) {
		t.Errorf("Process returned %v; the recording masked the job's own error", err)
	}
	if _, _, parked := q.settled(); len(parked) != 1 {
		t.Errorf("the job was not parked: %v", parked)
	}
	if !strings.Contains(logged.String(), "the dead letter table is gone") {
		t.Errorf("nothing was said about the list refusing the record:\n%s", logged.String())
	}
}

// TestTheFailedJobIsRecordedUnderAGrantAndWithoutLeakingThePayload: the record
// is a customer's, and the log is not where a customer's arguments go.
func TestTheFailedJobIsRecordedUnderAGrantAndWithoutLeakingThePayload(t *testing.T) {
	j := newJob(t, "invoice.send")
	q := newStoreQueue(j)
	provider := &recordingProvider{}

	w := queue.NewWorker(q, queue.WorkerOptions{MaxTries: 1}).SetFailedJobs(provider)
	w.HandleFunc("invoice.send", func(context.Context, auth.Grant, *jobs.Job) error {
		return errors.New("the payment gateway is down")
	})

	ctx, logged := capturing(context.Background())
	if err := deliverOnce(t, ctx, w, q); err == nil {
		t.Fatal("the handler failed and Process reported nothing")
	}

	grants, records := provider.seen()
	if len(grants) != 1 || len(records) != 1 {
		t.Fatalf("the list was told about %d failure(s)", len(grants))
	}
	if got := auth.Tenant(grants[0]); got != tenant {
		t.Errorf("the record was written for tenant %q, want the job's own %q", got, tenant)
	}
	if got := grants[0].Action(); got != failed.Action {
		t.Errorf("the record was written under %q, want %q -- what the commands hold", got, failed.Action)
	}
	if records[0].Name != "invoice.send" || string(records[0].Payload) != string(j.Payload) {
		t.Errorf("the record came out as %+v", records[0])
	}
	if strings.Contains(logged.String(), secretPayload) {
		t.Errorf("the payload was written to the log:\n%s", logged.String())
	}
}
