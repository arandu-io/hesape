package queue_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

// jobColumns is the projection every listing query returns.
var jobColumns = []string{
	"id", "queue", "name", "display_name", "tenant_id", "payload",
	"authorized_by", "action", "run_at", "attempts", "exceptions",
	"last_error", "attributes",
}

// jobRow builds one row of that projection.
func jobRow(id, name string, runAt time.Time, attempts int64, lastError any) []driver.Value {
	return []driver.Value{
		id, jobs.DefaultQueue, name, nil, tenant, "{}",
		"system", "invoice.send", runAt, attempts, int64(0), lastError, nil,
	}
}

func databaseQueue(t *testing.T) (*queue.DatabaseQueue, *fakeDB) {
	t.Helper()
	sqldb, state := newFakeDB()
	t.Cleanup(func() { _ = sqldb.Close() })
	return queue.NewDatabaseQueue(database.Wrap(sqldb, database.DialectSQLite)), state
}

func TestPushWritesTheJobRow(t *testing.T) {
	q, state := databaseQueue(t)
	j, err := jobs.New(grant(), "", "invoice.send", map[string]string{"id": "i-1"})
	if err != nil {
		t.Fatal(err)
	}

	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !state.sawStatement("INSERT INTO jobs") {
		t.Fatalf("Push issued %v", state.statements())
	}

	args := state.argsFor("INSERT INTO jobs")
	if len(args) != 13 {
		t.Fatalf("the insert bound %d arguments", len(args))
	}
	if got := args[0].Value; got != j.UUID {
		t.Errorf("the row was written under %v, want %s", got, j.UUID)
	}
	if got := args[4].Value; got != tenant {
		t.Errorf("tenant_id = %v, want the one on the Grant", got)
	}
	// The Grant that authorized the push is on the row, because it is what the
	// worker reissues the work under.
	if got := args[7].Value; got != "invoice.send" {
		t.Errorf("action = %v", got)
	}
	runAt, isTime := args[8].Value.(time.Time)
	if !isTime {
		t.Fatalf("run_at was bound as %T", args[8].Value)
	}
	// UTC, always: an engine that compares timestamps as text stored a job from
	// a UTC-3 machine three hours in the past and ran it immediately.
	if runAt.Location() != time.UTC {
		t.Errorf("run_at was written in %s", runAt.Location())
	}
}

// TestPushRefusesAForgedJobBeforeItReachesTheDatabase: the check is not a
// warning, it is what stops the queue being a way past authorization.
func TestPushRefusesAForgedJobBeforeItReachesTheDatabase(t *testing.T) {
	q, state := databaseQueue(t)

	forged := jobs.Job{UUID: "j-1", Name: "invoice.send", Action: "invoice.delete"}
	if err := q.Push(context.Background(), grant(), forged); !errors.Is(err, jobs.ErrForged) {
		t.Fatalf("err = %v, want ErrForged", err)
	}
	if len(state.statements()) != 0 {
		t.Errorf("a forged job reached the database: %v", state.statements())
	}
}

func TestLaterSchedulesTheJobForward(t *testing.T) {
	q, state := databaseQueue(t)
	j, _ := jobs.New(grant(), "", "invoice.send", nil)

	before := time.Now().UTC()
	if err := q.Later(context.Background(), grant(), time.Hour, j); err != nil {
		t.Fatalf("Later: %v", err)
	}

	runAt, isTime := state.argsFor("INSERT INTO jobs")[8].Value.(time.Time)
	if !isTime {
		t.Fatal("run_at was not bound as a time")
	}
	if runAt.Before(before.Add(time.Hour)) {
		t.Errorf("run_at = %s, want an hour after %s", runAt, before)
	}
}

func TestPushOnOverridesTheQueueOnTheJob(t *testing.T) {
	q, state := databaseQueue(t)
	j, _ := jobs.New(grant(), "mail", "invoice.send", nil)

	if err := q.PushOn(context.Background(), grant(), "reports", j); err != nil {
		t.Fatalf("PushOn: %v", err)
	}
	if got := state.argsFor("INSERT INTO jobs")[1].Value; got != "reports" {
		t.Errorf("queue = %v, want reports", got)
	}
}

func TestBulkWritesEveryJob(t *testing.T) {
	q, state := databaseQueue(t)
	first, _ := jobs.New(grant(), "", "invoice.send", nil)
	second, _ := jobs.New(grant(), "", "invoice.send", nil)

	if err := q.Bulk(context.Background(), grant(), []jobs.Job{first, second}); err != nil {
		t.Fatalf("Bulk: %v", err)
	}
	var inserts int
	for _, s := range state.statements() {
		if strings.Contains(s, "INSERT INTO jobs") {
			inserts++
		}
	}
	if inserts != 2 {
		t.Errorf("Bulk issued %d inserts for two jobs", inserts)
	}
}

// TestPopClaimsWithACompareAndSet: two workers can select the same candidate,
// and the claim is what decides between them. Without reserved_until in the
// WHERE, both would get the job.
func TestPopClaimsWithACompareAndSet(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-1", "invoice.send", time.Now().Add(-time.Minute), 0, nil))

	popped, err := q.Pop(context.Background(), "", 4, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(popped) != 1 {
		t.Fatalf("Pop returned %d jobs", len(popped))
	}

	if !state.sawStatement("UPDATE jobs SET reserved_until = ?, attempts = attempts + 1 WHERE id = ? AND (reserved_until IS NULL OR reserved_until < ?)") {
		t.Fatalf("the claim is not a compare-and-set: %v", state.statements())
	}
	// The delivery is counted when the job is handed over, so a job being
	// handled for the first time has Attempts == 1. A driver that returns the
	// count from before the delivery makes a try limit of 2 park on the first
	// failure.
	if popped[0].Attempts != 1 {
		t.Errorf("attempts = %d on the first delivery", popped[0].Attempts)
	}
	// The job carries the queue it came off, which is what lets the worker
	// settle it without knowing which driver it is.
	if popped[0].GetConnectionName() != "database" {
		t.Errorf("connection = %q", popped[0].GetConnectionName())
	}
}

// TestPopReturnsNothingWhenTheClaimIsLost: another worker got there between the
// select and the update. Not an error -- that is the compare-and-set working.
func TestPopReturnsNothingWhenTheClaimIsLost(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-1", "invoice.send", time.Now().Add(-time.Minute), 0, nil))
	state.setRowsAffected(0)

	popped, err := q.Pop(context.Background(), "", 4, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(popped) != 0 {
		t.Fatalf("a job whose claim was lost was handed to a worker: %+v", popped)
	}
}

// TestPopOnlyLooksAtJobsThatAreDueAndNotRunning is the definition of "waiting",
// and it has to be the same one PendingSize uses or a running job reads as a
// backlog.
func TestPopOnlyLooksAtJobsThatAreDueAndNotRunning(t *testing.T) {
	q, state := databaseQueue(t)
	if _, err := q.Pop(context.Background(), "mail", 1, time.Minute); err != nil {
		t.Fatalf("Pop: %v", err)
	}

	if !state.sawStatement("WHERE queue = ? AND failed_at IS NULL AND run_at <= ? AND (reserved_until IS NULL OR reserved_until < ?) ORDER BY COALESCE(created_at, run_at), id LIMIT ?") {
		t.Fatalf("the candidate query changed shape: %v", state.statements())
	}
}

// TestPopTakesTheJobThatWasQueuedFirst: A is pushed with a delay of ten
// seconds, B is pushed straight after with none. Ten seconds later both are
// eligible, and A goes first -- insertion order, because a delayed job that has
// come due is older than everything queued after it.
//
// Ordering by run_at hands over B, and it does that to every delayed job: the
// job waits out its delay and then goes to the back of a queue that filled up
// while it waited. On a busy queue that is not a reordering, it is a job that
// never runs.
//
// The column standing in for the id is created_at, because an id here is a
// random uuid and sorts by nothing (see database.NewID).
func TestPopTakesTheJobThatWasQueuedFirst(t *testing.T) {
	ctx := context.Background()
	q, state := databaseQueue(t)

	first, _ := jobs.New(grant(), "", "invoice.send", nil)
	if err := q.Later(ctx, grant(), 10*time.Second, first); err != nil {
		t.Fatalf("Later: %v", err)
	}
	second, _ := jobs.New(grant(), "", "invoice.send", nil)
	if err := q.Push(ctx, grant(), second); err != nil {
		t.Fatalf("Push: %v", err)
	}

	inserts := state.argsForAll("INSERT INTO jobs")
	if len(inserts) != 2 {
		t.Fatalf("the queue issued %d inserts", len(inserts))
	}
	queuedAt := make([]time.Time, 0, 2)
	for _, args := range inserts {
		at, isTime := args[len(args)-1].Value.(time.Time)
		if !isTime {
			t.Fatalf("the last bound column is %T, want the created_at of the row", args[len(args)-1].Value)
		}
		if at.Location() != time.UTC {
			t.Errorf("created_at was written in %s", at.Location())
		}
		queuedAt = append(queuedAt, at)
	}
	if queuedAt[1].Before(queuedAt[0]) {
		t.Errorf("the second push is dated %s, before the first at %s", queuedAt[1], queuedAt[0])
	}

	if _, err := q.Pop(ctx, "mail", 1, time.Minute); err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if !state.sawStatement("ORDER BY COALESCE(created_at, run_at), id LIMIT ?") {
		t.Fatalf("the candidates are not taken in the order they were queued: %v", state.statements())
	}
}

// TestReleasingAJobWritesItsExceptionCount: the count has to cross deliveries.
// With the column unwritten by the release, every delivery reads zero and a job
// with MaxExceptions 2 and MaxTries 5 is delivered all five times.
func TestReleasingAJobWritesItsExceptionCount(t *testing.T) {
	ctx := context.Background()
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-1", "invoice.send", time.Now().Add(-time.Minute), 1, nil))

	popped, err := q.Pop(ctx, "", 1, time.Minute)
	if err != nil || len(popped) != 1 {
		t.Fatalf("Pop: %v, %d", err, len(popped))
	}

	popped[0].Exceptions = 2
	if err := popped[0].Release(ctx, time.Minute); err != nil {
		t.Fatalf("Release: %v", err)
	}

	args := state.argsFor("UPDATE jobs SET run_at = ?, last_error = ?, exceptions = ?")
	if args == nil {
		t.Fatalf("Release issued %v", state.statements())
	}
	if got := args[2].Value; got != int64(2) {
		t.Errorf("exceptions = %v, want 2", got)
	}
}

func TestReleasePutsTheJobBackWithItsReason(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-1", "invoice.send", time.Now().Add(-time.Minute), 0, nil))

	popped, err := q.Pop(context.Background(), "", 1, time.Minute)
	if err != nil || len(popped) != 1 {
		t.Fatalf("Pop: %v, %d", err, len(popped))
	}

	popped[0].LastError = "the broker refused"
	before := time.Now().UTC()
	if err := popped[0].Release(context.Background(), time.Hour); err != nil {
		t.Fatalf("Release: %v", err)
	}

	args := state.argsFor("UPDATE jobs SET run_at = ?, last_error = ?, exceptions = ?, created_at = ?, reserved_until = NULL")
	if args == nil {
		t.Fatalf("Release issued %v", state.statements())
	}
	runAt, isTime := args[0].Value.(time.Time)
	if !isTime || runAt.Before(before.Add(time.Hour)) {
		t.Errorf("the job comes back at %v, want an hour out", args[0].Value)
	}
	// Stored rather than logged, because the thing anyone needs at 3am is "this
	// failed twelve times with this message".
	if got := args[1].Value; got != "the broker refused" {
		t.Errorf("last_error = %v", got)
	}
	if !popped[0].IsReleased() || popped[0].IsDeleted() {
		t.Errorf("released = %v, deleted = %v", popped[0].IsReleased(), popped[0].IsDeleted())
	}
}

func TestFailParksTheJob(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-1", "invoice.send", time.Now().Add(-time.Minute), 0, nil))

	popped, _ := q.Pop(context.Background(), "", 1, time.Minute)
	if err := popped[0].Fail(context.Background(), errors.New("the payload is malformed")); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	args := state.argsFor("UPDATE jobs SET failed_at = ?, last_error = ?, reserved_until = NULL")
	if args == nil {
		t.Fatalf("Fail issued %v", state.statements())
	}
	if got := args[1].Value; got != "the payload is malformed" {
		t.Errorf("last_error = %v", got)
	}
	// Parking a job settles it, and the worker must not settle it a second
	// time.
	if !popped[0].HasFailed() || !popped[0].IsDeletedOrReleased() {
		t.Error("a parked job does not report itself settled")
	}
}

func TestDeleteRemovesTheRow(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-1", "invoice.send", time.Now().Add(-time.Minute), 0, nil))

	popped, _ := q.Pop(context.Background(), "", 1, time.Minute)
	if err := popped[0].Delete(context.Background()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !state.sawStatement("DELETE FROM jobs WHERE id = ?") {
		t.Fatalf("Delete issued %v", state.statements())
	}
}

// TestARunningJobIsNotABacklog is a bug an audit found: a two-minute handler
// with a one-minute threshold made the health check answer 503 while the job
// ran, and a load balancer took the instance out of rotation for it.
func TestARunningJobIsNotABacklog(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT count(*)", []string{"count"}, []driver.Value{int64(0)})

	if _, err := q.PendingSize(context.Background(), ""); err != nil {
		t.Fatalf("PendingSize: %v", err)
	}
	if !state.sawStatement("WHERE queue = ? AND failed_at IS NULL AND (reserved_until IS NULL OR reserved_until < ?)") {
		t.Fatalf("PendingSize counts running jobs: %v", state.statements())
	}
}

func TestSizeCountsWhatIsOnTheQueue(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT count(*)", []string{"count"}, []driver.Value{int64(7)})

	size, err := q.Size(context.Background(), "mail")
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 7 {
		t.Errorf("size = %d", size)
	}
}

// TestClearLeavesTheParkedJobs: a job that gave up is no longer on a queue, it
// is in the dead letter list, and clearing the queue must not silently throw it
// away.
func TestClearLeavesTheParkedJobs(t *testing.T) {
	q, state := databaseQueue(t)
	state.setRowsAffected(3)

	removed, err := q.Clear(context.Background(), "mail")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if removed != 3 {
		t.Errorf("Clear removed %d", removed)
	}
	if !state.sawStatement("DELETE FROM jobs WHERE queue = ? AND failed_at IS NULL") {
		t.Fatalf("Clear issued %v", state.statements())
	}
}

func TestFailedListsTheMostRecentFirst(t *testing.T) {
	q, state := databaseQueue(t)
	state.answerWith("SELECT id, queue", jobColumns,
		jobRow("j-2", "report.monthly", time.Now(), 5, "the consumer was down"),
		jobRow("j-1", "invoice.send", time.Now(), 5, "the payload is malformed"))

	failed, err := q.Failed(context.Background(), 10)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}
	if len(failed) != 2 || failed[0].UUID != "j-2" {
		t.Fatalf("Failed returned %+v", failed)
	}
	if failed[0].LastError != "the consumer was down" {
		t.Errorf("the parked job does not say why: %q", failed[0].LastError)
	}
	if !state.sawStatement("WHERE failed_at IS NOT NULL ORDER BY failed_at DESC LIMIT ?") {
		t.Fatalf("Failed issued %v", state.statements())
	}
}

func TestRetryResetsTheAttempts(t *testing.T) {
	q, state := databaseQueue(t)
	if err := q.Retry(context.Background(), "j-1"); err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if !state.sawStatement("UPDATE jobs SET failed_at = NULL, attempts = 0, exceptions = 0, last_error = NULL, reserved_until = NULL, run_at = ?, created_at = ? WHERE id = ?") {
		t.Fatalf("Retry issued %v", state.statements())
	}
}

// TestTheModuleReportsABacklog: a stopped worker looks exactly like an idle
// one, and the lag is what tells them apart.
func TestTheModuleReportsABacklog(t *testing.T) {
	q := &fakeQueue{}
	ctx := context.Background()
	module := queue.NewModule(q)

	if err := module.Health(ctx); err != nil {
		t.Fatalf("an empty queue is unhealthy: %v", err)
	}
	if got := module.Diagnose(ctx); len(got) != 0 {
		t.Fatalf("an empty queue diagnosed %v", got)
	}

	// A job old enough to be a backlog.
	old, _ := jobs.New(grant(), "", "invoice.send", nil)
	old.RunAt = time.Now().Add(-10 * time.Minute)
	if err := q.Push(ctx, grant(), old); err != nil {
		t.Fatal(err)
	}

	if err := module.Health(ctx); err == nil {
		t.Fatal("a queue ten minutes behind is reported healthy")
	}
	diagnosis := module.Diagnose(ctx)
	if len(diagnosis) == 0 {
		t.Fatal("a backlog produced no diagnosis")
	}
	if !strings.Contains(diagnosis[0], "aru work") {
		t.Errorf("the diagnosis does not say what to check: %q", diagnosis[0])
	}
}

// TestTheModuleDeclaresNoSchemaForADriverThatOwnsNone: the jobs table belongs
// to the DatabaseQueue, and an application on RESP should not be migrating a
// table it will never read.
func TestTheModuleDeclaresNoSchemaForADriverThatOwnsNone(t *testing.T) {
	if got := queue.NewModule(queue.NullQueue{}).Migrations(); len(got) != 0 {
		t.Errorf("the NullQueue brought %d migrations", len(got))
	}
	// Three: the jobs table, the settings columns that were added to it once
	// the per-job attributes had somewhere to travel, and the created_at that
	// carries the order jobs are taken in.
	if got := queue.NewModule(queue.NewDatabaseQueue(nil)).Migrations(); len(got) != 3 {
		t.Errorf("the DatabaseQueue brought %d migrations, want the jobs table", len(got))
	}
}

// TestTheMigrationIsPortable: one migration, three engines.
func TestTheMigrationIsPortable(t *testing.T) {
	module := queue.NewModule(queue.NewDatabaseQueue(nil))
	for _, m := range module.Migrations() {
		for _, engineSpecific := range []string{"jsonb", "timestamptz", "SERIAL", "AUTO_INCREMENT", "SKIP LOCKED"} {
			if strings.Contains(m.Up, engineSpecific) {
				t.Errorf("%s uses %q, which is one engine's spelling", m.ID, engineSpecific)
			}
		}
		if m.Down == "" {
			t.Errorf("%s cannot be rolled back", m.ID)
		}
	}
	var _ foundation.Migratable = module
}
