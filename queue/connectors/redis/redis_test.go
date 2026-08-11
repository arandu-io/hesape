package redis_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/connectors/redis"
	"github.com/arandu-io/hesape/queue/jobs"
)

// The RESP driver against a real server. Everything it claims is a claim about
// the protocol -- that ZRem is the claim and exactly one worker wins it -- and a
// fake that implements the happy path proves none of it.
//
// Without KV_ADDRESS these skip. CI sets it.

const tenant = "11111111-1111-4111-8111-111111111111"

func grant() auth.Grant { return auth.SystemGrant("invoice.send", tenant) }

func queue(t *testing.T) *redis.RedisQueue {
	t.Helper()

	address := os.Getenv("KV_ADDRESS")
	if address == "" {
		t.Skip("KV_ADDRESS is not set: start a RESP server and set it, e.g. KV_ADDRESS=127.0.0.1:6379")
	}

	// A prefix per test AND per run, or a second run inside the same minute
	// inherits the first one's jobs.
	prefix := "test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" +
		strconv.FormatInt(time.Now().UnixNano(), 36)
	q := redis.New(redis.Options{Address: address, Prefix: prefix})
	if err := q.Ping(context.Background()); err != nil {
		t.Fatalf("connecting to %s: %v", address, err)
	}
	t.Cleanup(func() { _ = q.Close() })
	return q
}

func push(t *testing.T, q *redis.RedisQueue, name string) jobs.Job {
	t.Helper()
	j, err := jobs.New(grant(), "", name, map[string]string{"n": name})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := q.Push(context.Background(), grant(), j); err != nil {
		t.Fatalf("Push: %v", err)
	}
	return j
}

func TestPushAndPop(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	pushed := push(t, q, "invoice.send")

	popped, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(popped) != 1 || popped[0].UUID != pushed.UUID {
		t.Fatalf("popped %+v", popped)
	}
	if popped[0].TenantID != tenant || popped[0].Action != "invoice.send" {
		t.Errorf("the Grant did not survive: %+v", popped[0])
	}
	if popped[0].Attempts != 1 {
		t.Errorf("attempts = %d on the first delivery", popped[0].Attempts)
	}
	if popped[0].Connection() != "redis" {
		t.Errorf("connection = %q", popped[0].Connection())
	}
}

// TestOnlyOneWorkerGetsTheJob is the claim that makes more than one worker
// safe, and it is about ZRem being atomic -- which only a real server settles.
func TestOnlyOneWorkerGetsTheJob(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")

	first, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil || len(first) != 1 {
		t.Fatalf("Pop: %v, %d", err, len(first))
	}
	second, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("a second worker got %d jobs that were already taken", len(second))
	}
}

// TestAnExpiredLeaseComesBack is what makes a crashed worker recoverable.
func TestAnExpiredLeaseComesBack(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")

	if _, err := q.Pop(ctx, "", 10, 50*time.Millisecond); err != nil {
		t.Fatalf("Pop: %v", err)
	}
	time.Sleep(120 * time.Millisecond)

	again, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(again) != 1 {
		t.Fatal("the job did not come back after the lease expired")
	}
	if again[0].Attempts != 2 {
		t.Errorf("attempts = %d on the second delivery", again[0].Attempts)
	}
}

func TestDeleteRemovesTheJob(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")

	popped, _ := q.Pop(ctx, "", 10, 50*time.Millisecond)
	if err := popped[0].Delete(ctx); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Past the lease: a deleted job never comes back.
	time.Sleep(120 * time.Millisecond)
	again, _ := q.Pop(ctx, "", 10, time.Minute)
	if len(again) != 0 {
		t.Fatal("a deleted job came back when its lease expired")
	}
	if pending, err := q.PendingSize(ctx, ""); err != nil || pending != 0 {
		t.Errorf("pending = %d (%v)", pending, err)
	}
}

func TestReleaseSchedulesTheRetry(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")

	popped, _ := q.Pop(ctx, "", 10, time.Minute)
	popped[0].LastError = "the broker refused"
	if err := popped[0].Release(ctx, time.Hour); err != nil {
		t.Fatalf("Release: %v", err)
	}

	soon, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(soon) != 0 {
		t.Fatal("a job scheduled for the future was popped now")
	}
	oldest, err := q.CreationTimeOfOldestPendingJob(ctx, "")
	if err != nil {
		t.Fatalf("CreationTimeOfOldestPendingJob: %v", err)
	}
	if !oldest.After(time.Now()) {
		t.Errorf("a job scheduled for the future reports %s, which reads as a backlog", oldest)
	}
}

func TestAParkedJobDoesNotBlockTheQueue(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")

	popped, _ := q.Pop(ctx, "", 10, time.Minute)
	if err := popped[0].Fail(ctx, errors.New("the payload is malformed")); err != nil {
		t.Fatalf("Fail: %v", err)
	}

	failed, err := q.Failed(ctx, 10)
	if err != nil || len(failed) != 1 {
		t.Fatalf("Failed: %v, %d", err, len(failed))
	}
	if failed[0].LastError != "the payload is malformed" {
		t.Errorf("the parked job does not say why: %q", failed[0].LastError)
	}

	push(t, q, "report.monthly")
	next, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil || len(next) != 1 || next[0].Name != "report.monthly" {
		t.Fatalf("the parked job blocked the queue: %+v (%v)", next, err)
	}
}

func TestRetryBringsAParkedJobBack(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")

	popped, _ := q.Pop(ctx, "", 10, time.Minute)
	_ = popped[0].Fail(ctx, errors.New("the consumer was down"))

	failed, _ := q.Failed(ctx, 10)
	if err := q.Retry(ctx, failed[0].UUID); err != nil {
		t.Fatalf("Retry: %v", err)
	}

	back, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil || len(back) != 1 {
		t.Fatalf("the retried job did not come back: %v, %d", err, len(back))
	}
	if back[0].Attempts != 1 {
		t.Errorf("attempts = %d after a retry, want the count to have restarted", back[0].Attempts)
	}
}

func TestQueuesAreSeparate(t *testing.T) {
	q := queue(t)
	ctx := context.Background()

	slow, _ := jobs.New(grant(), "reports", "report.monthly", nil)
	if err := q.Push(ctx, grant(), slow); err != nil {
		t.Fatal(err)
	}
	push(t, q, "invoice.send")

	def, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil || len(def) != 1 || def[0].Name != "invoice.send" {
		t.Fatalf("the default queue returned %+v (%v)", def, err)
	}
	reports, err := q.Pop(ctx, "reports", 10, time.Minute)
	if err != nil || len(reports) != 1 || reports[0].Name != "report.monthly" {
		t.Fatalf("the reports queue returned %+v (%v)", reports, err)
	}
}

// TestClearEmptiesTheQueue: `aru queue:clear` has to leave the queue countable
// at zero, bodies included, or the hash grows forever.
func TestClearEmptiesTheQueue(t *testing.T) {
	q := queue(t)
	ctx := context.Background()
	push(t, q, "invoice.send")
	push(t, q, "report.monthly")

	removed, err := q.Clear(ctx, "")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if removed != 2 {
		t.Errorf("Clear removed %d, want 2", removed)
	}
	if size, err := q.Size(ctx, ""); err != nil || size != 0 {
		t.Errorf("size = %d (%v)", size, err)
	}
}

func TestAJobWithoutATenantIsRefused(t *testing.T) {
	q := queue(t)

	err := q.Push(context.Background(), auth.SystemGrant("invoice.send", ""),
		jobs.Job{UUID: "j-1", Name: "invoice.send", Payload: []byte("{}")})
	if !errors.Is(err, jobs.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

// TestNothingUsesLuaOrModules: the moment it does, Dragonfly stops being a
// drop-in replacement and four products become one (RULE 11).
func TestNothingUsesLuaOrModules(t *testing.T) {
	source, err := os.ReadFile("redis.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{".Eval(", ".EvalSha(", ".JSONSet(", ".FT_"} {
		if strings.Contains(string(source), forbidden) {
			t.Errorf("the driver calls %s", forbidden)
		}
	}
}
