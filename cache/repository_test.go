package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
)

const action auth.Action = "cache.test"

func grantFor(tenant string) auth.Grant { return auth.SystemGrant(action, tenant) }

func newRepository() *cache.Repository { return cache.New(cache.NewArrayStore()) }

type profile struct {
	Name string    `json:"name"`
	Seen time.Time `json:"seen"`
}

func TestPutAndGet(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.Put(ctx, g, "name", "Ana", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cache.Get[string](ctx, r, g, "name")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "Ana" {
		t.Fatalf("Get = %q, want %q", got, "Ana")
	}
}

func TestGetOfAnAbsentKey(t *testing.T) {
	r := newRepository()
	if _, err := cache.Get[string](context.Background(), r, grantFor("acme"), "absent"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("Get = %v, want cache.ErrNotFound", err)
	}
}

// TestTenantsDoNotShareEntries is the reason the Grant is in every signature.
// The keys are the same string at the call site and different entries in the
// store, and if they were not, one customer would be served the other's answer.
func TestTenantsDoNotShareEntries(t *testing.T) {
	ctx := context.Background()
	r := newRepository()

	if err := r.Put(ctx, grantFor("acme"), "total", 10, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := cache.Get[int](ctx, r, grantFor("globex"), "total"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the other tenant read the entry: err = %v, want cache.ErrNotFound", err)
	}
}

func TestNamespacesDoNotShareEntries(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.Namespace("user").Put(ctx, g, "1", "Ana", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := cache.Get[string](ctx, r.Namespace("invoice"), g, "1"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the other namespace read the entry: err = %v, want cache.ErrNotFound", err)
	}
}

func TestNamespaceDoesNotMutateTheRepositoryItCameFrom(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	_ = r.Namespace("user")
	if err := r.Put(ctx, g, "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := cache.Get[string](ctx, r, g, "k"); err != nil {
		t.Fatalf("Get on the repository Namespace was derived from: %v", err)
	}
}

func TestAGrantWithNoTenantIsRefused(t *testing.T) {
	ctx := context.Background()
	r := newRepository()

	var nobody auth.Grant
	if err := r.Put(ctx, nobody, "k", "v", time.Minute); !errors.Is(err, cache.ErrNoTenant) {
		t.Fatalf("Put with no tenant = %v, want cache.ErrNoTenant", err)
	}
	if _, err := cache.Get[string](ctx, r, nobody, "k"); !errors.Is(err, cache.ErrNoTenant) {
		t.Fatalf("Get with no tenant = %v, want cache.ErrNoTenant", err)
	}
}

// allowAll is a policy that authorizes anything, so a test can hold a Grant
// carrying a tenant SystemGrant would have refused.
type allowAll struct{}

func (allowAll) Can(context.Context, auth.Subject, auth.Action, struct{}) error { return nil }

// TestATenantThatCouldNameAnotherKeyIsRefused is the audit finding, in the
// cache: tenant "acme:user" with namespace "session" builds the same string as
// tenant "acme" with namespace "user" and a key beginning "session:". No policy
// is violated to get here -- the key is built after the policy has said yes.
func TestATenantThatCouldNameAnotherKeyIsRefused(t *testing.T) {
	ctx := context.Background()
	r := newRepository()

	g, err := auth.Authorize(ctx, allowAll{}, auth.Subject{ID: "1", Tenant: "acme:user"}, action, struct{}{})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	if err := r.Put(ctx, g, "k", "v", time.Minute); !errors.Is(err, cache.ErrNoTenant) {
		t.Fatalf("Put with a tenant carrying a colon = %v, want cache.ErrNoTenant", err)
	}
}

func TestANamespaceThatCouldNameAnotherKeyIsRefused(t *testing.T) {
	r := newRepository().Namespace("user:1")

	if err := r.Put(context.Background(), grantFor("acme"), "k", "v", time.Minute); err == nil {
		t.Fatal("Put under a namespace carrying a colon = nil, want an error")
	}
}

// TestPutWithATTLThatHasPassedForgetsTheKey is Repository::put(): "if
// ($seconds <= 0) return $this->forget($key)".
//
// The input is the one the audit ran: put a value for a minute, then put
// another with no ttl at all. This returned ErrNoTTL and wrote nothing, so the
// key went on reading "old" -- the caller told the cache to stop serving a
// value and the cache kept serving it.
func TestPutWithATTLThatHasPassedForgetsTheKey(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.Put(ctx, g, "k", "old", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := r.Put(ctx, g, "k", "new", 0); err != nil {
		t.Fatalf("Put with no ttl = %v, want the key forgotten", err)
	}
	if _, err := cache.Get[string](ctx, r, g, "k"); !errors.Is(err, cache.ErrNotFound) {
		got, _ := cache.Get[string](ctx, r, g, "k")
		t.Fatalf("the key still reads %q, want it forgotten", got)
	}

	// A negative ttl is the same statement said differently: the entry expired
	// before it was written.
	if err := r.Put(ctx, g, "k", "old", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := r.Put(ctx, g, "k", "new", -time.Hour); err != nil {
		t.Fatalf("Put with a negative ttl = %v", err)
	}
	if has, err := r.Has(ctx, g, "k"); err != nil || has {
		t.Fatalf("has = %v, err = %v, want the key forgotten", has, err)
	}
}

// TestPutManyWithATTLThatHasPassedForgetsTheKeys is the same line of
// Repository::putMany(): "if ($seconds <= 0) return
// $this->deleteMultiple(array_keys($values))".
func TestPutManyWithATTLThatHasPassedForgetsTheKeys(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.PutMany(ctx, g, map[string]any{"a": 1, "b": 2}, time.Minute); err != nil {
		t.Fatalf("PutMany: %v", err)
	}
	if err := r.PutMany(ctx, g, map[string]any{"a": 3, "b": 4}, 0); err != nil {
		t.Fatalf("PutMany with no ttl = %v, want the keys forgotten", err)
	}
	for _, key := range []string{"a", "b"} {
		if has, err := r.Has(ctx, g, key); err != nil || has {
			t.Fatalf("%q: has = %v, err = %v, want the key forgotten", key, has, err)
		}
	}
}

// TestAddWithATTLThatHasPassedAddsNothing is Repository::add(): "if ($seconds
// <= 0) return false". Nothing is written, and the caller is told it did not
// win -- which is what every other losing Add is told.
func TestAddWithATTLThatHasPassedAddsNothing(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	added, err := r.Add(ctx, g, "leader", "one", 0)
	if err != nil {
		t.Fatalf("Add with no ttl = %v, want false and no error", err)
	}
	if added {
		t.Fatal("Add with no ttl reported that it wrote")
	}
	if has, err := r.Has(ctx, g, "leader"); err != nil || has {
		t.Fatalf("has = %v, err = %v, want nothing written", has, err)
	}
}

// TestACounterNeedsATTL is the one of the three that keeps its error, and the
// reason is in Repository.Increment's doc comment: Repository::increment() has
// no ttl argument to copy a decision from.
func TestACounterNeedsATTL(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if _, err := r.Increment(ctx, g, "n", 1, 0); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("Increment with no ttl = %v, want cache.ErrNoTTL", err)
	}
}

func TestAnEmptyKeyIsRefused(t *testing.T) {
	r := newRepository()
	if err := r.Put(context.Background(), grantFor("acme"), "", "v", time.Minute); err == nil {
		t.Fatal("Put under an empty key = nil, want an error")
	}
}

func TestAddOnlyWritesOnce(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	first, err := r.Add(ctx, g, "leader", "one", time.Minute)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := r.Add(ctx, g, "leader", "two", time.Minute)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !first || second {
		t.Fatalf("Add = %v then %v, want true then false", first, second)
	}

	got, err := cache.Get[string](ctx, r, g, "leader")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "one" {
		t.Fatalf("Get = %q, want %q: the losing Add must not write", got, "one")
	}
}

func TestHasAndForget(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.Put(ctx, g, "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	has, err := r.Has(ctx, g, "k")
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if !has {
		t.Fatal("Has after Put = false, want true")
	}

	if err := r.Forget(ctx, g, "k"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	has, err = r.Has(ctx, g, "k")
	if err != nil {
		t.Fatalf("Has: %v", err)
	}
	if has {
		t.Fatal("Has after Forget = true, want false")
	}
}

func TestIncrementIsReadableAsANumber(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if _, err := r.Increment(ctx, g, "views", 3, time.Minute); err != nil {
		t.Fatalf("Increment: %v", err)
	}
	n, err := r.Increment(ctx, g, "views", 1, time.Minute)
	if err != nil {
		t.Fatalf("Increment: %v", err)
	}
	if n != 4 {
		t.Fatalf("Increment = %d, want 4", n)
	}

	// The codec claim in Store.Increment: decimal text is what json makes of an
	// integer, so a counter reads back through Get without a second codec.
	got, err := cache.Get[int64](ctx, r, g, "views")
	if err != nil {
		t.Fatalf("Get of a counter: %v", err)
	}
	if got != 4 {
		t.Fatalf("Get of a counter = %d, want 4", got)
	}
}

// TestFlushIsOneTenantOfOneNamespace is the property that keeps a support
// request from being an outage.
func TestFlushIsOneTenantOfOneNamespace(t *testing.T) {
	ctx := context.Background()
	store := cache.NewArrayStore()
	users := cache.New(store).Namespace("user")
	invoices := cache.New(store).Namespace("invoice")

	acme, globex := grantFor("acme"), grantFor("globex")
	for _, put := range []struct {
		r *cache.Repository
		g auth.Grant
	}{{users, acme}, {users, globex}, {invoices, acme}} {
		if err := put.r.Put(ctx, put.g, "1", "v", time.Minute); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	if err := users.Flush(ctx, acme); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if _, err := cache.Get[string](ctx, users, acme, "1"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the flushed entry survived: err = %v", err)
	}
	if _, err := cache.Get[string](ctx, users, globex, "1"); err != nil {
		t.Fatalf("another tenant lost its entry: %v", err)
	}
	if _, err := cache.Get[string](ctx, invoices, acme, "1"); err != nil {
		t.Fatalf("another namespace lost its entry: %v", err)
	}
}

func TestPutManyAndMany(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.PutMany(ctx, g, map[string]any{"a": 1, "b": 2}, time.Minute); err != nil {
		t.Fatalf("PutMany: %v", err)
	}

	got, err := cache.Many[int](ctx, r, g, "a", "b", "c")
	if err != nil {
		t.Fatalf("Many: %v", err)
	}
	if len(got) != 3 || got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("Many = %v, want a=1, b=2 and an entry for every key asked for", got)
	}

	// Repository::many maps over the store's answer, which carries one entry per
	// requested key with null for the misses, so every key asked for comes back.
	// The miss is the zero value of T here, which is as close as a typed map
	// gets to null -- and the reason a caller who must tell a miss from a cached
	// zero reaches for Get instead.
	if _, ok := got["c"]; !ok {
		t.Fatal("Many dropped the key that was not cached; Illuminate returns it as null")
	}
	if got["c"] != 0 {
		t.Fatalf("Many[%q] = %v, want the zero value that stands in for null", "c", got["c"])
	}
}

func TestPullReadsOnce(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	if err := r.Put(ctx, g, "token", "abc", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := cache.Pull[string](ctx, r, g, "token")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if got != "abc" {
		t.Fatalf("Pull = %q, want %q", got, "abc")
	}

	if _, err := cache.Pull[string](ctx, r, g, "token"); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("the second Pull = %v, want cache.ErrNotFound", err)
	}
}

func TestRememberComputesOnceAndThenReadsTheCache(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	calls := 0
	compute := func(context.Context) (profile, error) {
		// time.Now carries a monotonic reading that json cannot represent, so a
		// value handed straight back from the miss is NOT equal to the same
		// value read from the cache. That inequality is what this test is
		// looking for: it is the difference the round trip through the codec
		// exists to remove.
		calls++
		return profile{Name: "Ana", Seen: time.Now()}, nil
	}

	first, err := cache.Remember(ctx, r, g, "profile", time.Minute, compute)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	second, err := cache.Remember(ctx, r, g, "profile", time.Minute, compute)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	if calls != 1 {
		t.Fatalf("compute ran %d times, want 1", calls)
	}
	if first != second {
		t.Fatalf("the miss returned %+v and the hit %+v: they have to be the same value, or the first request after a deploy behaves differently from the rest", first, second)
	}
}

// TestRememberCachesAComputationThatCameBackWithNothing pins the one place
// this Remember answers differently from Repository::remember(), so that the
// difference is a decision somebody can read rather than an accident.
//
// The PHP asks "! is_null($value)", so a stored null is a miss and the callback
// runs again on every request. Here a miss is ErrNotFound and a stored null is
// a value, so a callback that comes back with nothing runs once. The whole
// reason ErrNotFound exists in this package is to keep "not cached" and "cached
// as nothing" apart -- see Repository.Has -- and Remember reading them as one
// thing again would be the second meaning of null this package removed.
//
// The consequence is negative caching, which is what a caller usually wants:
// "this customer has no plan" is exactly the answer that costs a query to
// produce and is asked for on every request.
func TestRememberCachesAComputationThatCameBackWithNothing(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	calls := 0
	compute := func(context.Context) (*profile, error) {
		calls++
		return nil, nil
	}

	for range 2 {
		got, err := cache.Remember(ctx, r, g, "plan", time.Minute, compute)
		if err != nil {
			t.Fatalf("Remember: %v", err)
		}
		if got != nil {
			t.Fatalf("Remember = %+v, want nothing", got)
		}
	}
	if calls != 1 {
		t.Fatalf("compute ran %d times, want 1: a cached nothing is a hit here, and the PHP would have run it twice", calls)
	}
}

func TestRememberReportsWhatTheComputationReported(t *testing.T) {
	ctx := context.Background()
	r := newRepository()
	g := grantFor("acme")

	want := errors.New("the database said no")
	if _, err := cache.Remember(ctx, r, g, "k", time.Minute, func(context.Context) (int, error) {
		return 0, want
	}); !errors.Is(err, want) {
		t.Fatalf("Remember = %v, want the computation's error", err)
	}

	if has, err := r.Has(ctx, g, "k"); err != nil || has {
		t.Fatalf("a failed computation was cached: has = %v, err = %v", has, err)
	}
}

// TestRememberRefusesBeforeItComputes is the guard against the failure mode
// that looks like a slow database: a Remember whose arguments are wrong must
// say so, not quietly become a function that computes every time.
func TestRememberRefusesBeforeItComputes(t *testing.T) {
	ctx := context.Background()
	r := newRepository()

	calls := 0
	compute := func(context.Context) (int, error) {
		calls++
		return 1, nil
	}

	var nobody auth.Grant
	if _, err := cache.Remember(ctx, r, nobody, "k", time.Minute, compute); !errors.Is(err, cache.ErrNoTenant) {
		t.Fatalf("Remember with no tenant = %v, want cache.ErrNoTenant", err)
	}
	if _, err := cache.Remember(ctx, r, grantFor("acme"), "k", 0, compute); !errors.Is(err, cache.ErrNoTTL) {
		t.Fatalf("Remember with no ttl = %v, want cache.ErrNoTTL", err)
	}
	if calls != 0 {
		t.Fatalf("compute ran %d times on a call that was refused, want 0", calls)
	}
}

// TestRememberAnswersWhenTheStoreIsDown is the deliberate swallow: the cost of
// a store that is down is load, and the alternative is an outage.
func TestRememberAnswersWhenTheStoreIsDown(t *testing.T) {
	ctx := context.Background()
	r := cache.New(brokenStore{})
	g := grantFor("acme")

	got, err := cache.Remember(ctx, r, g, "k", time.Minute, func(context.Context) (int, error) {
		return 7, nil
	})
	if err != nil {
		t.Fatalf("Remember against a store that is down: %v", err)
	}
	if got != 7 {
		t.Fatalf("Remember = %d, want 7", got)
	}
}

// brokenStore is every backend that is unreachable.
type brokenStore struct{}

var errDown = errors.New("the store is unreachable")

func (brokenStore) Get(context.Context, string) ([]byte, error) { return nil, errDown }

func (brokenStore) Put(context.Context, string, []byte, time.Duration) error { return errDown }

func (brokenStore) Add(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, errDown
}

func (brokenStore) Forget(context.Context, string) error { return errDown }

func (brokenStore) Increment(context.Context, string, int64, time.Duration) (int64, error) {
	return 0, errDown
}

func (brokenStore) Flush(context.Context, string) error { return errDown }
