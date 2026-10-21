package queue_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
)

type invoicePayload struct {
	Invoice string `json:"invoice"`
	Cents   int64  `json:"cents"`
}

// TestDispatchAndHandlerForCarryTheTypedPayload: the value handed to Dispatch is
// the value the handler receives, through the same JSON the store keeps.
func TestDispatchAndHandlerForCarryTheTypedPayload(t *testing.T) {
	w := registry()
	var got invoicePayload
	var gotJob *jobs.Job
	w.Handle("invoice.send", queue.HandlerFor(func(_ context.Context, _ auth.Grant, j *jobs.Job, p invoicePayload) error {
		got, gotJob = p, j
		return nil
	}))

	q := queue.NewSyncQueue(w)
	sent := invoicePayload{Invoice: "inv-7", Cents: 123450}
	if err := queue.Dispatch(context.Background(), q, grant(), "invoice.send", sent); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got != sent {
		t.Fatalf("the handler got %+v, want %+v", got, sent)
	}
	if gotJob == nil || gotJob.TenantID != tenant || gotJob.Queue != jobs.DefaultQueue {
		t.Fatalf("the job = %+v, want the Grant's tenant on the default queue", gotJob)
	}
}

// TestDispatchRefusesAGrantWithoutATenant: Dispatch builds the job with
// jobs.New, so it keeps the refusal a job without a tenant gets.
func TestDispatchRefusesAGrantWithoutATenant(t *testing.T) {
	q := queue.NewSyncQueue(registry())
	err := queue.Dispatch(context.Background(), q, auth.Grant{}, "invoice.send", invoicePayload{})
	if !errors.Is(err, jobs.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

// TestHandlerForFailsAPayloadThatDoesNotFit: a payload that does not decode is
// the handler's error, and the function never runs on a zero value.
func TestHandlerForFailsAPayloadThatDoesNotFit(t *testing.T) {
	called := false
	h := queue.HandlerFor(func(context.Context, auth.Grant, *jobs.Job, invoicePayload) error {
		called = true
		return nil
	})
	j := &jobs.Job{Name: "invoice.send", Payload: []byte(`{"cents":"not a number"}`)}
	err := h.Handle(context.Background(), grant(), j)
	if err == nil || !strings.Contains(err.Error(), "invoice.send") {
		t.Fatalf("err = %v, want the decode failure naming the job", err)
	}
	if called {
		t.Fatal("the handler ran on a payload that did not decode")
	}
}
