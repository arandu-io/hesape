package log_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/log"
)

// The overhead of this package was asserted and never measured, so nothing
// could be defended and nothing could regress visibly. These are the three
// paths worth a number.
//
// # How to re-run them
//
//	go test -run '^$' -bench . -benchmem -count 10 ./log/
//
// -benchmem is not optional: the number that matters here is allocs/op, and
// ns/op on a machine with other work on it moves by more than any change under
// review. -count 10 because a single run of a benchmark this short is noise.
//
// # The budget
//
// Stated as allocations, because an allocation is the same number on every
// machine and a nanosecond is not:
//
//   - a call at a level the handler does not handle: 1 alloc/op with no shared
//     context, 2 with some. It is not zero, and
//     TestTheDisabledLevelPathAllocatesWhatItAllocates says where the
//     allocations come from and what would remove them.
//   - the same call on the *slog.Logger this package builds: 0 allocs/op. slog
//     was written for that, and it is the floor the wrapper is measured
//     against -- 4.7 ns against 32.
//   - a line that is written: 3 allocs/op.
//   - an outbound call with the Collector recording it: 1 alloc/op more than the
//     same call without one, which is the redaction and the record together.
//
// Every one of those is asserted in log_test.go, so a change past a budget is
// a red test rather than a drift somebody notices in a flame graph.

// discard is a handler at Info, writing nowhere: the benchmarks measure this
// package and the encoder, not the disk under the test machine.
func discard(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: level}))
}

// BenchmarkLoggerEnabled is a line that is written: the merge of the logger's
// own context, the message formatting, the attributes and the encoder.
func BenchmarkLoggerEnabled(b *testing.B) {
	logger := log.NewLogger(discard(slog.LevelInfo), nil)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		logger.Info(ctx, "the invoice was paid", map[string]any{"invoice": 42})
	}
}

// BenchmarkLoggerDisabled is the same call at a level the handler does not
// handle, which is what a debug line costs in production.
//
// It is the number that decides whether a debug call may be left in a hot path.
func BenchmarkLoggerDisabled(b *testing.B) {
	logger := log.NewLogger(discard(slog.LevelError), nil)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		logger.Debug(ctx, "the invoice was paid", map[string]any{"invoice": 42})
	}
}

// BenchmarkLoggerWithContextDisabled is the disabled call on a logger that
// carries context, which is what an application actually holds: the shared
// fields are copied per call, so this is where the copy shows.
func BenchmarkLoggerWithContextDisabled(b *testing.B) {
	logger := log.NewLogger(discard(slog.LevelError), nil).
		WithContext(map[string]any{"tenant": "acme", "request": "req-1"})
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		logger.Debug(ctx, "the invoice was paid", map[string]any{"invoice": 42})
	}
}

// BenchmarkSlogDisabled is the floor: the same disabled call on the
// *slog.Logger this package builds, with nothing of this package in the way.
//
// It is here so the wrapper's cost is a difference and not a bare number.
func BenchmarkSlogDisabled(b *testing.B) {
	logger := discard(slog.LevelError)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		logger.DebugContext(ctx, "the invoice was paid", "invoice", 42)
	}
}

// BenchmarkTransportRedacting is the redaction path: an outbound call with a
// Collector on the context, which is what makes the URL be rebuilt without its
// query string before it is recorded.
func BenchmarkTransportRedacting(b *testing.B) {
	transport := log.Transport(answering{})
	collected := log.WithCollector(context.Background(), log.NewCollector("req-1"))
	request, err := http.NewRequestWithContext(collected, http.MethodGet,
		"https://api.example.test/rates?token=secret&from=BRL", nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		resp, err := transport.RoundTrip(request)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// BenchmarkTransportNotRecording is the same call with no Collector on the
// context, which is every request in production that nobody is watching.
//
// The pair is the measurement: the claim this transport makes about itself is
// that it is free when nothing is collecting, and the difference between these
// two is that claim as a number.
func BenchmarkTransportNotRecording(b *testing.B) {
	transport := log.Transport(answering{})
	request, err := http.NewRequest(http.MethodGet,
		"https://api.example.test/rates?token=secret&from=BRL", nil)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		resp, err := transport.RoundTrip(request)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

// answering is a round tripper that answers without a network, so the
// benchmarks above measure the transport and not a socket.
type answering struct{}

func (answering) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
}
