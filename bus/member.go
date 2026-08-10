package bus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/auth"
)

// formatVersion marks a payload as one this package wrote.
//
// It is what tells a batch member apart from an ordinary job whose arguments
// happen to be a JSON object, and it is a number rather than a bool because the
// day the envelope changes shape, a worker running the old binary has to be
// able to say so instead of silently reading the new one as the old.
const formatVersion = 1

// envelope is what bus puts around a job's arguments.
//
// The remaining links of a chain travel in it, which is why a chain needs no
// table: the chain is exactly as durable as the queue, and an abandoned one
// leaves nothing behind to clean up.
//
// Catch carries no omitempty because encoding/json has none for a struct: a
// zero Step encodes as a name nobody set, which is exactly what "this chain has
// no Catch" means, and declared() is what reads it back.
type envelope struct {
	Bus   int             `json:"bus"`
	Batch string          `json:"batch,omitempty"`
	Chain []Step          `json:"chain,omitempty"`
	Catch Step            `json:"chain_catch"`
	Body  json.RawMessage `json:"body,omitempty"`
}

// wrap serializes an envelope.
func wrap(e envelope) ([]byte, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("bus: serializing the envelope: %w", err)
	}
	return raw, nil
}

// Member is what a job belongs to: a batch, a chain, both, or neither.
//
// The zero value is a job that was pushed on its own, and Handled on it does
// nothing. That is deliberate: a handler can call Batching and Handled
// unconditionally, and a job that was never part of anything costs one failed
// type assertion for the privilege of not having two versions of every handler.
type Member struct {
	// Batch is the batch the job belongs to, empty when it belongs to none.
	Batch string
	// rest is the links of the chain that have not run yet, and catch is the
	// job that reports a chain that stopped. They are unexported because
	// nothing outside this package should be able to rewrite the tail of a
	// chain in flight.
	rest  []Step
	catch Step
}

// Chained reports whether more links follow this one.
func (m Member) Chained() bool { return len(m.rest) > 0 }

// Cancelled reports whether the work should be skipped.
//
// A handler asks before it does anything expensive. It is how cancelling a
// batch means something: the jobs already in the queue cannot be recalled, so
// they are delivered, they ask, and they skip. A batch that stopped on a
// failure answers yes for the same reason -- there is nothing left for the rest
// of the batch to contribute to.
//
// A job in no batch is never cancelled and the store is not consulted.
func (m Member) Cancelled(ctx context.Context, g auth.Grant, s Store) (bool, error) {
	if m.Batch == "" {
		return false, nil
	}
	if s == nil {
		return false, fmt.Errorf("bus: asking whether batch %s is cancelled needs a store", m.Batch)
	}
	b, err := s.Find(ctx, g, m.Batch)
	if err != nil {
		return false, err
	}
	return b.Cancelled() || b.Finished(), nil
}

// Batching reports what the job belongs to and decodes its arguments into v.
//
// It is Laravel's Batchable trait, in the shape a Go handler can use: one call,
// at the top, whether or not the job turned out to be part of anything.
//
//	var row Invoice
//	m, err := bus.Batching(j.Payload, &row)
//
// A payload this package did not write is returned as it is, with a zero
// Member, so a handler written for batches still works on a job pushed directly.
// Pass nil for v when the arguments are not wanted.
func Batching(payload []byte, v any) (Member, error) {
	var e envelope
	if err := json.Unmarshal(payload, &e); err != nil || e.Bus == 0 {
		// Not ours. The payload is the arguments.
		return Member{}, decodeInto(payload, v)
	}
	if e.Bus != formatVersion {
		return Member{}, fmt.Errorf("bus: this payload was written in envelope format %d and this binary reads %d", e.Bus, formatVersion)
	}
	return Member{Batch: e.Batch, rest: e.Chain, catch: e.Catch}, decodeInto(e.Body, v)
}

// decodeInto unmarshals raw into v, tolerating both being absent.
func decodeInto(raw []byte, v any) error {
	if v == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("bus: decoding the job payload: %w", err)
	}
	return nil
}

// Handled reports one job's outcome and does whatever that outcome triggers.
//
// It is the second of the two calls a handler makes, and the one that has to
// happen on every path out of it -- including the failing one, which is why it
// takes the error rather than being called only on success:
//
//	err := doTheWork(ctx, g, row)
//	return errors.Join(err, bus.Handled(ctx, g, store, queue, m, err))
//
// For a batch it decrements the counter and fires Catch on the first failure,
// Then when everything succeeded and Finally when the batch stops -- each
// exactly once, because the store decides, not this function.
//
// For a chain it pushes the next link when the job succeeded, and the chain's
// Catch when it did not. A chain stops at its first failure and does not
// resume: the point of a chain is that link N assumes link N-1 happened.
//
// A job that belongs to neither does nothing and returns nil.
func Handled(ctx context.Context, g auth.Grant, s Store, q Queue, m Member, cause error) error {
	if m.Batch == "" && !m.Chained() && !m.catch.declared() {
		return nil
	}
	if q == nil {
		return errors.New("bus: reporting a job needs a queue to push what the outcome triggers")
	}

	var errs []error
	if m.Batch != "" {
		errs = append(errs, handledBatch(ctx, g, s, q, m.Batch, cause))
	}
	errs = append(errs, handledChain(ctx, g, q, m, cause))
	return errors.Join(errs...)
}

// handledBatch is the batch half: record, then fire what the record changed.
func handledBatch(ctx context.Context, g auth.Grant, s Store, q Queue, id string, cause error) error {
	if s == nil {
		return fmt.Errorf("bus: reporting a job of batch %s needs a store", id)
	}
	r, err := s.Record(ctx, g, id, cause == nil)
	if err != nil {
		return err
	}

	var errs []error
	if r.FirstFailure && r.Batch.Catch.declared() {
		errs = append(errs, push(ctx, g, q, r.Batch, r.Batch.Catch))
	}
	if r.Finished {
		// Then is the success callback, and a cancelled batch did not succeed
		// even when every job that ran did: something asked for it to stop.
		if !r.Batch.HasFailures() && !r.Batch.Cancelled() && r.Batch.Then.declared() {
			errs = append(errs, push(ctx, g, q, r.Batch, r.Batch.Then))
		}
		if r.Batch.Finally.declared() {
			errs = append(errs, push(ctx, g, q, r.Batch, r.Batch.Finally))
		}
	}
	return errors.Join(errs...)
}

// handledChain is the chain half: advance, or report that it stopped.
func handledChain(ctx context.Context, g auth.Grant, q Queue, m Member, cause error) error {
	if cause != nil {
		if !m.catch.declared() {
			return nil
		}
		return q.Push(ctx, g, m.catch.Queue, m.catch.Name, m.catch.Payload)
	}
	if !m.Chained() {
		return nil
	}

	next, rest := m.rest[0], m.rest[1:]
	payload, err := wrap(envelope{Bus: formatVersion, Chain: rest, Catch: m.catch, Body: next.Payload})
	if err != nil {
		return err
	}
	if err := q.Push(ctx, g, next.Queue, next.Name, payload); err != nil {
		return fmt.Errorf("bus: pushing the next link %s: %w", next.Name, err)
	}
	return nil
}

// push sends a callback as an ordinary job.
//
// A callback carries no envelope: it is not part of the batch it closes, and a
// callback that reported to its own batch would decrement a counter that has
// already reached zero.
func push(ctx context.Context, g auth.Grant, q Queue, b Batch, s Step) error {
	queue := s.Queue
	if queue == "" {
		queue = b.Queue
	}
	if err := q.Push(ctx, g, queue, s.Name, s.Payload); err != nil {
		return fmt.Errorf("bus: pushing the callback %s of batch %s: %w", s.Name, b.ID, err)
	}
	return nil
}
