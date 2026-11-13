package cache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/support"
)

const (
	// signInKeyPrefix is in front of every key the throttle writes, so that what
	// it keeps in a shared store can be told from the application's own entries,
	// and so that a change of layout has a version to move.
	signInKeyPrefix = "arandu:signin-throttle:v1:"

	// signInLockTTL is how long a lock on a counter lives if its holder dies. An
	// attempt holds two of them for a handful of round trips, so this is long
	// enough for a slow store and short enough that a crashed process does not
	// stop anyone from signing in for more than a moment.
	signInLockTTL = 2 * time.Second

	// signInLockWait is the default bound on waiting for the two locks of one
	// attempt. It is what turns a wedged lock into a refused attempt instead of
	// a form that hangs.
	signInLockWait = 750 * time.Millisecond

	// signInBusyRetryAfter is what a refusal for a lock that could not be taken
	// tells the caller to wait. The attempt was not counted, and a second is
	// about as long as the lock itself can be held.
	signInBusyRetryAfter = time.Second

	// signInRepairBound bounds a Refund or a Clear, which run even when the
	// request that called them has been cancelled.
	signInRepairBound = 3 * time.Second

	// signInWarnEvery is how often the throttle writes the warning that it is
	// counting in process memory, so an outage is one line a minute and not one
	// per sign-in.
	signInWarnEvery = time.Minute
)

// ThrottleStore is the part of a cache store the sign-in throttle needs: values
// that expire, and a lock that more than one process can contend for. The
// SharedStore of hesape/redis is one, and so are ArrayStore, FileStore and
// DatabaseStore.
type ThrottleStore interface {
	Store
	Locking
}

// errSignInBusy is the throttle's own: the locks of an attempt could not be
// taken inside the bound. It never leaves the package.
var errSignInBusy = errors.New("cache: the sign-in counters are busy")

// SignInThrottle is auth.SignInThrottle over a cache store, so the counts
// survive a restart and are the same on every replica.
//
//	throttle, err := cache.NewSignInThrottle(store, policy)
//
// It keeps the rules of auth.MemoryThrottle exactly, under the same policy:
// an attempt takes one unit from the identity's budget and one from the
// address's, or is refused; a refused attempt costs nothing and does not extend
// a lockout; Refund gives one unit of each back; Clear forgets the identity and
// gives the address one unit back; and with ThrottlePolicy.IdentityWide the
// identity's budget is shared by every address.
//
// # One indivisible step
//
// The decision and the record are one event, as the interface asks. Attempt
// takes the store's lock on each of the two counters, in the order their keys
// sort, which is the same on every replica and so cannot deadlock; reads both;
// decides; writes both with their expiry; and releases the locks by token. A
// burst of simultaneous guesses queues on the lock and is counted one at a time,
// which the concurrency test proves with two hundred of them against a budget of
// ten. The locks live seconds: if their holder dies, the next attempt waits at
// most that long.
//
// If the locks cannot be taken inside the wait (LockWait), that one attempt is
// refused with a short retry-after and counts for nothing: it fails closed and
// leaves the counters as they were.
//
// # What is in the store
//
// Two kinds of key, both under one fixed prefix, and both a hash: the tenant
// and the identity or the address go into a SHA-256 and out as base64, so no
// e-mail address and no IP address is readable in Redis, and the size of a key
// is not chosen by whoever typed the form. The tenant is in every hash. The
// identity is lower-cased and trimmed first, because the users table is
// case-insensitive on e-mail. Each counter carries its own deadline and the
// store's expiry, so nothing outlives its window.
//
// The deadlines are read against the clock of the process that reads them, so
// replicas are expected to keep time the way any deployment that shares a
// session store already does.
//
// # When the store fails
//
// A store that answers with an error does not switch the lockout off. The
// attempt is counted in an in-process auth.MemoryThrottle under the same policy
// -- which holds only what this process saw, but holds it -- and a warning goes
// to the logger of the context, at most once a minute. The next call asks the
// store again. Nothing panics and nothing is let through uncounted.
//
// Refund and Clear are repairs, and cancellation is the reason Refund is called
// at all: they finish under their own short bound even when the request's
// context is already done. Every other call honours its context, and a
// cancelled Attempt is refused and counts nothing.
type SignInThrottle struct {
	store    ThrottleStore
	locks    *Locks
	policy   auth.ThrottlePolicy
	fallback *auth.MemoryThrottle
	wait     time.Duration
	warned   atomic.Int64
}

var _ auth.SignInThrottle = (*SignInThrottle)(nil)

// NewSignInThrottle returns a sign-in throttle that counts in store under
// policy, or an error when there is no store or the policy is incomplete: both
// budgets and the window must be positive, and a lockout cannot be negative.
func NewSignInThrottle(store ThrottleStore, policy auth.ThrottlePolicy) (*SignInThrottle, error) {
	if store == nil {
		return nil, errors.New("cache: a sign-in throttle needs a store, and with none it would count nothing")
	}
	fallback, err := auth.NewMemoryThrottleWith(policy)
	if err != nil {
		return nil, err
	}
	return &SignInThrottle{
		store:    store,
		locks:    NewLocks(store),
		policy:   policy,
		fallback: fallback,
		wait:     signInLockWait,
	}, nil
}

// LockWait sets how long one call waits for the locks of its counters before it
// gives up. It is for the construction of the throttle, before any call; the
// default is 750 ms and a value of zero or less keeps it.
func (t *SignInThrottle) LockWait(d time.Duration) *SignInThrottle {
	if d > 0 {
		t.wait = d
	}
	return t
}

// Attempt takes one unit from the identity's budget and one from the address's,
// or refuses. A refusal because the budget is spent costs nothing and says how
// long is left; a refusal because the counters were busy or the context was
// done costs nothing either and says to try again in a moment.
func (t *SignInThrottle) Attempt(ctx context.Context, tenant, identity, client string) (time.Duration, bool) {
	if ctx.Err() != nil {
		return signInBusyRetryAfter, false
	}
	retry, ok, err := t.attempt(ctx, t.keysFor(tenant, identity, client))
	switch {
	case err == nil:
		return retry, ok
	case errors.Is(err, errSignInBusy), ctx.Err() != nil:
		return signInBusyRetryAfter, false
	}
	t.degrade(ctx, "attempt", err)
	return t.fallback.Attempt(ctx, tenant, identity, client)
}

// Refund gives back the unit Attempt took from each of the two budgets, for an
// attempt that never reached the credential. It runs to its end even when ctx
// is already cancelled.
func (t *SignInThrottle) Refund(ctx context.Context, tenant, identity, client string) {
	ctx, cancel := repairContext(ctx)
	defer cancel()

	err := t.repair(ctx, t.keysFor(tenant, identity, client), func(k signInKeys, now time.Time) error {
		for _, key := range []string{k.identity, k.client} {
			if err := t.giveBack(ctx, key, now); err != nil {
				return err
			}
		}
		return nil
	})
	switch {
	case err == nil:
	case errors.Is(err, errSignInBusy):
		t.warn(ctx, "refund", err)
	default:
		t.degrade(ctx, "refund", err)
		t.fallback.Refund(ctx, tenant, identity, client)
	}
}

// Clear forgets what the identity failed, from every address under IdentityWide
// and from this one otherwise, and gives the address back the unit this attempt
// took. It runs to its end even when ctx is already cancelled.
func (t *SignInThrottle) Clear(ctx context.Context, tenant, identity, client string) {
	ctx, cancel := repairContext(ctx)
	defer cancel()

	// What the process counted while the store was down is forgotten with the
	// rest.
	t.fallback.Clear(ctx, tenant, identity, client)

	err := t.repair(ctx, t.keysFor(tenant, identity, client), func(k signInKeys, now time.Time) error {
		if err := t.store.Forget(ctx, k.identity); err != nil {
			return err
		}
		return t.giveBack(ctx, k.client, now)
	})
	switch {
	case err == nil:
	case errors.Is(err, errSignInBusy):
		t.warn(ctx, "clear", err)
	default:
		t.degrade(ctx, "clear", err)
	}
}

// signInKeys are the two counters one attempt spends.
type signInKeys struct {
	identity string
	client   string
}

func (t *SignInThrottle) keysFor(tenant, identity, client string) signInKeys {
	identity = strings.ToLower(strings.TrimSpace(identity))
	if t.policy.IdentityWide {
		return signInKeys{identity: signInKey("i", tenant, identity), client: signInKey("c", tenant, client)}
	}
	return signInKeys{identity: signInKey("i", tenant, identity, client), client: signInKey("c", tenant, client)}
}

// signInKey is a key of the throttle: the prefix, the kind, and a hash of every
// field with its length in front, so that ("ab", "c") and ("a", "bc") are two
// keys.
func signInKey(kind string, fields ...string) string {
	h := sha256.New()
	var n [binary.MaxVarintLen64]byte
	for _, f := range fields {
		_, _ = h.Write(n[:binary.PutUvarint(n[:], uint64(len(f)))])
		_, _ = io.WriteString(h, f)
	}
	return signInKeyPrefix + kind + ":" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// counter is what is stored under a key: how many units are spent and when the
// count stops mattering.
type counter struct {
	count int
	reset time.Time
}

func (c counter) encode() []byte {
	return []byte(strconv.Itoa(c.count) + "|" + strconv.FormatInt(c.reset.UnixNano(), 10))
}

// read returns the live counter under key, or the zero counter when there is
// none, it has run out, or what is stored is not a counter: the next write puts
// a good one in its place. Only a store that failed is an error.
func (t *SignInThrottle) read(ctx context.Context, key string, now time.Time) (counter, error) {
	raw, err := t.store.Get(ctx, key)
	if errors.Is(err, ErrNotFound) {
		return counter{}, nil
	}
	if err != nil {
		return counter{}, err
	}
	count, deadline, found := strings.Cut(string(raw), "|")
	n, errCount := strconv.Atoi(count)
	nanos, errDeadline := strconv.ParseInt(deadline, 10, 64)
	if !found || errCount != nil || errDeadline != nil || n <= 0 {
		return counter{}, nil
	}
	c := counter{count: n, reset: time.Unix(0, nanos)}
	if !now.Before(c.reset) {
		return counter{}, nil
	}
	return c, nil
}

// write stores the counter for as long as it is live, which is never less than
// a millisecond.
func (t *SignInThrottle) write(ctx context.Context, key string, c counter, now time.Time) error {
	ttl := max(c.reset.Sub(now), time.Millisecond)
	return t.store.Put(ctx, key, c.encode(), ttl)
}

// spent answers, as MemoryThrottle does, whether the counter has reached limit
// and for how long it stays there.
func spent(c counter, limit int, now time.Time) (time.Duration, bool) {
	if c.count < limit || !now.Before(c.reset) {
		return 0, false
	}
	return c.reset.Sub(now), true
}

// bump adds one to a counter, opening a fresh window when there is none. The
// attempt that spends the budget starts the lockout, so the identity stays
// locked for the whole Lockout whatever was left of the window.
func (t *SignInThrottle) bump(c counter, limit int, now time.Time) counter {
	if c.count == 0 {
		c.reset = now.Add(t.policy.Window)
	}
	c.count++
	if c.count >= limit && t.policy.Lockout > 0 {
		c.reset = now.Add(t.policy.Lockout)
	}
	return c
}

// attempt is the indivisible step: lock both counters, read both, decide, write
// both, unlock.
func (t *SignInThrottle) attempt(ctx context.Context, k signInKeys) (time.Duration, bool, error) {
	release, err := t.lock(ctx, k)
	if err != nil {
		return 0, false, err
	}
	defer release()

	now := support.Now()
	identity, err := t.read(ctx, k.identity, now)
	if err != nil {
		return 0, false, err
	}
	client, err := t.read(ctx, k.client, now)
	if err != nil {
		return 0, false, err
	}

	// Both are asked before either is written, and the address's budget is
	// asked before the identity's counter is opened: once an address has spent
	// its own, everything it sends is refused until the window turns over, and a
	// counter written for it would record something nobody will ask about.
	if retry, over := spent(identity, t.policy.MaxFailures, now); over {
		return retry, false, nil
	}
	if retry, over := spent(client, t.policy.MaxFailuresPerClient, now); over {
		return retry, false, nil
	}

	identity = t.bump(identity, t.policy.MaxFailures, now)
	client = t.bump(client, t.policy.MaxFailuresPerClient, now)
	if err := t.write(ctx, k.identity, identity, now); err != nil {
		return 0, false, err
	}
	if err := t.write(ctx, k.client, client, now); err != nil {
		return 0, false, err
	}
	return 0, true, nil
}

// repair runs a change to both counters under their locks.
func (t *SignInThrottle) repair(ctx context.Context, k signInKeys, change func(signInKeys, time.Time) error) error {
	release, err := t.lock(ctx, k)
	if err != nil {
		return err
	}
	defer release()
	return change(k, support.Now())
}

// giveBack takes one off a counter and removes it when nothing is left, so a
// deployment whose sign-ins all succeed stores nothing.
func (t *SignInThrottle) giveBack(ctx context.Context, key string, now time.Time) error {
	c, err := t.read(ctx, key, now)
	if err != nil {
		return err
	}
	if c.count == 0 {
		return nil
	}
	if c.count--; c.count <= 0 {
		return t.store.Forget(ctx, key)
	}
	return t.write(ctx, key, c, now)
}

// lock takes the locks of both counters in the order their keys sort and
// returns the call that releases them. It gives up when LockWait has passed.
func (t *SignInThrottle) lock(ctx context.Context, k signInKeys) (func(), error) {
	names := []string{k.identity, k.client}
	sort.Strings(names)

	deadline := time.Now().Add(t.wait)
	var held []*Lock
	release := func() {
		// Whatever the request's context says, the locks go back: a lock left
		// behind is a sign-in form that waits for its expiry.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), signInRepairBound)
		defer cancel()
		for i := len(held) - 1; i >= 0; i-- {
			_ = held[i].Release(ctx)
		}
		held = nil
	}

	for _, name := range names {
		lock := t.locks.Lock(name, signInLockTTL)
		for {
			err := lock.Acquire(ctx)
			if err == nil {
				held = append(held, lock)
				break
			}
			if !errors.Is(err, ErrLocked) {
				release()
				return nil, err
			}
			if !time.Now().Before(deadline) {
				release()
				return nil, errSignInBusy
			}
			// A few milliseconds with jitter, so that two hundred waiters do not
			// come back in step.
			pause := time.NewTimer(2*time.Millisecond + rand.N(4*time.Millisecond))
			select {
			case <-ctx.Done():
				pause.Stop()
				release()
				return nil, ctx.Err()
			case <-pause.C:
			}
		}
	}
	return release, nil
}

// repairContext is the context a Refund or a Clear runs under: the values of the
// caller's, a bound of its own, and no cancellation.
func repairContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), signInRepairBound)
}

// degrade says the throttle is counting in process memory because the store
// failed. The attempt that follows is counted there; nothing is let through
// uncounted.
func (t *SignInThrottle) degrade(ctx context.Context, operation string, err error) {
	t.warn(ctx, operation, fmt.Errorf("the shared store failed, counting in process memory: %w", err))
}

// warn writes the line, at most once per signInWarnEvery.
func (t *SignInThrottle) warn(ctx context.Context, operation string, err error) {
	now := time.Now().UnixNano()
	last := t.warned.Load()
	if last != 0 && now-last < int64(signInWarnEvery) {
		return
	}
	if !t.warned.CompareAndSwap(last, now) {
		return
	}
	log.For(ctx).Warn("sign-in throttle", "operation", operation, "error", err)
}
