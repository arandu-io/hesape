package log_test

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"testing"

	"github.com/arandu-io/hesape/log"
)

// raceHeadroom is what the race detector adds to a written line, and zero when
// it is not running.
//
// The budgets below are about this package, and under the detector the counter
// answers partly about the instrumentation: a written line reads four
// allocations under it and three without, while a call that is never written
// reads the same either way. Skipping the measurement under -race would be
// simpler and would mean the budgets never run in a suite that is always run
// with it, so the one number that moves gets the headroom and every other
// assertion stays exact.
func raceHeadroom() float64 {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return 0
	}
	for _, setting := range info.Settings {
		if setting.Key == "-race" && setting.Value == "true" {
			return 1
		}
	}
	return 0
}

// TestTheDisabledLevelPathAllocatesWhatItAllocates fixes the cost of a call at a
// level the handler does not handle, which is what a debug line left in a hot
// path costs in production.
//
// It is not zero, and that is the finding rather than the assertion. slog was
// written so that a disabled call allocates nothing, and the *slog.Logger this
// package builds does exactly that -- the floor below is 0. The wrapper around
// it allocates once for a logger with no shared context and twice for one that
// has some, because the map that merges the logger's context with the call's is
// built before the handler is asked whether it wants the line at all. Moving
// that question above the merge would make both zero.
//
// Written down as a number so a change either keeps it or has to argue with
// this test. The budget is the measurement plus nothing: a call that starts
// allocating a third time has grown a cost nobody decided on.
func TestTheDisabledLevelPathAllocatesWhatItAllocates(t *testing.T) {
	ctx := context.Background()

	for _, c := range []struct {
		name   string
		logger *log.Logger
		want   float64
	}{
		{
			name:   "no shared context",
			logger: log.NewLogger(discard(slog.LevelError), nil),
			want:   1,
		},
		{
			name: "with shared context",
			logger: log.NewLogger(discard(slog.LevelError), nil).
				WithContext(map[string]any{"tenant": "acme", "request": "req-1"}),
			want: 2,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := testing.AllocsPerRun(1000, func() {
				c.logger.Debug(ctx, "the invoice was paid", map[string]any{"invoice": 42})
			})
			if got != c.want {
				t.Errorf("a disabled call allocates %v times, and the budget is %v. "+
					"Going up is a cost nobody decided on; going down is this test being "+
					"rewritten to the new floor.", got, c.want)
			}
		})
	}
}

// TestTheEnabledPathStaysWithinItsBudget fixes the cost of a line that is
// actually written, so that a change to the merge or to the attributes shows up
// here rather than in somebody's throughput.
func TestTheEnabledPathStaysWithinItsBudget(t *testing.T) {
	ctx := context.Background()

	budget := 3 + raceHeadroom()
	logger := log.NewLogger(discard(slog.LevelInfo), nil)
	got := testing.AllocsPerRun(1000, func() {
		logger.Info(ctx, "the invoice was paid", map[string]any{"invoice": 42})
	})
	if got > budget {
		t.Errorf("a written line allocates %v times, and the budget is %v", got, budget)
	}
}

// TestRecordingAnOutboundCallCostsOneAllocationMoreThanNotRecording fixes the
// price of the redaction path.
//
// The transport says of itself that it costs nothing when nothing is
// collecting, and the pair below is that claim as a number: with no Collector
// on the context it allocates once, which is the response; with one, twice,
// which is the record. The URL is rebuilt without its query string on the
// second path only, and it used to be rebuilt on both -- that is the shape of
// regression this exists to catch.
func TestRecordingAnOutboundCallCostsOneAllocationMoreThanNotRecording(t *testing.T) {
	ctx := context.Background()

	transport := log.Transport(answering{})
	const target = "https://api.example.test/rates?token=secret&from=BRL"

	plain, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	collected, err := http.NewRequestWithContext(
		log.WithCollector(ctx, log.NewCollector("req-1")), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}

	roundTrip := func(r *http.Request) func() {
		return func() {
			resp, err := transport.RoundTrip(r)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		}
	}

	quiet := testing.AllocsPerRun(1000, roundTrip(plain))
	recording := testing.AllocsPerRun(1000, roundTrip(collected))

	if quiet != 1 {
		t.Errorf("an outbound call with nobody collecting allocates %v times, and the budget is 1", quiet)
	}
	if recording-quiet > 1 {
		t.Errorf("recording an outbound call costs %v allocations more than not recording, "+
			"and the budget is 1", recording-quiet)
	}
}
