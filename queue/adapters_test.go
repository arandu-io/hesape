package queue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/bus"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

// TestTheBusAdapterCarriesEncodedArgumentsThroughTheQueueToTheConsumer proves
// the whole structural bridge. Before the adapter, bus.Queue claimed that the
// Hesape queue satisfied it, but their Push signatures were different and no
// value could be passed between them.
func TestTheBusAdapterCarriesEncodedArgumentsThroughTheQueueToTheConsumer(t *testing.T) {
	type invoice struct {
		ID string `json:"id"`
	}

	var consumed invoice
	worker := queue.NewWorker(queue.NullQueue{}, queue.WorkerOptions{})
	worker.HandleFunc("invoice.send", func(_ context.Context, _ auth.Grant, j *jobs.Job) error {
		return j.Decode(&consumed)
	})

	synchronous := queue.NewSyncQueue(worker)
	dispatcher := bus.NewDispatcher(queue.NewBusAdapter(synchronous), nil)
	payload, err := json.Marshal(invoice{ID: "inv-17"})
	if err != nil {
		t.Fatal(err)
	}

	if err := dispatcher.Dispatch(context.Background(), grant(), bus.Step{
		Queue: "notifications", Name: "invoice.send", Payload: payload,
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if consumed.ID != "inv-17" {
		t.Errorf("the consumer decoded invoice %q, want inv-17", consumed.ID)
	}
}

// TestTheBusAdapterPreservesRawPayloadWithAContractOnlyQueue covers the
// fallback used by third-party Queue implementations that do not expose the
// optional PushRaw optimization.
func TestTheBusAdapterPreservesRawPayloadWithAContractOnlyQueue(t *testing.T) {
	recorder := &jobRecorder{Queue: queue.NullQueue{}}
	payload := []byte(`{"id":"inv-23"}`)

	if err := queue.NewBusAdapter(recorder).Push(
		context.Background(), grant(), "reports", "invoice.report", payload,
	); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if recorder.job.Name != "invoice.report" || recorder.job.Queue != "reports" {
		t.Errorf("the job was routed as %s on %s", recorder.job.Name, recorder.job.Queue)
	}
	if string(recorder.job.Payload) != string(payload) {
		t.Errorf("payload = %q, want %q", recorder.job.Payload, payload)
	}
}

type jobRecorder struct {
	queue.Queue
	job jobs.Job
}

func (r *jobRecorder) Push(_ context.Context, _ auth.Grant, j jobs.Job) error {
	r.job = j
	return nil
}
