package concurrency

import (
	"context"
	"sync"
)

// Task is a unit of work that produces a value.
//
// The context a task receives is derived from the one given to [Run], and it is
// cancelled as soon as any sibling task fails. A task that blocks -- on I/O, on
// a channel, on a lock -- should pass that context along rather than ignore it,
// or the whole call waits for work whose result is already discarded.
type Task[T any] func(ctx context.Context) (T, error)

// Run runs every task concurrently and returns the results in argument order.
//
// A set of results in completion order would be a different set every run.
//
// Run waits for every goroutine it starts, so none outlives the call. The first
// task to fail cancels the context the others run under, and tasks that had not
// started by then never start. The error returned is that first failure -- the
// cause, not whatever a sibling reported on its way out.
//
// The results slice is nil whenever the error is non-nil. A half-filled slice
// next to an error is a trap, and the caller has no way to tell which entries
// are real.
//
// A task that panics fails with a [PanicError] instead of ending the process.
//
// With no tasks Run returns (nil, nil). If ctx is already done, Run returns its
// error without starting anything, and it returns that error too when ctx is
// cancelled while the tasks are running: an incomplete set must not come back
// looking complete.
func Run[T any](ctx context.Context, tasks ...Task[T]) ([]T, error) {
	return RunWith(ctx, 0, tasks...)
}

// Defer returns the callback that starts the tasks once the current work is
// finished. It runs nothing itself, and the caller decides when the callback
// runs.
//
// The context is detached with context.WithoutCancel: the tasks run after the
// request they came from is over, and a deferred callback that inherits a
// cancelled context never runs at all. Deadlines go with it, so a deferred task
// that needs one sets its own.
//
// The callback runs the tasks the way [Run] does -- concurrently, the first
// failure cancelling the rest, a panic answering as a [PanicError]. Invoking
// the callback twice runs the tasks twice.
func Defer[T any](ctx context.Context, tasks ...Task[T]) func() error {
	deferred := context.WithoutCancel(ctx)

	return func() error {
		_, err := Run(deferred, tasks...)
		return err
	}
}

// RunWith is [Run] with a ceiling on how many tasks run at the same time.
//
// A limit of zero or less is no ceiling, which is what [Run] passes. Use a
// ceiling when the tasks contend for something finite: database connections, an
// API that rate limits, memory proportional to each payload.
//
// A goroutine is cheap enough that a hundred of them are free and a hundred
// connections are not, which is the one place where what the mechanism costs
// changes what the API needs.
func RunWith[T any](ctx context.Context, limit int, tasks ...Task[T]) ([]T, error) {
	if len(tasks) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// A ceiling wider than the set of tasks is no ceiling at all, and the
	// channel would only add a hop to every task.
	var sem chan struct{}
	if limit > 0 && limit < len(tasks) {
		sem = make(chan struct{}, limit)
	}

	results := make([]T, len(tasks))

	var (
		wg      sync.WaitGroup
		once    sync.Once
		failure error
	)
	for i, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-runCtx.Done():
					// Cancelled while queued behind the ceiling. This task never
					// ran, so it reports nothing: the failure that cancelled it
					// is the one worth returning.
					return
				}
			}
			if runCtx.Err() != nil {
				return
			}

			v, err := call(runCtx, taskName(i), task)
			if err != nil {
				once.Do(func() {
					failure = err
					cancel()
				})
				return
			}
			results[i] = v
		}()
	}
	wg.Wait()

	if failure != nil {
		return nil, failure
	}
	// No task failed, so a cancelled parent is the only way the set can still be
	// incomplete -- and it is incomplete in silence unless it is reported here.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
