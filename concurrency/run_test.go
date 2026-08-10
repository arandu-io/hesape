package concurrency_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arandu-io/hesape/concurrency"
)

func TestRunReturnsResultsInArgumentOrder(t *testing.T) {
	t.Parallel()

	// The third task finishes first; the order of the results must not depend
	// on the order the goroutines happened to complete in.
	done := make(chan struct{})
	tasks := []concurrency.Task[string]{
		func(ctx context.Context) (string, error) {
			<-done
			return "first", nil
		},
		func(ctx context.Context) (string, error) {
			<-done
			return "second", nil
		},
		func(ctx context.Context) (string, error) {
			close(done)
			return "third", nil
		},
	}

	got, err := concurrency.Run(context.Background(), tasks...)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRunRunsTasksAtTheSameTime(t *testing.T) {
	t.Parallel()

	const n = 4
	started := make(chan struct{}, n)
	release := make(chan struct{})

	tasks := make([]concurrency.Task[int], n)
	for i := range tasks {
		tasks[i] = func(ctx context.Context) (int, error) {
			started <- struct{}{}
			<-release
			return i, nil
		}
	}

	type outcome struct {
		values []int
		err    error
	}
	result := make(chan outcome, 1)
	go func() {
		v, err := concurrency.Run(context.Background(), tasks...)
		result <- outcome{v, err}
	}()

	// Every task must reach its body before any of them is allowed to return.
	// Serial execution cannot satisfy that.
	for i := 0; i < n; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d tasks started", i, n)
		}
	}
	close(release)

	got := <-result
	if got.err != nil {
		t.Fatalf("Run: %v", got.err)
	}
	for i, v := range got.values {
		if v != i {
			t.Fatalf("got %v, want ascending indexes", got.values)
		}
	}
}

func TestRunReturnsTheFirstFailureAndCancelsTheRest(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	observed := make(chan error, 1)
	entered := make(chan struct{})

	// The blocking task comes first in argument order, so a failure reported on
	// the way out must not displace the one that actually caused the cancel. It
	// also has to be running before the other fails, or it is simply skipped.
	_, err := concurrency.Run(context.Background(),
		func(ctx context.Context) (int, error) {
			close(entered)
			<-ctx.Done()
			observed <- ctx.Err()
			return 0, ctx.Err()
		},
		func(ctx context.Context) (int, error) {
			<-entered
			return 0, boom
		},
	)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}

	select {
	case cause := <-observed:
		if !errors.Is(cause, context.Canceled) {
			t.Fatalf("sibling saw %v, want context.Canceled", cause)
		}
	default:
		t.Fatal("Run returned before the sibling observed the cancellation")
	}
}

func TestRunDiscardsResultsWhenATaskFails(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	got, err := concurrency.Run(context.Background(),
		func(ctx context.Context) (int, error) { return 1, nil },
		func(ctx context.Context) (int, error) { return 0, boom },
	)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
	if got != nil {
		t.Fatalf("got %v, want no results alongside an error", got)
	}
}

func TestRunTurnsAPanicIntoAnError(t *testing.T) {
	t.Parallel()

	_, err := concurrency.Run(context.Background(),
		func(ctx context.Context) (int, error) { return 1, nil },
		func(ctx context.Context) (int, error) { panic("held wrong") },
	)

	var panicErr *concurrency.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("got %v (%T), want a *concurrency.PanicError", err, err)
	}
	if panicErr.Name != "task 1" {
		t.Fatalf("name is %q, want the position of the task", panicErr.Name)
	}
	if panicErr.Value != "held wrong" {
		t.Fatalf("value is %v, want the recovered value", panicErr.Value)
	}
	if !strings.Contains(string(panicErr.Stack), "concurrency") {
		t.Fatalf("stack does not name the package:\n%s", panicErr.Stack)
	}
}

func TestRunWithoutTasks(t *testing.T) {
	t.Parallel()

	got, err := concurrency.Run[int](context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want no results", got)
	}
}

func TestRunStartsNothingWhenTheContextIsAlreadyDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var ran atomic.Bool
	_, err := concurrency.Run(ctx, func(ctx context.Context) (int, error) {
		ran.Store(true)
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if ran.Load() {
		t.Fatal("the task ran under a context that was already done")
	}
}

func TestRunReportsAParentCancelledMidFlight(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The task succeeds, so nothing inside the call failed. The set is still
	// incomplete from the caller's point of view.
	got, err := concurrency.Run(ctx, func(ctx context.Context) (int, error) {
		cancel()
		return 1, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if got != nil {
		t.Fatalf("got %v, want no results alongside an error", got)
	}
}

func TestRunWithHoldsTheCeiling(t *testing.T) {
	t.Parallel()

	const (
		n     = 6
		limit = 2
	)

	var (
		running atomic.Int64
		peak    atomic.Int64
		reached = make(chan struct{})
		closed  atomic.Bool
	)

	tasks := make([]concurrency.Task[int], n)
	for i := range tasks {
		tasks[i] = func(ctx context.Context) (int, error) {
			current := running.Add(1)
			defer running.Add(-1)

			for {
				was := peak.Load()
				if current <= was || peak.CompareAndSwap(was, current) {
					break
				}
			}

			// Hold every task in the body until the ceiling is filled, which
			// proves the ceiling is a floor too: it does let limit tasks run.
			if current >= limit && closed.CompareAndSwap(false, true) {
				close(reached)
			}
			select {
			case <-reached:
			case <-time.After(5 * time.Second):
				return 0, errors.New("the ceiling was never filled")
			}
			return i, nil
		}
	}

	got, err := concurrency.RunWith(context.Background(), limit, tasks...)
	if err != nil {
		t.Fatalf("RunWith: %v", err)
	}
	if len(got) != n {
		t.Fatalf("got %d results, want %d", len(got), n)
	}
	if p := peak.Load(); p != limit {
		t.Fatalf("%d tasks ran at once, want exactly %d", p, limit)
	}
}

func TestRunWithTreatsANonPositiveLimitAsNoCeiling(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{0, -1} {
		const n = 3
		started := make(chan struct{}, n)
		release := make(chan struct{})

		tasks := make([]concurrency.Task[int], n)
		for i := range tasks {
			tasks[i] = func(ctx context.Context) (int, error) {
				started <- struct{}{}
				<-release
				return i, nil
			}
		}

		errc := make(chan error, 1)
		go func() {
			_, err := concurrency.RunWith(context.Background(), limit, tasks...)
			errc <- err
		}()

		for i := 0; i < n; i++ {
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatalf("limit %d: only %d of %d tasks started", limit, i, n)
			}
		}
		close(release)

		if err := <-errc; err != nil {
			t.Fatalf("limit %d: RunWith: %v", limit, err)
		}
	}
}
