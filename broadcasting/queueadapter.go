package broadcasting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/attributes"
	"github.com/arandu-io/hesape/queue/jobs"
)

// BroadcastJobName is the queue routing name of every queued broadcast.
const BroadcastJobName = "broadcasting.broadcast"

// QueueAdapter pushes resolved broadcasts through a QueueManager and consumes
// them as a queue Handler.
//
// Register the same value under [BroadcastJobName] on the worker that drains
// the target queue. Keeping both halves on one value prevents the producer's
// envelope and the consumer's decoder from drifting apart.
type QueueAdapter struct {
	queues     *queue.QueueManager
	broadcasts Factory
}

// NewQueueAdapter returns the bridge between broadcast and queue connections.
func NewQueueAdapter(queues *queue.QueueManager, broadcasts Factory) *QueueAdapter {
	return &QueueAdapter{queues: queues, broadcasts: broadcasts}
}

var (
	_ Queue         = (*QueueAdapter)(nil)
	_ queue.Handler = (*QueueAdapter)(nil)
)

// PushOn resolves and queues a BroadcastEvent or UniqueBroadcastEvent.
func (a *QueueAdapter) PushOn(ctx context.Context, g auth.Grant, connection, queueName string, job any) error {
	if a == nil || a.queues == nil {
		return errors.New("broadcasting: no queue manager was configured")
	}

	broadcast, event, err := queuedSnapshot(job)
	if err != nil {
		return err
	}
	broadcast.jobAttributes = broadcastQueueAttributes(event, connection, queueName)
	record, err := queue.CreatePayload(g, connection, queueName, BroadcastJobName, broadcast, 0)
	if err != nil {
		return err
	}

	target, err := a.queues.Connection(connection)
	if err != nil {
		return err
	}
	return target.Push(ctx, g, record)
}

func (b queuedBroadcast) QueueAttributes() attributes.Attributes { return b.jobAttributes }

// Handle decodes and publishes one queued broadcast.
func (a *QueueAdapter) Handle(ctx context.Context, g auth.Grant, job *jobs.Job) error {
	if a == nil || a.broadcasts == nil {
		return errors.New("broadcasting: no broadcast factory was configured")
	}
	var broadcast queuedBroadcast
	if err := job.Decode(&broadcast); err != nil {
		return err
	}
	return broadcast.handle(ctx, g, a.broadcasts)
}

func queuedSnapshot(job any) (queuedBroadcast, *BroadcastEvent, error) {
	var event *BroadcastEvent
	switch value := job.(type) {
	case *BroadcastEvent:
		event = value
	case *UniqueBroadcastEvent:
		if value != nil {
			event = &value.BroadcastEvent
		}
	default:
		return queuedBroadcast{}, nil, fmt.Errorf(
			"broadcasting: cannot queue %T; want *BroadcastEvent or *UniqueBroadcastEvent", job,
		)
	}
	if event == nil {
		return queuedBroadcast{}, nil, errors.New("broadcasting: cannot queue a nil broadcast event")
	}

	broadcast, err := event.snapshot()
	return broadcast, event, err
}

func broadcastQueueAttributes(event *BroadcastEvent, connection, queueName string) attributes.Attributes {
	var backoff []time.Duration
	if event.Backoff != 0 {
		backoff = []time.Duration{event.Backoff}
	}
	return attributes.Attributes{
		Backoff:                 backoff,
		Connection:              connection,
		DeleteWhenMissingModels: event.DeleteWhenMissingModels,
		MaxExceptions:           event.MaxExceptions,
		Queue:                   queueName,
		Timeout:                 event.Timeout,
		Tries:                   event.Tries,
	}
}
