package foundation_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/pipeline"

	"github.com/arandu-io/hesape/foundation"
)

// Exit criterion 2 of phase 3: the overhead of the Collector with tracing off
// has to be measurable and zero -- not low. Same binary, same middleware, the
// difference being only whether the Collector was installed.
//
// This is what makes "zero cost in production" a claim rather than a hope:
//
//	go test ./foundation -bench Observe -benchmem
//
// The number that matters is BenchmarkObserveProduction, which must not grow as
// the collected surface grows. It records queries the whole time and the
// Collector is nil, so every Record call is a method on a nil receiver that
// returns immediately -- no allocation, no lock, no slice.

// args and dumped exist outside the handler on purpose.
//
// Building them inside would measure the cost of building them, which is not
// what the criterion is about: on the real path they already exist -- the
// database handle passes the same slice it just handed to the driver, and a
// Dump call passes a value the handler already had. Allocating them here would
// put 20 allocations into the "instrumented" column that production never pays.
var (
	args   = []any{"c-1"}
	dumped = map[string]any{"path": "/customers"}
)

// work stands in for a handler that touches the database and dumps a value.
func work(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	col := log.FromContext(ctx)

	for range 20 {
		col.RecordQuery("SELECT * FROM invoice WHERE customer_id = ?", args, time.Microsecond, 1, nil)
	}
	log.Dump(ctx, "the request", dumped)
	col.RecordRender("customer/list.kyse.go", time.Microsecond)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// BenchmarkObserveProduction: no dev, no tracing secret, no recorder. This is
// what a deployed binary runs on every request.
func BenchmarkObserveProduction(b *testing.B) {
	h := pipeline.Chain[http.Handler](http.HandlerFunc(work), foundation.Observe(false, "", nil))
	benchmark(b, h)
}

// BenchmarkObserveDevelopment is the cost when the console is on, for
// comparison. It is expected to be much larger -- twenty query records with
// their stack frames -- and that is fine, because it only happens on a laptop.
func BenchmarkObserveDevelopment(b *testing.B) {
	h := pipeline.Chain[http.Handler](http.HandlerFunc(work), foundation.Observe(true, "", log.NewRecorder(200)))
	benchmark(b, h)
}

// BenchmarkObserveProductionUninstrumented is the comparison that settles the
// criterion.
//
// It is the same pipeline and the same environment as
// BenchmarkObserveProduction, with one difference: the handler records nothing.
// Whatever separates the two IS the cost of instrumenting the code, measured
// with tracing off -- and it has to be indistinguishable from noise, because a
// Record on a nil Collector returns before touching anything.
//
// The other benchmarks measure the request id, the scoped logger and the access
// log, which every environment pays and which are not what this criterion is
// about.
func BenchmarkObserveProductionUninstrumented(b *testing.B) {
	h := pipeline.Chain[http.Handler](http.HandlerFunc(bareWork), foundation.Observe(false, "", nil))
	benchmark(b, h)
}

// bareWork is work() with the instrumentation deleted.
func bareWork(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// BenchmarkNoMiddleware is the floor: the instrumented handler with nothing
// wrapped around it.
func BenchmarkNoMiddleware(b *testing.B) {
	benchmark(b, http.HandlerFunc(work))
}

func benchmark(b *testing.B, h http.Handler) {
	b.Helper()

	// The access log is written on every request in every environment, and
	// writing it to the terminal would be measuring the terminal. Discarding it
	// keeps the log formatting in the measurement, which is honest -- production
	// pays that cost too, it just pays it into a file or a collector.
	quiet := slog.New(slog.NewJSONHandler(io.Discard, nil))
	h = pipeline.Chain[http.Handler](h, log.Middleware(quiet))

	r := httptest.NewRequest(http.MethodGet, "/customers", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
}

// TestTheCollectorIsAbsentInProduction is the assertion behind the benchmark: a
// number is only meaningful if the thing it measures is really off.
func TestTheCollectorIsAbsentInProduction(t *testing.T) {
	var installed bool
	h := pipeline.Chain[http.Handler](http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		installed = log.FromContext(r.Context()) != nil
	}), foundation.Observe(false, "", nil))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if installed {
		t.Fatal("the Collector was installed in production")
	}
}

// TestNothingIsRecordedWithoutARecorder: passing nil is what a production
// pipeline does, and it must not be a special case anyone has to remember.
func TestNothingIsRecordedWithoutARecorder(t *testing.T) {
	h := pipeline.Chain[http.Handler](http.HandlerFunc(work), foundation.Observe(false, "", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// TestTheTracingHeaderTurnsItOnForOneRequest: production tracing on demand,
// which is the point of the header. Without the header the same binary records
// nothing.
func TestTheTracingHeaderTurnsItOnForOneRequest(t *testing.T) {
	recorder := log.NewRecorder(10)
	h := pipeline.Chain[http.Handler](http.HandlerFunc(work), foundation.Observe(false, "s3cret", recorder))

	plain := httptest.NewRequest(http.MethodGet, "/customers", nil)
	h.ServeHTTP(httptest.NewRecorder(), plain)
	if recorder.Len() != 0 {
		t.Fatal("a request without the header was recorded")
	}

	traced := httptest.NewRequest(http.MethodGet, "/customers", nil)
	traced.Header.Set(log.TracingHeader, "s3cret")
	h.ServeHTTP(httptest.NewRecorder(), traced)
	if recorder.Len() != 1 {
		t.Fatalf("the traced request was not recorded: %d entries", recorder.Len())
	}

	// A wrong secret is the interesting case: it must behave exactly like no
	// header at all, or the endpoint becomes an oracle for guessing it.
	wrong := httptest.NewRequest(http.MethodGet, "/customers", nil)
	wrong.Header.Set(log.TracingHeader, "not-the-secret")
	h.ServeHTTP(httptest.NewRecorder(), wrong)
	if recorder.Len() != 1 {
		t.Fatal("a wrong tracing secret was accepted")
	}
}

// TestAnEmptySecretDoesNotEnableTracing: the zero value of the configuration
// must not be the one that turns production tracing on for anybody who sends an
// empty header.
func TestAnEmptySecretDoesNotEnableTracing(t *testing.T) {
	recorder := log.NewRecorder(10)
	h := pipeline.Chain[http.Handler](http.HandlerFunc(work), foundation.Observe(false, "", recorder))

	r := httptest.NewRequest(http.MethodGet, "/customers", nil)
	r.Header.Set(log.TracingHeader, "")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if recorder.Len() != 0 {
		t.Fatal("an empty tracing secret enabled the Collector")
	}
}

// An id echoed from the client lands in every log line of the request, so an id
// that is not short and hexadecimal is an injection into the log aggregator.
func TestRequestIDIsSanitized(t *testing.T) {
	cases := map[string]bool{
		"a1b2c3d4":                        true,
		"5f2c-9a11":                       true,
		"trusted\nlevel=error msg=forged": false,
		"' OR 1=1":                        false,
		strings.Repeat("a", 65):           false,
	}

	for id, kept := range cases {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Request-ID", id)

		pipeline.Chain[http.Handler](http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
			foundation.Observe(true, "", nil)).ServeHTTP(rec, r)

		got := rec.Header().Get("X-Request-ID")
		if kept && got != id {
			t.Errorf("id %q was rejected, want it kept", id)
		}
		if !kept && got == id {
			t.Errorf("id %q was echoed back, want a generated one", id)
		}
		if got == "" {
			t.Errorf("id %q produced no request id at all", id)
		}
	}
}

// HTMX streams over SSE, and losing Flush behind the wrapper breaks it in a way
// that is very hard to trace back to a middleware.
func TestStatusWriterSupportsFlush(t *testing.T) {
	var flushed bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("the wrapped writer does not implement http.Flusher")
		}
		_, _ = w.Write([]byte("event: ping\n\n"))
		f.Flush()
		flushed = true
	})

	pipeline.Chain[http.Handler](handler, foundation.Observe(true, "", nil)).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stream", nil))

	if !flushed {
		t.Error("Flush did not reach the underlying writer")
	}
}
