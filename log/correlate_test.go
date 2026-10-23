package log_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/log"
)

// TestALineLoggedUnderACollectorIsRecordedByIt holds both orders a request can
// install the two in: the logger first and the collector after it, which is
// what Observe does, and the collector first, which is what a trace wrapped
// around the whole application does before the kernel installs its logger.
func TestALineLoggedUnderACollectorIsRecordedByIt(t *testing.T) {
	for name, install := range map[string]func(context.Context, *slog.Logger, *log.Collector) context.Context{
		"logger then collector": func(ctx context.Context, l *slog.Logger, c *log.Collector) context.Context {
			return log.WithCollector(log.Into(ctx, l), c)
		},
		"collector then logger": func(ctx context.Context, l *slog.Logger, c *log.Collector) context.Context {
			return log.Into(log.WithCollector(ctx, c), l)
		},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			col := log.NewCollector("req-1")
			ctx := install(context.Background(), slog.New(slog.NewTextHandler(&out, nil)), col)
			ctx = log.With(ctx, "request_id", "req-1")
			log.For(ctx).WithGroup("invoice").Info("issued", "id", "inv-1")

			lines := col.Logs()
			if len(lines) != 1 {
				t.Fatalf("the collector recorded %d lines, want 1", len(lines))
			}
			got := lines[0]
			if got.Message != "issued" || got.Level != slog.LevelInfo || got.Attrs["request_id"] != "req-1" || got.Attrs["invoice.id"] != "inv-1" {
				t.Fatalf("recorded %+v", got)
			}
			if !strings.Contains(out.String(), "issued") {
				t.Fatalf("the line did not reach the logger's own handler: %q", out.String())
			}
		})
	}
}

// TestALineIsRecordedOnceByTheCollectorTheRequestHoldsNow: a second collector
// installed further in -- the application's own, under a trace that installed
// one outside it -- takes the lines from there on, and none is recorded twice.
func TestALineIsRecordedOnceByTheCollectorTheRequestHoldsNow(t *testing.T) {
	outer, inner := log.NewCollector("req-1"), log.NewCollector("req-1")
	ctx := log.Into(log.WithCollector(context.Background(), outer), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	log.For(ctx).Info("before")
	ctx = log.Into(ctx, log.For(ctx))
	ctx = log.WithCollector(ctx, inner)
	ctx = log.Into(ctx, log.For(ctx).With("k", "v"))
	log.For(ctx).Info("after")

	if lines := outer.Logs(); len(lines) != 1 || lines[0].Message != "before" {
		t.Fatalf("the outer collector recorded %+v, want only the line before the inner one existed", lines)
	}
	if lines := inner.Logs(); len(lines) != 1 || lines[0].Message != "after" || lines[0].Attrs["k"] != "v" {
		t.Fatalf("the inner collector recorded %+v, want only the line after it", lines)
	}
}

// TestALineBelowTheLevelIsNotRecorded: the collector records what the logger
// writes, and a filtered line is not written.
func TestALineBelowTheLevelIsNotRecorded(t *testing.T) {
	col := log.NewCollector("req-1")
	ctx := log.WithCollector(log.Into(context.Background(), slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}))), col)
	log.For(ctx).Info("quiet")
	log.For(ctx).Warn("loud")
	if lines := col.Logs(); len(lines) != 1 || lines[0].Message != "loud" {
		t.Fatalf("recorded %+v, want only the warning", lines)
	}
}

// TestWithoutACollectorTheLoggerIsStoredAsGiven is the production half: no
// wrapper, no recording, the same logger back.
func TestWithoutACollectorTheLoggerIsStoredAsGiven(t *testing.T) {
	l := slog.New(slog.DiscardHandler)
	if got := log.For(log.Into(context.Background(), l)); got != l {
		t.Fatal("Into wrapped the logger with no collector in the context")
	}
	allocs := testing.AllocsPerRun(100, func() {
		var none *log.Collector
		none.RecordDecision("u1", "t1", "invoice.view", true, "")
		none.RecordLog(log.Record{})
	})
	if allocs != 0 {
		t.Fatalf("recording on a nil collector allocated %v times", allocs)
	}
}

func TestADecisionIsRecordedWithItsAnswerAndCaller(t *testing.T) {
	col := log.NewCollector("req-1")
	authorize := func(allowed bool, reason string) {
		col.RecordDecision("u1", "tenant-a", "invoice.view", allowed, reason)
	}
	authorize(true, "")
	authorize(false, "not the owner")

	got := col.Decisions()
	if len(got) != 2 {
		t.Fatalf("recorded %d decisions, want 2", len(got))
	}
	if got[0].Subject != "u1" || got[0].Tenant != "tenant-a" || got[0].Action != "invoice.view" || !got[0].Allowed || got[0].Reason != "" {
		t.Fatalf("first decision = %+v", got[0])
	}
	if got[1].Allowed || got[1].Reason != "not the owner" {
		t.Fatalf("second decision = %+v", got[1])
	}
	if !strings.HasSuffix(got[0].Caller.File, "correlate_test.go") {
		t.Fatalf("the caller is %+v, want the code that asked for the Grant", got[0].Caller)
	}
	got[0].Action = "changed"
	if col.Decisions()[0].Action != "invoice.view" {
		t.Fatal("Decisions handed out the collector's own slice")
	}
}

// TestFieldsAddedUnderACollectorWithNoLoggerAreRecorded: a request that holds a
// collector and no logger of its own -- Observe running without the kernel's
// logger above it -- writes through the default logger, and the fields With
// attached are still on the line the collector records.
func TestFieldsAddedUnderACollectorWithNoLoggerAreRecorded(t *testing.T) {
	col := log.NewCollector("req-1")
	ctx := log.With(log.WithCollector(context.Background(), col), "request_id", "req-1")
	log.For(ctx).Info("traced")

	lines := col.Logs()
	if len(lines) != 1 {
		t.Fatalf("the collector recorded %d lines, want 1", len(lines))
	}
	if got := lines[0]; got.Message != "traced" || got.Attrs["request_id"] != "req-1" {
		t.Fatalf("recorded %+v, want the line with the field With attached", got)
	}
}
