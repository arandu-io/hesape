package routing_test

import (
	"context"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	httpx "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/routing"
)

func TestEventStreamKeepsTheExistingAPIAndStreamsMultipleEvents(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)
	response := factory.EventStream(func(yield func(any) bool) {
		if !yield(httpx.NewStreamedEvent("created", "first\nsecond")) {
			return
		}
		yield(map[string]any{"id": 42})
	}, http.Header{"X-Test": []string{"kept"}}, nil)

	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	if err := response.SendContent(recorder, httptest.NewRequest(http.MethodGet, "/events", nil)); err != nil {
		t.Fatalf("SendContent returned %v", err)
	}

	want := "event: created\ndata: first\ndata: second\n\nevent: update\ndata: {\"id\":42}\n\n"
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
	if got := recorder.Header().Get("X-Test"); got != "kept" {
		t.Errorf("X-Test = %q, want kept", got)
	}
	if recorder.flushes < 3 {
		t.Errorf("flushes = %d, want at least initial headers plus each event", recorder.flushes)
	}
}

func TestEventStreamSupportsCompleteEventMetadata(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)
	response := factory.EventStream(func(yield func(any) bool) {
		event := httpx.NewServerSentEvent("evt-7", "invoice.updated", "paid", 2*time.Second)
		event.Comment = "checkpoint"
		yield(event)
	}, nil, nil)

	recorder := httptest.NewRecorder()
	if err := response.SendContent(recorder, httptest.NewRequest(http.MethodGet, "/events", nil)); err != nil {
		t.Fatalf("SendContent returned %v", err)
	}
	want := ": checkpoint\nid: evt-7\nevent: invoice.updated\nretry: 2000\ndata: paid\n\n"
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestEventStreamHeartbeatIsWrittenWhileTheProducerWaits(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)
	started := make(chan struct{})
	release := make(chan struct{})
	response := factory.EventStreamWithOptions(func(func(any) bool) {
		close(started)
		<-release
	}, nil, routing.EventStreamOptions{Heartbeat: time.Millisecond}, nil)

	writer := newObservedWriter()
	done := make(chan error, 1)
	go func() {
		done <- response.SendContent(writer, httptest.NewRequest(http.MethodGet, "/events", nil))
	}()
	<-started

	select {
	case <-writer.heartbeat:
	case <-time.After(time.Second):
		t.Fatal("event stream did not emit a heartbeat while its producer waited")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("SendContent returned %v", err)
	}
	if body := writer.bodyString(); !strings.Contains(body, ": heartbeat\n\n") {
		t.Fatalf("body = %q, want a heartbeat comment", body)
	}
}

func TestEventStreamCancellationStopsTheProducerAndOmitsTheClosingEvent(t *testing.T) {
	factory := routing.NewResponseFactory(nil, nil)
	producerStopped := make(chan struct{})
	response := factory.EventStream(iter.Seq[any](func(yield func(any) bool) {
		defer close(producerStopped)
		for i := 0; ; i++ {
			if !yield(i) {
				return
			}
		}
	}), nil)

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	writer := newObservedWriter()
	done := make(chan error, 1)
	go func() { done <- response.SendContent(writer, request) }()

	select {
	case <-writer.wrote:
	case <-time.After(time.Second):
		t.Fatal("event stream did not write its first event")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("SendContent returned %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("event stream did not stop after cancellation")
	}
	select {
	case <-producerStopped:
	case <-time.After(time.Second):
		t.Fatal("event stream producer did not observe yield returning false")
	}
	if body := writer.bodyString(); strings.Contains(body, "</stream>") {
		t.Fatalf("canceled stream wrote its closing event: %q", body)
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (r *flushRecorder) Flush() {
	r.flushes++
	r.ResponseRecorder.Flush()
}

type observedWriter struct {
	mu        sync.Mutex
	header    http.Header
	body      strings.Builder
	status    int
	wrote     chan struct{}
	heartbeat chan struct{}
	onceWrite sync.Once
	onceBeat  sync.Once
}

func newObservedWriter() *observedWriter {
	return &observedWriter{
		header:    http.Header{},
		wrote:     make(chan struct{}),
		heartbeat: make(chan struct{}),
	}
}

func (w *observedWriter) Header() http.Header { return w.header }

func (w *observedWriter) WriteHeader(status int) { w.status = status }

func (w *observedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onceWrite.Do(func() { close(w.wrote) })
	if strings.Contains(string(p), ": heartbeat\n\n") {
		w.onceBeat.Do(func() { close(w.heartbeat) })
	}
	return w.body.Write(p)
}

func (w *observedWriter) Flush() {}

func (w *observedWriter) bodyString() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}
