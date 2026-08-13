package promises_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/http/client/promises"
)

func TestADeferredHandsTheValueToWhoeverWaits(t *testing.T) {
	deferred := promises.NewDeferred(func() {})

	if got := deferred.GetState(); got != promises.StatePending {
		t.Fatalf("state = %q, want pending", got)
	}

	go func() { _ = deferred.Resolve("ok") }()

	value, err := deferred.Wait(true)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if value != "ok" {
		t.Fatalf("value = %v, want ok", value)
	}
	if got := deferred.GetState(); got != promises.StateFulfilled {
		t.Fatalf("state = %q, want fulfilled", got)
	}
}

func TestOtherwiseRunsOnTheRejectionAndThenDoesNot(t *testing.T) {
	deferred := promises.NewDeferred(nil)

	var rejected error
	fulfilled := false
	deferred.Then(func(any) any { fulfilled = true; return nil }, nil)
	deferred.Otherwise(func(reason error) any { rejected = reason; return nil })

	boom := errors.New("boom")
	if err := deferred.Reject(boom); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	if fulfilled {
		t.Fatal("the fulfilment callback ran on a rejected promise")
	}
	if !errors.Is(rejected, boom) {
		t.Fatalf("rejection reason = %v, want boom", rejected)
	}
	if _, err := deferred.Wait(true); !errors.Is(err, boom) {
		t.Fatalf("Wait error = %v, want boom", err)
	}
	if _, err := deferred.Wait(false); err != nil {
		t.Fatalf("Wait without unwrapping should not return the reason, got %v", err)
	}
}

func TestCancelRejectsAPendingPromiseAndLetsASettledOneBe(t *testing.T) {
	pending := promises.NewDeferred(nil)
	if err := pending.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if _, err := pending.Wait(true); !errors.Is(err, promises.ErrCancelled) {
		t.Fatalf("Wait error = %v, want ErrCancelled", err)
	}

	settled := promises.NewDeferred(nil)
	_ = settled.Resolve("done")
	if err := settled.Cancel(); err != nil {
		t.Fatalf("cancelling a settled promise should be quiet, got %v", err)
	}
	value, _ := settled.Wait(true)
	if value != "done" {
		t.Fatalf("value = %v, want done -- Cancel must not undo a settled promise", value)
	}
}

func TestALazyPromiseDoesNothingUntilItIsWaitedOn(t *testing.T) {
	built := 0
	lazy := promises.NewLazyPromise(func() promises.Promise {
		built++
		deferred := promises.NewDeferred(nil)
		_ = deferred.Resolve("late")
		return deferred
	})

	replayed := false
	lazy.Then(func(any) any { replayed = true; return nil }, nil)

	if built != 0 {
		t.Fatal("the builder ran before anything waited on the promise")
	}
	if !lazy.PromiseNeedsBuilt() {
		t.Fatal("an unbuilt promise should say it needs building")
	}
	if got := lazy.GetState(); got != promises.StatePending {
		t.Fatalf("state = %q, want pending", got)
	}

	value, err := lazy.Wait(true)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if value != "late" {
		t.Fatalf("value = %v, want late", value)
	}
	if built != 1 {
		t.Fatalf("the builder ran %d times, want 1", built)
	}
	if !replayed {
		t.Fatal("the callback queued before the build was not replayed onto it")
	}
	if _, err := lazy.BuildPromise(); !errors.Is(err, promises.ErrAlreadyBuilt) {
		t.Fatalf("BuildPromise error = %v, want ErrAlreadyBuilt", err)
	}
}

func TestALazyPromiseRefusesToBeSettledByHand(t *testing.T) {
	lazy := promises.NewLazyPromise(func() promises.Promise { return promises.NewDeferred(nil) })

	for name, err := range map[string]error{
		"Resolve": lazy.Resolve("x"),
		"Reject":  lazy.Reject(errors.New("x")),
		"Cancel":  lazy.Cancel(),
	} {
		if !errors.Is(err, promises.ErrLazy) {
			t.Fatalf("%s error = %v, want ErrLazy", name, err)
		}
	}
}

func TestAFluentPromiseForwardsAndKeepsChaining(t *testing.T) {
	deferred := promises.NewDeferred(nil)
	fluent := promises.NewFluentPromise(deferred)

	seen := ""
	if fluent.Then(func(value any) any { seen, _ = value.(string); return nil }, nil) != promises.Promise(fluent) {
		t.Fatal("Then should hand back the fluent promise itself")
	}

	if err := fluent.Resolve("through"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if seen != "through" {
		t.Fatalf("callback saw %q, want through", seen)
	}
	if got := fluent.GetState(); got != promises.StateFulfilled {
		t.Fatalf("state = %q, want fulfilled", got)
	}
	if fluent.GetUnderlyingPromise() != promises.Promise(deferred) {
		t.Fatal("GetUnderlyingPromise should hand back what was decorated")
	}
}
