package queue

import (
	"context"
	"errors"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/jobs"
)

// BusAdapter presents a Queue through the narrow push contract consumed by the
// bus package.
//
// The adapter lives below the bus and satisfies its interface structurally, so
// neither package imports the other. The payload is already JSON and is copied
// into the job unchanged rather than encoded as a byte slice a second time.
type BusAdapter struct {
	queue Queue
}

// NewBusAdapter returns the bus-facing adapter over q.
func NewBusAdapter(q Queue) *BusAdapter { return &BusAdapter{queue: q} }

// Push creates one queue job from an already encoded bus payload.
func (a *BusAdapter) Push(ctx context.Context, g auth.Grant, queueName, name string, payload []byte) error {
	if a == nil || a.queue == nil {
		return errors.New("queue: the bus adapter has no queue")
	}

	// Drivers already expose PushRaw even though it is deliberately not part of
	// Queue: it avoids decoding and re-encoding arguments on this exact path.
	// A third-party Queue need only implement the public contract; for it the
	// fallback builds the same Job and preserves the bytes itself.
	if raw, ok := a.queue.(interface {
		PushRaw(context.Context, auth.Grant, string, []byte, string) error
	}); ok {
		return raw.PushRaw(ctx, g, name, payload, queueName)
	}

	j, err := jobs.New(g, queueName, name, nil)
	if err != nil {
		return err
	}
	j.Payload = append([]byte(nil), payload...)
	return a.queue.Push(ctx, g, j)
}
