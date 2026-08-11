package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/cache/events"
)

func TestGetFiresRetrievingThenHit(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetName("memory").SetEventDispatcher(rec)
	g := grantFor("acme")

	if err := r.Put(ctx, g, "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rec.events = nil

	if _, err := cache.Get[string](ctx, r, g, "k"); err != nil {
		t.Fatalf("Get: %v", err)
	}

	all := rec.all()
	if len(all) != 2 {
		t.Fatalf("Get fired %d events, want 2", len(all))
	}
	if _, ok := all[0].(*events.RetrievingKey); !ok {
		t.Fatalf("the first event is a %T, want *events.RetrievingKey", all[0])
	}
	hit, ok := all[1].(*events.CacheHit)
	if !ok {
		t.Fatalf("the second event is a %T, want *events.CacheHit", all[1])
	}
	if hit.Key != "k" {
		t.Fatalf("CacheHit.Key = %q, want %q: the event carries the caller's key, not the built one", hit.Key, "k")
	}
	if hit.StoreName != "memory" {
		t.Fatalf("CacheHit.StoreName = %q, want %q", hit.StoreName, "memory")
	}
	if hit.Value != "v" {
		t.Fatalf("CacheHit.Value = %v, want %q", hit.Value, "v")
	}
}

func TestGetFiresMissedOnAMiss(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)

	if _, err := cache.Get[string](ctx, r, grantFor("acme"), "absent"); err == nil {
		t.Fatal("Get of an absent key = nil, want cache.ErrNotFound")
	}
	if n := count[*events.CacheMissed](rec); n != 1 {
		t.Fatalf("%d CacheMissed events, want 1", n)
	}
	if n := count[*events.CacheHit](rec); n != 0 {
		t.Fatalf("%d CacheHit events on a miss, want 0", n)
	}
}

func TestPutFiresWritingThenWritten(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)

	if err := r.Put(ctx, grantFor("acme"), "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	all := rec.all()
	if len(all) != 2 {
		t.Fatalf("Put fired %d events, want 2", len(all))
	}
	writing, ok := all[0].(*events.WritingKey)
	if !ok {
		t.Fatalf("the first event is a %T, want *events.WritingKey", all[0])
	}
	if writing.Seconds != 60 {
		t.Fatalf("WritingKey.Seconds = %d, want 60", writing.Seconds)
	}
	if _, ok := all[1].(*events.KeyWritten); !ok {
		t.Fatalf("the second event is a %T, want *events.KeyWritten", all[1])
	}
}

// TestPutManyFiresOneWritingManyKeys is the difference between putMany and a
// loop over put: a listener counting writes must not count each of them twice.
func TestPutManyFiresOneWritingManyKeys(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)

	err := r.PutMany(ctx, grantFor("acme"), map[string]any{"a": 1, "b": 2, "c": 3}, time.Minute)
	if err != nil {
		t.Fatalf("PutMany: %v", err)
	}

	if n := count[*events.WritingManyKeys](rec); n != 1 {
		t.Fatalf("%d WritingManyKeys events, want 1", n)
	}
	if n := count[*events.WritingKey](rec); n != 0 {
		t.Fatalf("%d WritingKey events from PutMany, want 0", n)
	}
	if n := count[*events.KeyWritten](rec); n != 3 {
		t.Fatalf("%d KeyWritten events, want 3", n)
	}

	batch := rec.all()[0].(*events.WritingManyKeys)
	if len(batch.Keys) != 3 || batch.Keys[0] != "a" {
		t.Fatalf("WritingManyKeys.Keys = %v, want a, b, c in order", batch.Keys)
	}
	if batch.Key != "a" {
		t.Fatalf("WritingManyKeys.Key = %q, want the first key", batch.Key)
	}
}

// TestManyFiresOneRetrievingManyKeys is the same rule on the read side.
func TestManyFiresOneRetrievingManyKeys(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)
	g := grantFor("acme")

	if err := r.Put(ctx, g, "a", 1, time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rec.events = nil

	if _, err := cache.Many[int](ctx, r, g, "a", "b"); err != nil {
		t.Fatalf("Many: %v", err)
	}
	if n := count[*events.RetrievingManyKeys](rec); n != 1 {
		t.Fatalf("%d RetrievingManyKeys events, want 1", n)
	}
	if n := count[*events.RetrievingKey](rec); n != 0 {
		t.Fatalf("%d RetrievingKey events from Many, want 0", n)
	}
	if n := count[*events.CacheHit](rec); n != 1 {
		t.Fatalf("%d CacheHit events, want 1", n)
	}
	if n := count[*events.CacheMissed](rec); n != 1 {
		t.Fatalf("%d CacheMissed events, want 1", n)
	}
}

func TestForgetFiresForgettingThenForgotten(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)
	g := grantFor("acme")

	if err := r.Put(ctx, g, "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rec.events = nil

	if err := r.Forget(ctx, g, "k"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	all := rec.all()
	if len(all) != 2 {
		t.Fatalf("Forget fired %d events, want 2", len(all))
	}
	if _, ok := all[0].(*events.ForgettingKey); !ok {
		t.Fatalf("the first event is a %T, want *events.ForgettingKey", all[0])
	}
	if _, ok := all[1].(*events.KeyForgotten); !ok {
		t.Fatalf("the second event is a %T, want *events.KeyForgotten", all[1])
	}
}

func TestFlushFiresFlushingThenFlushed(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)

	if err := r.Flush(ctx, grantFor("acme")); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	all := rec.all()
	if len(all) != 2 {
		t.Fatalf("Flush fired %d events, want 2", len(all))
	}
	if _, ok := all[0].(*events.CacheFlushing); !ok {
		t.Fatalf("the first event is a %T, want *events.CacheFlushing", all[0])
	}
	if _, ok := all[1].(*events.CacheFlushed); !ok {
		t.Fatalf("the second event is a %T, want *events.CacheFlushed", all[1])
	}
}

func TestFlushLocksFiresItsThreeEvents(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)

	if err := r.FlushLocks(ctx); err != nil {
		t.Fatalf("FlushLocks: %v", err)
	}
	if n := count[*events.CacheLocksFlushing](rec); n != 1 {
		t.Fatalf("%d CacheLocksFlushing events, want 1", n)
	}
	if n := count[*events.CacheLocksFlushed](rec); n != 1 {
		t.Fatalf("%d CacheLocksFlushed events, want 1", n)
	}
	if n := count[*events.CacheLocksFlushFailed](rec); n != 0 {
		t.Fatalf("%d CacheLocksFlushFailed events on a flush that worked, want 0", n)
	}
}

func TestTaggedEventsCarryTheTags(t *testing.T) {
	ctx := context.Background()
	rec := &recorder{}
	r := cache.New(cache.NewArrayStore()).SetEventDispatcher(rec)

	tagged, err := r.Tags("invoices", "totals")
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if err := tagged.Put(ctx, grantFor("acme"), "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}

	written, ok := rec.all()[1].(*events.KeyWritten)
	if !ok {
		t.Fatalf("the second event is a %T, want *events.KeyWritten", rec.all()[1])
	}
	if len(written.Tags) != 2 || written.Tags[0] != "invoices" {
		t.Fatalf("KeyWritten.Tags = %v, want the two tags in order", written.Tags)
	}
}

func TestARepositoryWithNoDispatcherFiresNothing(t *testing.T) {
	ctx := context.Background()
	r := cache.New(cache.NewArrayStore())

	if r.GetEventDispatcher() != nil {
		t.Fatal("a repository built with New already has a dispatcher")
	}
	// The only assertion available is that it does not panic, which is the one
	// that matters: a cache built without an event bus has to work.
	if err := r.Put(ctx, grantFor("acme"), "k", "v", time.Minute); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func TestSetEventDispatcherDerives(t *testing.T) {
	rec := &recorder{}
	plain := cache.New(cache.NewArrayStore())
	loud := plain.SetEventDispatcher(rec)

	if plain.GetEventDispatcher() != nil {
		t.Fatal("SetEventDispatcher changed the repository it was called on")
	}
	if loud.GetEventDispatcher() == nil {
		t.Fatal("SetEventDispatcher returned a repository without one")
	}
}

func TestSetTagsRewritesTheEvent(t *testing.T) {
	e := events.NewCacheHit("memory", "k", "v", nil)
	e.SetTags([]string{"invoices"})

	if len(e.Tags) != 1 || e.Tags[0] != "invoices" {
		t.Fatalf("Tags after SetTags = %v, want invoices", e.Tags)
	}
}

func TestItemKeyShowsWhereTheEntryWent(t *testing.T) {
	ctx := context.Background()
	r := cache.New(cache.NewArrayStore()).Namespace("invoice")

	key, err := r.ItemKey(ctx, grantFor("acme"), "total")
	if err != nil {
		t.Fatalf("ItemKey: %v", err)
	}
	if key != "cache:acme:invoice:total" {
		t.Fatalf("ItemKey = %q, want %q", key, "cache:acme:invoice:total")
	}
}
