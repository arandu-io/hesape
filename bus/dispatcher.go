package bus

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/arandu-io/hesape/auth"
)

// Handler runs one job.
//
// It is what Illuminate finds by class name in its `$handlers` map, with the
// arguments still encoded: a handler decodes them itself, because the type it
// decodes into is the one thing this package cannot know.
type Handler func(ctx context.Context, g auth.Grant, payload []byte) error

// Pipe wraps a Handler.
//
// It is Illuminate's bus pipeline, which sends every command through a list of
// pipes before the handler sees it -- the place a transaction, a log line or a
// tenant check goes when it must apply to every job and not to one.
type Pipe func(next Handler) Handler

// Dispatcher sends a job to its handler: now, or through the queue.
//
// It is Illuminate's Bus\Dispatcher. The difference is what a job is: there a
// command is an object and the handler is found by its class name, here a job
// is a name and a JSON payload and the handler is found by that name. The
// mapping is explicit either way -- Illuminate's `map()` is this one's Map --
// and explicit is the only option without a container (ADR 0001).
//
// A Dispatcher with no queue runs everything in the current process, which is
// what a command-line tool and a test want. With one, Dispatch queues and
// DispatchNow does not, which is the same pair of words Illuminate uses.
type Dispatcher struct {
	repository BatchRepository
	queue      Queue

	mu       sync.RWMutex
	handlers map[string]Handler
	pipes    []Pipe
	// allowsDispatchingAfterResponses is Illuminate's flag of the same name:
	// with it off, DispatchAfterResponse runs the job at once instead of
	// holding it, which is what a test wants.
	allowsDispatchingAfterResponses bool
	terminating                     []func(ctx context.Context) error
}

// NewDispatcher returns a dispatcher over a queue and a batch repository.
//
// Either may be nil: without a queue nothing can be dispatched to one, and
// without a repository nothing can be batched. Both say so at the call rather
// than at the panic.
func NewDispatcher(q Queue, r BatchRepository) *Dispatcher {
	return &Dispatcher{
		repository:                      r,
		queue:                           q,
		handlers:                        make(map[string]Handler),
		allowsDispatchingAfterResponses: true,
	}
}

// Map registers handlers by job name, adding to whatever is already there.
//
// It is Illuminate's `map()`, which merges rather than replaces for the same
// reason: a module registers its own jobs and must not silence another
// module's.
func (d *Dispatcher) Map(m map[string]Handler) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	maps.Copy(d.handlers, m)
	return d
}

// HasCommandHandler reports whether a job name has a handler registered.
func (d *Dispatcher) HasCommandHandler(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.handlers[name]
	return ok
}

// GetCommandHandler returns the handler for a job name.
//
// The second result is false when there is none, where Illuminate returns the
// literal `false` from a method documented as returning `mixed`.
func (d *Dispatcher) GetCommandHandler(name string) (Handler, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	h, ok := d.handlers[name]
	return h, ok
}

// PipeThrough sets the pipes every job is sent through before its handler runs.
//
// It replaces the list, as Illuminate's does.
func (d *Dispatcher) PipeThrough(pipes []Pipe) *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pipes = append([]Pipe(nil), pipes...)
	return d
}

// Dispatch sends a job to the queue when there is one, and runs it here when
// there is not.
//
// It is the call an application makes and the one place the answer to "does
// this block" is decided. Illuminate decides it by whether the command
// implements ShouldQueue; here it is whether the Dispatcher was given a queue,
// because a job that sometimes blocks for two seconds and sometimes does not,
// with nothing at the call site to say which, is the thing that makes a request
// mysteriously slow.
func (d *Dispatcher) Dispatch(ctx context.Context, g auth.Grant, job Step) error {
	if d.queue != nil {
		return d.DispatchToQueue(ctx, g, job)
	}
	return d.DispatchNow(ctx, g, job)
}

// DispatchNow runs the job in this process, through the pipes.
func (d *Dispatcher) DispatchNow(ctx context.Context, g auth.Grant, job Step) error {
	if !job.declared() {
		return ErrNoName
	}
	handler, ok := d.GetCommandHandler(job.Name)
	if !ok {
		return fmt.Errorf("bus: no handler is registered for %q", job.Name)
	}

	d.mu.RLock()
	pipes := append([]Pipe(nil), d.pipes...)
	d.mu.RUnlock()

	// Applied back to front, so that the first pipe given is the outermost one
	// and sees the job before the others do.
	for _, pipe := range slices.Backward(pipes) {
		if pipe != nil {
			handler = pipe(handler)
		}
	}
	return handler(ctx, g, job.Payload)
}

// DispatchSync runs the job in this process even when a queue is wired.
//
// Illuminate sends it to the "sync" connection, which is the same thing with a
// driver in between. It is what a test uses and what a command uses when the
// answer is needed before it returns.
func (d *Dispatcher) DispatchSync(ctx context.Context, g auth.Grant, job Step) error {
	return d.DispatchNow(ctx, g, job)
}

// DispatchToQueue pushes the job, whatever the Dispatcher would otherwise do.
func (d *Dispatcher) DispatchToQueue(ctx context.Context, g auth.Grant, job Step) error {
	if !job.declared() {
		return ErrNoName
	}
	if d.queue == nil {
		return errors.New("bus: this dispatcher has no queue to push to")
	}
	return d.queue.Push(ctx, g, job.Queue, job.Name, job.Payload)
}

// DispatchAfterResponse holds the job until Terminating is called.
//
// It is for the work that must happen but that the person waiting on the
// response should not pay for: a thumbnail, a webhook, a search index update on
// a system with no queue. Illuminate hangs it on the container's terminating
// callback; there is no container here (ADR 0001), so the HTTP layer calls
// Terminating once the response has gone out.
//
// With WithoutDispatchingAfterResponses the job runs at once instead, which is
// what Illuminate does too and what a test wants.
func (d *Dispatcher) DispatchAfterResponse(ctx context.Context, g auth.Grant, job Step) error {
	d.mu.RLock()
	allowed := d.allowsDispatchingAfterResponses
	d.mu.RUnlock()
	if !allowed {
		return d.DispatchSync(ctx, g, job)
	}
	d.afterResponse(func(ctx context.Context) error { return d.DispatchSync(ctx, g, job) })
	return nil
}

// Terminating runs everything DispatchAfterResponse held, in the order it was
// held, and empties the list.
//
// It is the container's terminating hook, spelled where the callbacks live.
// Errors are joined rather than returned one at a time: the response has
// already gone out, and stopping at the first one silently drops the rest.
func (d *Dispatcher) Terminating(ctx context.Context) error {
	d.mu.Lock()
	pending := d.terminating
	d.terminating = nil
	d.mu.Unlock()

	var errs []error
	for _, run := range pending {
		errs = append(errs, run(ctx))
	}
	return errors.Join(errs...)
}

// WithDispatchingAfterResponses lets DispatchAfterResponse hold work back. It
// is the default.
func (d *Dispatcher) WithDispatchingAfterResponses() *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.allowsDispatchingAfterResponses = true
	return d
}

// WithoutDispatchingAfterResponses makes DispatchAfterResponse run the job at
// once.
func (d *Dispatcher) WithoutDispatchingAfterResponses() *Dispatcher {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.allowsDispatchingAfterResponses = false
	return d
}

// FindBatch returns a batch by id.
func (d *Dispatcher) FindBatch(ctx context.Context, g auth.Grant, batchID string) (Batch, error) {
	if d.repository == nil {
		return Batch{}, errors.New("bus: this dispatcher has no batch repository")
	}
	return d.repository.Find(ctx, g, batchID)
}

// Batch starts describing a batch of the given jobs.
func (d *Dispatcher) Batch(jobs ...Step) *PendingBatch {
	return (&PendingBatch{}).AddStep(jobs...)
}

// Chain starts describing a chain of the given jobs.
func (d *Dispatcher) Chain(jobs ...Step) *Chain {
	return NewChain().AddStep(jobs...)
}

// Repository is the batch repository this dispatcher was given, so that a
// PendingBatch built by Batch can be dispatched against the same one.
func (d *Dispatcher) Repository() BatchRepository { return d.repository }

// Queue is the queue this dispatcher was given.
func (d *Dispatcher) Queue() Queue { return d.queue }

// afterResponse records one callback for Terminating.
func (d *Dispatcher) afterResponse(run func(ctx context.Context) error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.terminating = append(d.terminating, run)
}
