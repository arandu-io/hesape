package redis_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/arandu-io/hesape/queue/connectors/redis"
	"github.com/arandu-io/hesape/redis/connections"
)

// TestFromConnectionQueuesOverTheSharedClient: the queue built from the
// connection the cache already holds writes under that connection's prefix,
// through that connection's client -- one pool, one TLS configuration.
func TestFromConnectionQueuesOverTheSharedClient(t *testing.T) {
	address := os.Getenv("REDIS_ADDRESS")
	if address == "" {
		t.Skip("REDIS_ADDRESS is not set: start a RESP server and set it, e.g. REDIS_ADDRESS=127.0.0.1:6379")
	}
	prefix := "test-from-connection-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	conn := connections.Connect(connections.Config{Address: address, Prefix: prefix})
	t.Cleanup(func() { _ = conn.Close() })

	q, err := redis.FromConnection(conn)
	if err != nil {
		t.Fatalf("FromConnection: %v", err)
	}
	ctx := context.Background()
	pushed := push(t, q, "invoice.send")

	stored, err := conn.Client().HExists(ctx, conn.Key("q:jobs"), pushed.UUID).Result()
	if err != nil || !stored {
		t.Fatalf("the job is not under the connection's prefix: stored=%v err=%v", stored, err)
	}
	popped, err := q.Pop(ctx, "", 10, time.Minute)
	if err != nil || len(popped) != 1 || popped[0].UUID != pushed.UUID {
		t.Fatalf("Pop = %+v, %v", popped, err)
	}
	if _, err := q.Clear(ctx, pushed.Queue); err != nil {
		t.Fatalf("Clear: %v", err)
	}
}

// TestFromConnectionRefusesACluster: a claim moves a job between keys in
// different slots inside one transaction, which a cluster refuses. The
// refusal belongs at boot, not at the first job.
func TestFromConnectionRefusesACluster(t *testing.T) {
	conn := connections.Connect(connections.Config{Addresses: []string{"127.0.0.1:1", "127.0.0.1:2"}})
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := redis.FromConnection(conn); !errors.Is(err, redis.ErrClustered) {
		t.Fatalf("err = %v, want ErrClustered", err)
	}
}
