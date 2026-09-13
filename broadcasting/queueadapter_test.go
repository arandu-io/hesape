package broadcasting_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

const queuedTenant = "11111111-1111-4111-8111-111111111111"

func queuedGrant() auth.Grant { return auth.SystemGrant("invoice.sent", queuedTenant) }

type queuedInvoiceSent struct {
	broadcasting.InteractsWithBroadcasting
	Connection string `json:"-"`
	Queue      string `json:"-"`
	InvoiceID  string `json:"invoice_id"`
}

func (queuedInvoiceSent) BroadcastOn() []broadcasting.Channel {
	return []broadcasting.Channel{broadcasting.NewPrivateChannel("invoices.17")}
}

func (queuedInvoiceSent) BroadcastAs() string { return "InvoiceSent" }

func (queuedInvoiceSent) Tries() int { return 3 }

func (queuedInvoiceSent) Backoff() time.Duration { return 12 * time.Second }

type queuedPublication struct {
	grant    auth.Grant
	channels []broadcasting.Channel
	event    string
	payload  map[string]any
}

type queuedBroadcastRecorder struct {
	mu        sync.Mutex
	published []queuedPublication
}

func (*queuedBroadcastRecorder) Auth(context.Context, string) (auth.Grant, any, error) {
	return auth.Grant{}, nil, nil
}

func (*queuedBroadcastRecorder) ValidAuthenticationResponse(context.Context, auth.Grant, broadcasting.Channel, any) (any, error) {
	return nil, nil
}

func (b *queuedBroadcastRecorder) Broadcast(_ context.Context, g auth.Grant, channels []broadcasting.Channel, event string, payload map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, queuedPublication{
		grant: g, channels: append([]broadcasting.Channel(nil), channels...), event: event, payload: payload,
	})
	return nil
}

func (b *queuedBroadcastRecorder) all() []queuedPublication {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]queuedPublication(nil), b.published...)
}

type queuedBroadcastFactory struct{ driver broadcasting.Broadcaster }

func (f queuedBroadcastFactory) Connection(string) (broadcasting.Broadcaster, error) {
	return f.driver, nil
}

type queuedJobRecorder struct {
	queue.Queue
	job jobs.Job
}

func (r *queuedJobRecorder) Push(ctx context.Context, g auth.Grant, job jobs.Job) error {
	r.job = job
	return r.Queue.Push(ctx, g, job)
}

// TestAQueuedBroadcastIsResolvedEncodedDecodedAndPublished proves the complete
// bridge. Resolving before JSON is load-bearing: decoding Event as any would
// produce a map and lose BroadcastOn, BroadcastAs and every other Go method.
func TestAQueuedBroadcastIsResolvedEncodedDecodedAndPublished(t *testing.T) {
	worker := queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
	driver := &queuedBroadcastRecorder{}
	queues := queue.NewQueueManager()
	adapter := broadcasting.NewQueueAdapter(queues, queuedBroadcastFactory{driver: driver})
	worker.Handle(broadcasting.BroadcastJobName, adapter)
	queued := &queuedJobRecorder{Queue: queue.NewSyncQueue(worker)}
	queues.Extend("sync", queued)

	manager := broadcasting.NewBroadcastManager(broadcasting.Config{}, nil, adapter, nil)
	event := &queuedInvoiceSent{Connection: "sync", Queue: "realtime", InvoiceID: "inv-17"}
	event.BroadcastVia("joaju")

	if err := manager.Queue(context.Background(), queuedGrant(), event); err != nil {
		t.Fatalf("Queue: %v", err)
	}

	all := driver.all()
	if len(all) != 1 {
		t.Fatalf("the consumer published %d broadcasts, want one", len(all))
	}
	if all[0].event != "InvoiceSent" {
		t.Errorf("event = %q, want InvoiceSent", all[0].event)
	}
	if len(all[0].channels) != 1 || all[0].channels[0].Name != "private-invoices.17" {
		t.Errorf("channels = %+v", all[0].channels)
	}
	if all[0].payload["invoice_id"] != "inv-17" {
		t.Errorf("payload = %+v", all[0].payload)
	}
	if auth.Tenant(all[0].grant) != queuedTenant {
		t.Errorf("the consumer received a Grant for tenant %q", auth.Tenant(all[0].grant))
	}
	if queued.job.Queue != "realtime" || queued.job.Attributes.Connection != "sync" {
		t.Errorf("job was routed to queue %q on connection %q", queued.job.Queue, queued.job.Attributes.Connection)
	}
	if queued.job.Attributes.Tries != 3 || len(queued.job.Attributes.Backoff) != 1 || queued.job.Attributes.Backoff[0] != 12*time.Second {
		t.Errorf("job attributes = %+v", queued.job.Attributes)
	}
}

var _ queue.Handler = (*broadcasting.QueueAdapter)(nil)
