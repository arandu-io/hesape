package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/events"
)

// TestTheRelayPublishesNothingWhileAnotherReplicaHoldsTheLock is the test the
// Locker interface never had.
//
// The relay used to take an interface of its own named Locker, which nothing in
// the collection implemented -- *cache.Locks has Lock and RestoreLock, not Run
// -- so the lock was configurable, unusable, and untested. It takes *cache.Locks
// now, the way the scheduler does, and this holds it to the behaviour the field
// exists for: a replica that finds the lock held publishes nothing and does not
// report a failure, and it publishes as soon as the lock is free.
func TestTheRelayPublishesNothingWhileAnotherReplicaHoldsTheLock(t *testing.T) {
	locks := cache.NewLocks(cache.NewArrayStore())
	ctx := context.Background()

	// Another replica is mid-pass.
	held := locks.Lock("outbox-relay", time.Minute)
	if err := held.Acquire(ctx); err != nil {
		t.Fatalf("taking the lock: %v", err)
	}

	publisher := newRecorder()
	relay, table := relayOver(t, events.RelayOptions{
		Interval: time.Millisecond,
		Locks:    locks,
		LockTTL:  time.Minute,
	}, publisher)
	table.add("order.placed", time.Second)

	loop, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- relay.Run(loop) }()

	// Long enough for many passes, every one of them refused.
	time.Sleep(50 * time.Millisecond)
	if got := publisher.received(); len(got) != 0 {
		t.Fatalf("the relay published %v while another replica held the lock", got)
	}

	if err := held.Release(ctx); err != nil {
		t.Fatalf("releasing the lock: %v", err)
	}

	select {
	case <-publisher.got:
	case <-time.After(2 * time.Second):
		t.Fatal("the relay published nothing after the lock was released")
	}

	stop()
	if err := <-done; err != nil {
		t.Fatalf("the relay stopped with %v", err)
	}
	if got := table.delivered(); len(got) == 0 {
		t.Fatal("nothing was marked published")
	}
}
