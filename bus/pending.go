package bus

import (
	"context"
	"fmt"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// PendingBatch is a batch being described, before anything is written.
//
// It is a builder and it collects the first error it makes rather than
// returning one per call: Add cannot both chain and return an error, and a
// builder whose every step has to be checked is a builder nobody uses. Dispatch
// is where the error surfaces, and Dispatch is the call that could act on it
// anyway.
type PendingBatch struct {
	name          string
	queue         string
	jobs          []Step
	then          Step
	catch         Step
	finally       Step
	allowFailures bool
	err           error
}

// NewBatch starts describing a batch. The name is for people -- it is what the
// batch is called on a dashboard -- and nothing looks a batch up by it.
func NewBatch(name string) *PendingBatch {
	return &PendingBatch{name: name}
}

// Add puts a job in the batch. The payload is serialized here, so a value that
// cannot be is refused at the call site that built it rather than in a worker.
func (p *PendingBatch) Add(name string, payload any) *PendingBatch {
	s, err := step("", name, payload)
	if err != nil {
		p.fail(err)
		return p
	}
	p.jobs = append(p.jobs, s)
	return p
}

// Then names the job dispatched when every job in the batch succeeded.
func (p *PendingBatch) Then(name string, payload any) *PendingBatch {
	return p.callback(&p.then, name, payload)
}

// Catch names the job dispatched on the first failure, and only the first.
//
// It fires whether or not failures are allowed: AllowFailures decides what
// happens to the other jobs, not whether anybody is told.
func (p *PendingBatch) Catch(name string, payload any) *PendingBatch {
	return p.callback(&p.catch, name, payload)
}

// Finally names the job dispatched when the batch stops, whichever way it
// stopped.
func (p *PendingBatch) Finally(name string, payload any) *PendingBatch {
	return p.callback(&p.finally, name, payload)
}

// AllowFailures lets the rest of the batch continue after a job fails.
//
// Without it the first failure stops the batch: the jobs already queued are
// delivered, ask Member.Cancelled, and skip their work. That is the default
// because a batch is usually one operation split up, and finishing half of an
// operation is worse than finishing none of it. An import that is genuinely
// row-by-row independent is the case this exists for.
func (p *PendingBatch) AllowFailures() *PendingBatch {
	p.allowFailures = true
	return p
}

// OnQueue is which queue the jobs and the callbacks go on.
func (p *PendingBatch) OnQueue(queue string) *PendingBatch {
	p.queue = queue
	return p
}

// Dispatch writes the batch and pushes its jobs.
//
// The batch row is written before the first job, because a job that arrives
// before its batch exists has nothing to report to -- and with a worker already
// running, "before" is measured in microseconds.
//
// Inside database.Transaction, with the table queue, the row and the jobs
// commit together and a rollback takes all of it. With a queue that cannot join
// the transaction, a push that fails partway leaves jobs already queued against
// a batch whose remaining jobs will never arrive; that batch is cancelled before
// the error is returned, so the ones already queued skip their work instead of
// doing half an import.
func (p *PendingBatch) Dispatch(ctx context.Context, g auth.Grant, s Store, q Queue) (Batch, error) {
	if p.err != nil {
		return Batch{}, p.err
	}
	if len(p.jobs) == 0 {
		return Batch{}, ErrEmptyBatch
	}
	if s == nil || q == nil {
		return Batch{}, fmt.Errorf("bus: dispatching the batch %q needs a store and a queue", p.name)
	}
	tenant, err := tenantOf(g)
	if err != nil {
		return Batch{}, err
	}
	id, err := database.NewID()
	if err != nil {
		return Batch{}, err
	}

	b := Batch{
		ID:            id,
		Name:          p.name,
		TenantID:      tenant,
		Queue:         p.queue,
		Total:         len(p.jobs),
		Pending:       len(p.jobs),
		AllowFailures: p.allowFailures,
		Then:          p.then,
		Catch:         p.catch,
		Finally:       p.finally,
	}
	if err := s.Create(ctx, g, b); err != nil {
		return Batch{}, err
	}

	for _, j := range p.jobs {
		payload, err := wrap(envelope{Bus: formatVersion, Batch: id, Body: j.Payload})
		if err != nil {
			return Batch{}, p.abandon(ctx, g, s, id, err)
		}
		if err := q.Push(ctx, g, p.queueFor(j), j.Name, payload); err != nil {
			return Batch{}, p.abandon(ctx, g, s, id,
				fmt.Errorf("bus: pushing %s of batch %s: %w", j.Name, id, err))
		}
	}

	return b, nil
}

// queueFor is where a step goes: its own queue, or the batch's.
func (p *PendingBatch) queueFor(s Step) string {
	if s.Queue != "" {
		return s.Queue
	}
	return p.queue
}

// abandon cancels a batch that could not be fully dispatched, and keeps the
// original error whatever the cancel does. A cancel that also fails leaves a
// batch that never finishes, and saying so is more useful than replacing the
// error that caused it.
func (p *PendingBatch) abandon(ctx context.Context, g auth.Grant, s Store, id string, cause error) error {
	if err := s.Cancel(ctx, g, id); err != nil {
		return fmt.Errorf("%w (and cancelling the batch failed too: %v)", cause, err)
	}
	return cause
}

// callback fills one of the three callback slots.
func (p *PendingBatch) callback(slot *Step, name string, payload any) *PendingBatch {
	s, err := step("", name, payload)
	if err != nil {
		p.fail(err)
		return p
	}
	*slot = s
	return p
}

// fail keeps the first error. The first is the one with the cause in it; the
// ones after it are usually the same value refusing to serialize again.
func (p *PendingBatch) fail(err error) {
	if p.err == nil {
		p.err = err
	}
}
