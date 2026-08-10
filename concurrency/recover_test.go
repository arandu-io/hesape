package concurrency_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/concurrency"
)

func TestRecoverPassesThroughWhatFnReturns(t *testing.T) {
	t.Parallel()

	if err := concurrency.Recover(context.Background(), "queue: noop", func() error {
		return nil
	}); err != nil {
		t.Fatalf("got %v, want nil", err)
	}

	boom := errors.New("boom")
	err := concurrency.Recover(context.Background(), "queue: send-invoice", func() error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
	var panicErr *concurrency.PanicError
	if errors.As(err, &panicErr) {
		t.Fatal("an ordinary failure was reported as a panic")
	}
}

func TestRecoverTurnsAPanicIntoAnError(t *testing.T) {
	t.Parallel()

	err := concurrency.Recover(context.Background(), "queue: send-invoice", func() error {
		var recipients []string
		_ = recipients[3]
		return nil
	})

	var panicErr *concurrency.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("got %v (%T), want a *concurrency.PanicError", err, err)
	}
	if panicErr.Name != "queue: send-invoice" {
		t.Fatalf("name is %q, want the name given to Recover", panicErr.Name)
	}
	if len(panicErr.Stack) == 0 {
		t.Fatal("no stack was captured")
	}
	if !strings.Contains(err.Error(), "queue: send-invoice") {
		t.Fatalf("message %q does not name the call site", err.Error())
	}
}

func TestRecoverReachesAnErrorThatWasPanickedWith(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	err := concurrency.Recover(context.Background(), "relay", func() error {
		panic(boom)
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want errors.Is to reach %v", err, boom)
	}

	var panicErr *concurrency.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("got %v (%T), want a *concurrency.PanicError", err, err)
	}
	if panicErr.Unwrap() != boom {
		t.Fatalf("Unwrap returned %v, want %v", panicErr.Unwrap(), boom)
	}
}

func TestRecoverDoesNotRunWorkThatIsAlreadyCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ran := false
	err := concurrency.Recover(ctx, "schedule: prune-sessions", func() error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if ran {
		t.Fatal("fn ran under a context that was already done")
	}
}

func TestPanicErrorMessage(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "queue: send-invoice", value: "held wrong", want: "queue: send-invoice: panic: held wrong"},
		{name: "", value: 42, want: "panic: 42"},
	} {
		err := &concurrency.PanicError{Name: c.name, Value: c.value}
		if got := err.Error(); got != c.want {
			t.Fatalf("got %q, want %q", got, c.want)
		}
	}
}

func TestPanicErrorUnwrapsNothingButAnError(t *testing.T) {
	t.Parallel()

	err := &concurrency.PanicError{Name: "relay", Value: "held wrong"}
	if unwrapped := err.Unwrap(); unwrapped != nil {
		t.Fatalf("got %v, want nil for a value that is not an error", unwrapped)
	}
}
