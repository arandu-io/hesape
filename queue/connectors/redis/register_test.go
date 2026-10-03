package redis_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	queuecore "github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/connectors/redis"
)

// TestImportingThePackageLinksTheQueue is the whole of what a blank import
// does, and the check a boot makes before it opens anything.
func TestImportingThePackageLinksTheQueue(t *testing.T) {
	if err := queuecore.Linked("QUEUE_CONNECTION", "redis"); err != nil {
		t.Errorf("queue.Linked(QUEUE_CONNECTION, redis) = %v, want nil once this package is imported", err)
	}
}

// TestOpenDialsNothing: a binary that runs a migration must not open a socket
// to the queue on the way past. Nothing listens on port 1, and Open succeeds
// anyway; the first command is what finds out.
func TestOpenDialsNothing(t *testing.T) {
	q, err := queuecore.Open("redis", cache.Endpoint{Address: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("Open dialled, or refused an endpoint it had no reason to: %v", err)
	}
	rq, ok := q.(*redis.RedisQueue)
	if !ok {
		t.Fatalf("Open returned a %T, want *redis.RedisQueue", q)
	}
	t.Cleanup(func() { _ = rq.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rq.Ping(ctx); err == nil {
		t.Error("Ping reported a server that is not there as reachable")
	}
}

// TestOpenRefusesAnEndpointWithNoAddress: an empty address read from the
// environment means nobody set it, and a worker popping from localhost instead
// would never see what the web replicas pushed.
func TestOpenRefusesAnEndpointWithNoAddress(t *testing.T) {
	if _, err := queuecore.Open("redis", cache.Endpoint{Prefix: "app"}); err == nil || !strings.Contains(err.Error(), "no address") {
		t.Errorf("Open = %v, want a refusal that says the address is missing", err)
	}
}

// TestAQueueOpenedThroughTheRegistryIsTheQueueBuiltByHand: the two wirings
// speak the same keys, so a job pushed by a replica on one is popped by a
// worker on the other -- which is what a rolling deploy between them is.
func TestAQueueOpenedThroughTheRegistryIsTheQueueBuiltByHand(t *testing.T) {
	address := os.Getenv("REDIS_ADDRESS")
	if address == "" {
		t.Skip("REDIS_ADDRESS is not set: start a RESP server and set it, e.g. REDIS_ADDRESS=127.0.0.1:6379")
	}
	prefix := "test-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx := context.Background()

	byHand := redis.New(redis.Options{Address: address, Prefix: prefix})
	t.Cleanup(func() { _ = byHand.Close() })
	opened, err := queuecore.Open("redis", cache.Endpoint{Address: address, Prefix: prefix})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	registered, ok := opened.(*redis.RedisQueue)
	if !ok {
		t.Fatalf("Open returned a %T, want *redis.RedisQueue", opened)
	}
	t.Cleanup(func() { _ = registered.Close() })

	sent := push(t, registered, "invoice.send")

	popped, err := byHand.Pop(ctx, "", 10, time.Minute)
	if err != nil {
		t.Fatalf("Pop: %v", err)
	}
	if len(popped) != 1 || popped[0].UUID != sent.UUID {
		t.Fatalf("the queue built by hand popped %+v, want the job pushed through the registry", popped)
	}

	back := push(t, byHand, "invoice.remind")
	if size, err := registered.Size(ctx, ""); err != nil || size != 2 {
		t.Errorf("the registered queue counts %d jobs (%v), want 2: the leased one and %s", size, err, back.UUID)
	}
}
