package http_test

import (
	"testing"
	"time"

	httpx "github.com/arandu-io/hesape/http"
)

func TestStreamedEventKeepsItsExistingWireFormat(t *testing.T) {
	event := httpx.NewStreamedEvent("invoice.updated", "first\r\nsecond\rthird")
	want := "event: invoice.updated\ndata: first\ndata: second\ndata: third\n\n"
	if got := event.String(); got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
}

func TestServerSentEventFormatsIDRetryCommentsAndJSONData(t *testing.T) {
	event := httpx.NewServerSentEvent("evt-42", "invoice.updated", map[string]any{"total": 42}, 1500*time.Millisecond)
	event.Comment = "keep\r\nalive"

	want := ": keep\n: alive\nid: evt-42\nevent: invoice.updated\nretry: 1500\ndata: {\"total\":42}\n\n"
	if got := event.String(); got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
}

func TestServerSentEventDoesNotAllowMetadataLineInjection(t *testing.T) {
	event := httpx.NewServerSentEvent("safe\nid", "safe\nevent", "payload", 0)
	want := "id: safe id\nevent: safe event\ndata: payload\n\n"
	if got := event.String(); got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
}

func TestServerSentEventOmitsAnIDContainingNull(t *testing.T) {
	event := httpx.NewServerSentEvent("unsafe\x00id", "update", "payload", 0)
	want := "event: update\ndata: payload\n\n"
	if got := event.String(); got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
}

func TestCommentOnlyServerSentEventHasNoDataField(t *testing.T) {
	event := &httpx.ServerSentEvent{Comment: "heartbeat"}
	if got, want := event.String(), ": heartbeat\n\n"; got != want {
		t.Fatalf("event = %q, want %q", got, want)
	}
}
