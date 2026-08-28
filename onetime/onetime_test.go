package onetime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/onetime"
)

// appKey is a thirty-two byte application key. Its content does not matter to
// any test here; its length does, because New refuses a shorter one.
var appKey = []byte("0123456789abcdef0123456789abcdef")

// clock is the frozen time the tests run at.
//
// Frozen rather than slept through: every deadline in this package is minutes
// long, and a test that waited them out would take minutes. It is guarded by a
// mutex because one test reads it from several goroutines at once.
type clock struct {
	mu sync.Mutex
	at time.Time
}

func newClock() *clock {
	return &clock{at: time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)}
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// hookedStore is an ArrayStore with seams in it.
//
// Every seam exists for one property: an error on a single method proves that
// the method failing refuses the attempt rather than allowing it, and
// forgetIsNoOp proves which of the two writes in Consume is the one that makes
// a code spent.
type hookedStore struct {
	inner cache.Store

	mu       sync.Mutex
	putTTLs  map[string]time.Duration
	addTTLs  map[string]time.Duration
	forghits int

	getErr       error
	putErr       error
	addErr       error
	incrementErr error

	forgetIsNoOp bool

	// afterGet runs once the inner Get has answered. It is what lets several
	// goroutines be held together at the point where they have all read the same
	// record and none has spent it yet.
	afterGet func(key string)
}

func newHooked() *hookedStore {
	return &hookedStore{
		inner:   cache.NewArrayStore(),
		putTTLs: map[string]time.Duration{},
		addTTLs: map[string]time.Duration{},
	}
}

func (s *hookedStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	value, err := s.inner.Get(ctx, key)
	if s.afterGet != nil {
		s.afterGet(key)
	}
	return value, err
}

func (s *hookedStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if s.putErr != nil {
		return s.putErr
	}
	s.mu.Lock()
	s.putTTLs[key] = ttl
	s.mu.Unlock()
	return s.inner.Put(ctx, key, value, ttl)
}

func (s *hookedStore) Add(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if s.addErr != nil {
		return false, s.addErr
	}
	s.mu.Lock()
	s.addTTLs[key] = ttl
	s.mu.Unlock()
	return s.inner.Add(ctx, key, value, ttl)
}

func (s *hookedStore) Forget(ctx context.Context, key string) error {
	s.mu.Lock()
	s.forghits++
	noop := s.forgetIsNoOp
	s.mu.Unlock()
	if noop {
		return errors.New("this store cannot remove anything")
	}
	return s.inner.Forget(ctx, key)
}

func (s *hookedStore) Increment(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	if s.incrementErr != nil {
		return 0, s.incrementErr
	}
	return s.inner.Increment(ctx, key, delta, ttl)
}

func (s *hookedStore) Flush(ctx context.Context, prefix string) error {
	return s.inner.Flush(ctx, prefix)
}

var _ cache.Store = (*hookedStore)(nil)

// expiringStore is an ArrayStore that expires its entries against the test
// clock instead of the wall clock.
//
// It is here because the tests are frozen in time and every deadline in this
// package is minutes long. An ArrayStore goes on holding an entry the test
// believes was reclaimed an hour ago, so a test of what happens once an entry
// has gone would pass without the entry ever going -- which is the difference
// between proving the ttl is written and assuming it.
type expiringStore struct {
	inner cache.Store
	now   func() time.Time

	mu       sync.Mutex
	deadline map[string]time.Time
}

func newExpiring(clk *clock) *expiringStore {
	return &expiringStore{
		inner:    cache.NewArrayStore(),
		now:      clk.now,
		deadline: map[string]time.Time{},
	}
}

// sweep removes key if the test clock has passed its deadline.
func (s *expiringStore) sweep(ctx context.Context, key string) {
	s.mu.Lock()
	deadline, known := s.deadline[key]
	s.mu.Unlock()

	if !known || s.now().Before(deadline) {
		return
	}
	_ = s.inner.Forget(ctx, key)
	s.mu.Lock()
	delete(s.deadline, key)
	s.mu.Unlock()
}

func (s *expiringStore) note(key string, ttl time.Duration, keepExisting bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if keepExisting {
		if _, known := s.deadline[key]; known {
			return
		}
	}
	s.deadline[key] = s.now().Add(ttl)
}

func (s *expiringStore) Get(ctx context.Context, key string) ([]byte, error) {
	s.sweep(ctx, key)
	return s.inner.Get(ctx, key)
}

func (s *expiringStore) Put(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.sweep(ctx, key)
	s.note(key, ttl, false)
	return s.inner.Put(ctx, key, value, ttl)
}

func (s *expiringStore) Add(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	s.sweep(ctx, key)
	added, err := s.inner.Add(ctx, key, value, ttl)
	if added {
		s.note(key, ttl, false)
	}
	return added, err
}

func (s *expiringStore) Forget(ctx context.Context, key string) error {
	s.mu.Lock()
	delete(s.deadline, key)
	s.mu.Unlock()
	return s.inner.Forget(ctx, key)
}

// Increment keeps whatever deadline the key already had, which is what the
// Store contract says: an expiry refreshed on every hit is a window that never
// closes.
func (s *expiringStore) Increment(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	s.sweep(ctx, key)
	s.note(key, ttl, true)
	return s.inner.Increment(ctx, key, delta, ttl)
}

func (s *expiringStore) Flush(ctx context.Context, prefix string) error {
	return s.inner.Flush(ctx, prefix)
}

var _ cache.Store = (*expiringStore)(nil)

// codesOver builds a Codes over store, on clk, with cfg's remaining fields.
func codesOver(t *testing.T, store cache.Store, clk *clock, cfg onetime.Config) *onetime.Codes {
	t.Helper()

	cfg.Now = clk.now
	codes, err := onetime.New(store, appKey, cfg)
	if err != nil {
		t.Fatalf("building the codes: %v", err)
	}
	return codes
}

func TestAnIssuedCodeIsSixDigitsAndIsAccepted(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if len(code) != onetime.CodeLength {
		t.Errorf("the code is %q, %d characters, want %d", code, len(code), onetime.CodeLength)
	}
	if strings.Trim(code, "0123456789") != "" {
		t.Errorf("the code is %q, and it has something other than a decimal digit in it", code)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Errorf("Consume of the code just issued = %v, want nil", err)
	}
}

func TestAnotherPurposeDoesNotAcceptTheCode(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := codes.Consume(ctx, "delete-account", "person-1", code); err == nil {
		t.Fatal("a code issued to confirm an address deleted an account")
	} else if !errors.Is(err, onetime.ErrNoCode) && !errors.Is(err, onetime.ErrInvalidCode) {
		t.Errorf("Consume under another purpose = %v, want a refusal", err)
	}

	// And the code is still the code it was issued for: a refusal under the
	// wrong purpose must not have spent it under the right one.
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Errorf("Consume under the right purpose = %v, want nil", err)
	}
}

func TestAnotherSubjectDoesNotAcceptTheCode(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	if err := codes.Consume(ctx, "confirm-address", "person-2", code); err == nil {
		t.Fatal("one person's code was accepted for another person")
	} else if !errors.Is(err, onetime.ErrNoCode) && !errors.Is(err, onetime.ErrInvalidCode) {
		t.Errorf("Consume as another subject = %v, want a refusal", err)
	}

	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Errorf("Consume as the right subject = %v, want nil", err)
	}
}

// TestASecondSubjectWithACodeOfItsOwnDoesNotAcceptTheFirstOne is the case the
// test above cannot see: with only one code outstanding, refusing the second
// subject needs nothing more than the lookup finding nothing. Here both subjects
// have a code, so the lookup finds a record either way and the refusal has to
// come from the code being bound to whom it was issued to.
func TestASecondSubjectWithACodeOfItsOwnDoesNotAcceptTheFirstOne(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	first, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue for the first person: %v", err)
	}
	second, err := codes.Issue(ctx, "confirm-address", "person-2")
	if err != nil {
		t.Fatalf("Issue for the second person: %v", err)
	}
	if first == second {
		t.Fatal("two people were issued the same code, and this test cannot tell them apart")
	}

	if err := codes.Consume(ctx, "confirm-address", "person-2", first); !errors.Is(err, onetime.ErrInvalidCode) {
		t.Errorf("Consume of the first person's code as the second = %v, want ErrInvalidCode", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-2", second); err != nil {
		t.Errorf("Consume of the second person's own code = %v, want nil", err)
	}
}

// TestTwoPurposesWithACodeEachDoNotAcceptEachOther is the same argument for the
// purpose: both purposes have a record, so the refusal cannot come from an empty
// lookup.
func TestTwoPurposesWithACodeEachDoNotAcceptEachOther(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	confirm, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue to confirm: %v", err)
	}
	remove, err := codes.Issue(ctx, "delete-account", "person-1")
	if err != nil {
		t.Fatalf("Issue to delete: %v", err)
	}
	if confirm == remove {
		t.Fatal("both purposes were issued the same code, and this test cannot tell them apart")
	}

	if err := codes.Consume(ctx, "delete-account", "person-1", confirm); !errors.Is(err, onetime.ErrInvalidCode) {
		t.Errorf("Consume of the confirmation code against the deletion = %v, want ErrInvalidCode", err)
	}
	if err := codes.Consume(ctx, "delete-account", "person-1", remove); err != nil {
		t.Errorf("Consume of the deletion code = %v, want nil", err)
	}
}

func TestAConsumedCodeDoesNotWorkTwice(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Fatalf("the first Consume: %v", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); !errors.Is(err, onetime.ErrNoCode) {
		t.Errorf("the second Consume = %v, want ErrNoCode", err)
	}
}

// TestTheCodeIsSpentEvenWhenTheRecordCannotBeRemoved separates the burn from the
// tidying up.
//
// Consume writes a marker and then removes the record. Only the first of those
// is what makes a code spent, and the difference is invisible while both
// succeed. With a store that refuses to remove anything, the record survives the
// first Consume, so the second one reads it, matches the code and gets as far as
// the marker -- which is the only thing standing between one use and two.
func TestTheCodeIsSpentEvenWhenTheRecordCannotBeRemoved(t *testing.T) {
	store := newHooked()
	store.forgetIsNoOp = true

	clk := newClock()
	codes := codesOver(t, store, clk, onetime.Config{})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Fatalf("the first Consume: %v", err)
	}
	if store.forghits == 0 {
		t.Fatal("Forget was never called, so this test is not exercising the case it describes")
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); !errors.Is(err, onetime.ErrNoCode) {
		t.Errorf("the second Consume against a record that was never removed = %v, want ErrNoCode", err)
	}
}

// TestOneOfManySimultaneousConsumersWins is the test the atomicity claim stands
// or falls on.
//
// Four goroutines present the same correct code, and the store holds every one
// of them at the moment it has read the record and before any of them has
// written anything. From there, the only thing that can order them is the
// compare-and-set the burn is made of; a read-then-write would let all four
// through.
func TestOneOfManySimultaneousConsumersWins(t *testing.T) {
	const consumers = 4

	store := newHooked()
	var arrived sync.WaitGroup
	arrived.Add(consumers)

	clk := newClock()
	codes := codesOver(t, store, clk, onetime.Config{MaxAttempts: consumers})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Only from here on: Issue reads the record too, and holding it would
	// deadlock before the test began.
	store.afterGet = func(key string) {
		if strings.HasPrefix(key, "onetime:code:") {
			arrived.Done()
			arrived.Wait()
		}
	}

	results := make([]error, consumers)
	var running sync.WaitGroup
	running.Add(consumers)
	for i := range consumers {
		go func() {
			defer running.Done()
			results[i] = codes.Consume(ctx, "confirm-address", "person-1", code)
		}()
	}
	running.Wait()

	won := 0
	for i, err := range results {
		switch {
		case err == nil:
			won++
		case errors.Is(err, onetime.ErrNoCode):
			// The ordinary answer for the ones that lost.
		default:
			t.Errorf("consumer %d = %v, want nil or ErrNoCode", i, err)
		}
	}
	if won != 1 {
		t.Errorf("%d of %d simultaneous consumers were told the code was theirs, want exactly 1", won, consumers)
	}
}

func TestAnExpiredCodeIsRefused(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{TTL: 10 * time.Minute})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	clk.advance(10*time.Minute - time.Second)
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Fatalf("Consume a second before the ttl runs out = %v, want nil", err)
	}

	next, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("the second Issue: %v", err)
	}
	clk.advance(10*time.Minute + time.Second)
	if err := codes.Consume(ctx, "confirm-address", "person-1", next); !errors.Is(err, onetime.ErrExpired) {
		t.Errorf("Consume a second after the ttl ran out = %v, want ErrExpired", err)
	}
}

// TestAnExpiredCodeIsRefusedOnceTheEntryHasGoneToo is the same refusal arriving
// under its other name.
//
// In the ordinary configuration the code and the entry holding it die together,
// so by the time anybody types the code there is nothing left to compare it
// against and the answer is ErrNoCode rather than ErrExpired. Both are
// refusals, which is why the two must read the same on the screen; the test is
// here so that the difference is written down rather than discovered.
func TestAnExpiredCodeIsRefusedOnceTheEntryHasGoneToo(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, newExpiring(clk), clk, onetime.Config{TTL: time.Minute, Cooldown: time.Minute})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	clk.advance(time.Minute + time.Second)
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); !errors.Is(err, onetime.ErrNoCode) {
		t.Errorf("Consume once the entry was reclaimed = %v, want ErrNoCode", err)
	}
}

// TestTheEntryIsKeptForTheLongerOfTheTwoDeadlines covers the other half of
// expiry: nothing is written without a deadline, so a store does not fill up
// with records of codes nobody used.
//
// The deadline is the ttl or the cooldown, whichever is longer, because the
// entry answers two questions -- what code is outstanding, and when the last one
// went out -- and the second outlives the first whenever the cooldown does.
func TestTheEntryIsKeptForTheLongerOfTheTwoDeadlines(t *testing.T) {
	cases := map[string]struct {
		config onetime.Config
		want   time.Duration
	}{
		"TheTTLIsLonger":      {onetime.Config{TTL: 7 * time.Minute, Cooldown: time.Minute}, 7 * time.Minute},
		"TheCooldownIsLonger": {onetime.Config{TTL: time.Minute, Cooldown: time.Hour}, time.Hour},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store := newHooked()
			codes := codesOver(t, store, newClock(), c.config)

			if _, err := codes.Issue(context.Background(), "confirm-address", "person-1"); err != nil {
				t.Fatalf("Issue: %v", err)
			}

			written := 0
			for key, ttl := range store.putTTLs {
				written++
				if ttl != c.want {
					t.Errorf("%s was written to live %s, want %s", key, ttl, c.want)
				}
			}
			if written != 1 {
				t.Errorf("Issue wrote %d entries, want 1", written)
			}
		})
	}
}

// TestSpendingACodeEndsTheCooldown pins the consequence of the cooldown living
// in the outstanding record and nowhere else: consuming removes the record, so
// the wait goes with it.
//
// That is the right answer rather than an accident of the storage. The cooldown
// is there to stop a resend button from mailing somebody ten messages, and
// somebody who has just used a code is not resending anything.
func TestSpendingACodeEndsTheCooldown(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{Cooldown: time.Hour})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); !errors.Is(err, onetime.ErrCooldown) {
		t.Fatalf("a resend before the code was used = %v, want ErrCooldown", err)
	}

	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Errorf("Issue with no time passed but the code spent = %v, want nil", err)
	}
}

// TestTheCooldownOutlivesTheCodeItThrottles is the case that made the entry's
// life the longer of the two deadlines.
//
// With the entry kept only for the ttl, a cooldown longer than the ttl would end
// when the record was reclaimed: the wait would be silently the ttl, and a
// resend button would mail again the moment the code went stale.
func TestTheCooldownOutlivesTheCodeItThrottles(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, newExpiring(clk), clk, onetime.Config{TTL: time.Minute, Cooldown: time.Hour})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Past the code's own deadline and nowhere near the cooldown's.
	clk.advance(2 * time.Minute)
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); !errors.Is(err, onetime.ErrExpired) {
		t.Errorf("the code two minutes into a one-minute ttl = %v, want ErrExpired", err)
	}
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); !errors.Is(err, onetime.ErrCooldown) {
		t.Errorf("a resend two minutes into an hour of cooldown = %v, want ErrCooldown", err)
	}

	clk.advance(59 * time.Minute)
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Errorf("a resend once the hour is up = %v, want nil", err)
	}
}

func TestTheAttemptLimitFinishesTheCode(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{MaxAttempts: 3})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	for attempt := 1; attempt <= 3; attempt++ {
		if err := codes.Consume(ctx, "confirm-address", "person-1", wrong); !errors.Is(err, onetime.ErrInvalidCode) {
			t.Fatalf("wrong guess %d = %v, want ErrInvalidCode", attempt, err)
		}
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", wrong); !errors.Is(err, onetime.ErrTooManyAttempts) {
		t.Errorf("the fourth wrong guess = %v, want ErrTooManyAttempts", err)
	}

	// The one that matters: past the limit, the right code is refused too. A
	// limit that still lets the correct code through is a limit on typing
	// speed, not on guessing.
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); !errors.Is(err, onetime.ErrTooManyAttempts) {
		t.Errorf("the right code after the limit = %v, want ErrTooManyAttempts", err)
	}
}

// TestTheAttemptLimitIsPerIssue proves the counter belongs to the code and not
// to the person: somebody whose code was guessed at until it died gets a working
// budget back with the next code, or the limit would be a way to lock them out.
func TestTheAttemptLimitIsPerIssue(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{MaxAttempts: 2, Cooldown: time.Minute})
	ctx := context.Background()

	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	for range 3 {
		_ = codes.Consume(ctx, "confirm-address", "person-1", "000000")
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", "000000"); !errors.Is(err, onetime.ErrTooManyAttempts) {
		t.Fatalf("the budget was not spent: %v", err)
	}

	clk.advance(time.Minute)
	fresh, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("the second Issue: %v", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", fresh); err != nil {
		t.Errorf("Consume of the replacement code = %v, want nil", err)
	}
}

// TestTheLimitFailsClosed is the difference between this and a rate limiter.
//
// Each subtest breaks one call the store owes and presents the CORRECT code:
// anything other than a refusal means the store being unreachable is a way past
// the attempt limit, which is the one outcome that is worse than an outage.
func TestTheLimitFailsClosed(t *testing.T) {
	broken := errors.New("the store is unreachable")

	cases := map[string]func(s *hookedStore){
		"ReadingTheRecord":   func(s *hookedStore) { s.getErr = broken },
		"CountingTheAttempt": func(s *hookedStore) { s.incrementErr = broken },
		"SpendingTheCode":    func(s *hookedStore) { s.addErr = broken },
	}

	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			store := newHooked()
			clk := newClock()
			codes := codesOver(t, store, clk, onetime.Config{})
			ctx := context.Background()

			code, err := codes.Issue(ctx, "confirm-address", "person-1")
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}

			breakIt(store)
			err = codes.Consume(ctx, "confirm-address", "person-1", code)
			if err == nil {
				t.Fatal("the right code was accepted while the store could not answer")
			}
			if !errors.Is(err, onetime.ErrUnavailable) {
				t.Errorf("Consume = %v, want it to unwrap to ErrUnavailable", err)
			}
			if !errors.Is(err, broken) {
				t.Errorf("Consume = %v, want it to carry the store's own error", err)
			}
		})
	}
}

func TestIssueFailsWhenTheRecordCannotBeWritten(t *testing.T) {
	broken := errors.New("the store is unreachable")

	t.Run("Reading", func(t *testing.T) {
		store := newHooked()
		store.getErr = broken
		codes := codesOver(t, store, newClock(), onetime.Config{})

		if _, err := codes.Issue(context.Background(), "confirm-address", "person-1"); !errors.Is(err, onetime.ErrUnavailable) {
			t.Errorf("Issue = %v, want ErrUnavailable", err)
		}
	})

	t.Run("Writing", func(t *testing.T) {
		store := newHooked()
		store.putErr = broken
		codes := codesOver(t, store, newClock(), onetime.Config{})

		code, err := codes.Issue(context.Background(), "confirm-address", "person-1")
		if !errors.Is(err, onetime.ErrUnavailable) {
			t.Errorf("Issue = %v, want ErrUnavailable", err)
		}
		if code != "" {
			t.Errorf("Issue returned the code %q along with an error, and a caller that mails it is mailing a code nothing will accept", code)
		}
	})
}

// TestIssuingInvalidatesThePrevious pins down what "invalidates" means when the
// store is the only memory there is: the record is replaced, so the code it
// described stops being recognisable -- immediately, and whether or not anybody
// is holding it.
func TestIssuingInvalidatesThePrevious(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{Cooldown: time.Minute})
	ctx := context.Background()

	first, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("the first Issue: %v", err)
	}

	clk.advance(time.Minute)
	second, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("the second Issue: %v", err)
	}
	if first == second {
		t.Fatal("both issues produced the same code, and this test cannot tell them apart")
	}

	if err := codes.Consume(ctx, "confirm-address", "person-1", first); !errors.Is(err, onetime.ErrInvalidCode) {
		t.Errorf("the replaced code = %v, want ErrInvalidCode", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", second); err != nil {
		t.Errorf("the code that replaced it = %v, want nil", err)
	}
}

func TestTheCooldownRefusesAResend(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{Cooldown: time.Minute})
	ctx := context.Background()

	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Fatalf("the first Issue: %v", err)
	}

	clk.advance(20 * time.Second)
	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if !errors.Is(err, onetime.ErrCooldown) {
		t.Fatalf("Issue twenty seconds into a minute of cooldown = %v, want ErrCooldown", err)
	}
	if code != "" {
		t.Errorf("Issue returned the code %q along with the cooldown error", code)
	}

	var cooling *onetime.CooldownError
	if !errors.As(err, &cooling) {
		t.Fatalf("Issue = %v, want a *CooldownError somewhere in it", err)
	}
	if cooling.RetryAfter != 40*time.Second {
		t.Errorf("RetryAfter = %s, want 40s", cooling.RetryAfter)
	}

	// Another subject is not waiting for this one's cooldown.
	if _, err := codes.Issue(ctx, "confirm-address", "person-2"); err != nil {
		t.Errorf("Issue for another subject during the first one's cooldown = %v, want nil", err)
	}

	clk.advance(40 * time.Second)
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Errorf("Issue once the cooldown has run out = %v, want nil", err)
	}
}

func TestAnAbsentCodeIsRefused(t *testing.T) {
	codes := codesOver(t, cache.NewArrayStore(), newClock(), onetime.Config{})

	if err := codes.Consume(context.Background(), "confirm-address", "person-1", "123456"); !errors.Is(err, onetime.ErrNoCode) {
		t.Errorf("Consume with nothing outstanding = %v, want ErrNoCode", err)
	}
}

func TestAnEmptyPurposeOrSubjectIsRefused(t *testing.T) {
	codes := codesOver(t, cache.NewArrayStore(), newClock(), onetime.Config{})
	ctx := context.Background()

	if _, err := codes.Issue(ctx, "", "person-1"); !errors.Is(err, onetime.ErrNoPurpose) {
		t.Errorf("Issue with no purpose = %v, want ErrNoPurpose", err)
	}
	if _, err := codes.Issue(ctx, "confirm-address", ""); !errors.Is(err, onetime.ErrNoSubject) {
		t.Errorf("Issue with no subject = %v, want ErrNoSubject", err)
	}
	if err := codes.Consume(ctx, "", "person-1", "123456"); !errors.Is(err, onetime.ErrNoPurpose) {
		t.Errorf("Consume with no purpose = %v, want ErrNoPurpose", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "", "123456"); !errors.Is(err, onetime.ErrNoSubject) {
		t.Errorf("Consume with no subject = %v, want ErrNoSubject", err)
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", ""); !errors.Is(err, onetime.ErrInvalidCode) {
		t.Errorf("Consume with no code = %v, want ErrInvalidCode", err)
	}
}

func TestNewRefusesWhatCannotWork(t *testing.T) {
	cases := map[string]struct {
		store  cache.Store
		key    []byte
		config onetime.Config
	}{
		"NoStore":          {nil, appKey, onetime.Config{}},
		"ShortKey":         {cache.NewArrayStore(), appKey[:31], onetime.Config{}},
		"NoKey":            {cache.NewArrayStore(), nil, onetime.Config{}},
		"NegativeTTL":      {cache.NewArrayStore(), appKey, onetime.Config{TTL: -time.Second}},
		"NegativeCooldown": {cache.NewArrayStore(), appKey, onetime.Config{Cooldown: -time.Second}},
		"NegativeAttempts": {cache.NewArrayStore(), appKey, onetime.Config{MaxAttempts: -1}},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			codes, err := onetime.New(c.store, c.key, c.config)
			if err == nil {
				t.Fatal("New accepted a configuration that cannot work")
			}
			if codes != nil {
				t.Error("New returned a Codes along with the error")
			}
		})
	}
}

func TestTheZeroConfigIsTheDocumentedOne(t *testing.T) {
	clk := newClock()
	codes := codesOver(t, cache.NewArrayStore(), clk, onetime.Config{})
	ctx := context.Background()

	code, err := codes.Issue(ctx, "confirm-address", "person-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	clk.advance(onetime.DefaultTTL - time.Second)
	if err := codes.Consume(ctx, "confirm-address", "person-1", code); err != nil {
		t.Errorf("Consume just inside the default ttl = %v, want nil", err)
	}

	// The default cooldown is in force from the same zero config.
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Fatalf("the second Issue: %v", err)
	}
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); !errors.Is(err, onetime.ErrCooldown) {
		t.Errorf("a resend under the default cooldown = %v, want ErrCooldown", err)
	}

	clk.advance(onetime.DefaultCooldown + time.Second)
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Errorf("a resend past the default cooldown = %v, want nil", err)
	}
}

func TestTheDefaultAttemptLimitIsInForce(t *testing.T) {
	codes := codesOver(t, cache.NewArrayStore(), newClock(), onetime.Config{})
	ctx := context.Background()

	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	for attempt := 1; attempt <= onetime.DefaultMaxAttempts; attempt++ {
		if err := codes.Consume(ctx, "confirm-address", "person-1", "000000"); !errors.Is(err, onetime.ErrInvalidCode) {
			t.Fatalf("guess %d of %d = %v, want ErrInvalidCode", attempt, onetime.DefaultMaxAttempts, err)
		}
	}
	if err := codes.Consume(ctx, "confirm-address", "person-1", "000000"); !errors.Is(err, onetime.ErrTooManyAttempts) {
		t.Errorf("the guess after the default limit = %v, want ErrTooManyAttempts", err)
	}
}

func TestCooldownErrorReadsAsASentence(t *testing.T) {
	err := &onetime.CooldownError{RetryAfter: 40 * time.Second}
	if !strings.Contains(err.Error(), "40s") {
		t.Errorf("the message is %q, and it does not say how long is left", err.Error())
	}
	if !errors.Is(err, onetime.ErrCooldown) {
		t.Error("a CooldownError does not unwrap to ErrCooldown")
	}
}
