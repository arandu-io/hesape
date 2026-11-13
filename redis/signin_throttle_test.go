package redis_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/redis"
	"github.com/arandu-io/hesape/redis/connections"
)

// replica is one process of an application: its own connection and its own
// throttle, over the server and the key prefix every replica shares.
func replica(t *testing.T, prefix string, policy auth.ThrottlePolicy) (*cache.SignInThrottle, *redis.RedisStore) {
	t.Helper()

	database := 0
	if v := os.Getenv("REDIS_DATABASE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("REDIS_DATABASE=%q is not a number", v)
		}
		database = n
	}
	conn := connections.Connect(connections.Config{Address: address(t), Prefix: prefix, Database: database})
	if err := conn.Ping(context.Background()); err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	store := redis.NewRedisStore(conn)
	th, err := cache.NewSignInThrottle(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	return th, store
}

// sharedPrefix is per test and per run, as connect's is, and every key the
// throttle writes lives under it, so the cleanup removes only what the test
// made.
func sharedPrefix(t *testing.T) string {
	t.Helper()
	prefix := "throttle-" + strings.ReplaceAll(t.Name(), "/", "-") + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ":"
	return prefix
}

func forgetEverything(t *testing.T, store *redis.RedisStore) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		// Both flushes are scoped to the connection's prefix; neither is a
		// FLUSHDB.
		_ = store.Flush(ctx, "arandu:signin-throttle:")
		_ = store.Flush(ctx, "lock:arandu:signin-throttle:")
	})
}

func try(th auth.SignInThrottle, tenant, identity, client string) bool {
	_, ok := th.Attempt(context.Background(), tenant, identity, client)
	return ok
}

// TestSignInThrottleIsOneBudgetAcrossReplicas is what the in-process throttle
// cannot do: two processes, one count. Two hundred simultaneous guesses at one
// account, split between two replicas and sent from two hundred addresses, get
// exactly the ten the policy allows.
func TestSignInThrottleIsOneBudgetAcrossReplicas(t *testing.T) {
	policy := auth.LockoutPolicy()
	policy.IdentityWide = true
	policy.MaxFailuresPerClient = 1000

	prefix := sharedPrefix(t)
	one, storeOne := replica(t, prefix, policy)
	two, _ := replica(t, prefix, policy)
	forgetEverything(t, storeOne)
	one.LockWait(20 * time.Second)
	two.LockWait(20 * time.Second)

	var through atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 200; i++ {
		wg.Add(1)
		th := auth.SignInThrottle(one)
		if i%2 == 1 {
			th = two
		}
		go func() {
			defer wg.Done()
			<-start
			if try(th, "acme", "ana@example.com", fmt.Sprintf("ip:10.0.%d.%d", i/250, i%250)) {
				through.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := through.Load(); got != 10 {
		t.Fatalf("a budget of 10 let %d of 200 simultaneous attempts through across two replicas", got)
	}

	// The lockout is what a third replica, started now, sees.
	three, _ := replica(t, prefix, policy)
	retry, ok := three.Attempt(context.Background(), "acme", "ana@example.com", "ip:192.0.2.1")
	if ok || retry <= 29*time.Minute || retry > 30*time.Minute {
		t.Fatalf("a replica that joined later: ok=%v retry=%v, want the lockout of 30m", ok, retry)
	}
}

func TestSignInThrottleRulesAgainstARealServer(t *testing.T) {
	policy := auth.ThrottlePolicy{MaxFailures: 3, MaxFailuresPerClient: 5, Window: 2 * time.Second, IdentityWide: true}
	th, store := replica(t, sharedPrefix(t), policy)
	forgetEverything(t, store)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if !try(th, "acme", "ana@example.com", fmt.Sprintf("ip:198.51.100.%d", i)) {
			t.Fatalf("attempt %d was refused before the budget was spent", i+1)
		}
	}
	retry, ok := th.Attempt(ctx, "acme", "ana@example.com", "ip:203.0.113.1")
	if ok || retry <= 0 || retry > 2*time.Second {
		t.Fatalf("the fourth attempt: ok=%v retry=%v", ok, retry)
	}
	if !try(th, "globex", "ana@example.com", "ip:198.51.100.0") {
		t.Fatal("one tenant's budget reached another's")
	}

	th.Refund(ctx, "acme", "ana@example.com", "ip:198.51.100.0")
	if !try(th, "acme", "ana@example.com", "ip:203.0.113.1") {
		t.Fatal("a refund gave nothing back")
	}
	if try(th, "acme", "ana@example.com", "ip:203.0.113.1") {
		t.Fatal("a refund gave back more than one unit")
	}

	th.Clear(ctx, "acme", "ana@example.com", "ip:203.0.113.1")
	if !try(th, "acme", "ana@example.com", "ip:203.0.113.2") {
		t.Fatal("a successful sign-in did not forget the identity")
	}

	time.Sleep(2200 * time.Millisecond)
	for i := 0; i < 3; i++ {
		if !try(th, "acme", "bia@example.com", "ip:198.51.100.9") {
			t.Fatalf("attempt %d after the window was refused", i+1)
		}
	}
}

// TestSignInThrottleKeepsNothingReadableInTheServer: what an operator running
// KEYS or MONITOR on a production Redis sees must not be the people who are
// signing in.
func TestSignInThrottleKeepsNothingReadableInTheServer(t *testing.T) {
	policy := auth.LockoutPolicy()
	prefix := sharedPrefix(t)
	th, store := replica(t, prefix, policy)
	forgetEverything(t, store)

	th.Attempt(context.Background(), "acme", "ana@example.com", "ip:198.51.100.7")

	client := store.Connection().Client()
	keys, err := client.Keys(context.Background(), prefix+"*").Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("nothing was stored")
	}
	for _, key := range keys {
		for _, secret := range []string{"ana", "example.com", "198.51.100.7"} {
			if strings.Contains(key, secret) {
				t.Errorf("the key %q carries %q in clear", key, secret)
			}
		}
		if ttl := client.TTL(context.Background(), key).Val(); ttl <= 0 {
			t.Errorf("the key %q has no expiry (%v)", key, ttl)
		}
	}
}
