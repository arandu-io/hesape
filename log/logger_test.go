package log_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/log"
)

// captureStdout runs fn with os.Stdout replaced by a pipe and returns what was
// written. New writes to os.Stdout by design -- the process logs to the
// aggregator, not to a file the framework manages -- so this is the only way to
// assert on what it emits.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	previous := os.Stdout
	os.Stdout = w

	read := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		read <- string(b)
	}()

	fn()

	os.Stdout = previous
	_ = w.Close()
	out := <-read
	_ = r.Close()
	return out
}

// TestForNeverReturnsNil is the contract every call site depends on: nobody
// writes `if l := log.For(ctx); l != nil`, so a nil here is a panic in whatever
// tried to log.
func TestForNeverReturnsNil(t *testing.T) {
	if log.For(context.Background()) == nil {
		t.Fatal("For on a bare context returned nil instead of the default logger")
	}
	// A nil logger stored in the context is the case that would slip through a
	// type assertion that only checked the type.
	if log.For(log.Into(context.Background(), nil)) == nil {
		t.Fatal("For returned the nil logger that was put in the context")
	}
}

func TestIntoAndForRoundTrip(t *testing.T) {
	logger, records := log.Capture()
	ctx := log.Into(context.Background(), logger)

	log.For(ctx).Info("hello")

	if records.Len() != 1 || records.All()[0].Message != "hello" {
		t.Fatalf("records = %+v, want the line written through the context", records.All())
	}
}

// TestMiddlewareReachesTheHandler: without it, For(ctx) inside a request falls
// back to slog.Default(), which ignores the configured handler -- so production
// would emit text where the aggregator expects JSON, and the configured level
// would not apply either.
func TestMiddlewareReachesTheHandler(t *testing.T) {
	logger, records := log.Capture()

	handler := log.Middleware(logger)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		log.For(r.Context()).Warn("handled")
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/customers", nil))

	if records.Len() != 1 {
		t.Fatalf("the handler wrote %d lines, want 1 through the installed logger", records.Len())
	}
	if got := records.All()[0].Level; got != log.LevelWarning {
		t.Errorf("level = %v, want warning", got)
	}
}

// TestWithIsInheritedByEverythingDownstream is why With takes a context instead
// of decorating a local variable. The old shape -- l := For(ctx).With(...) --
// left the repository, the mailer and the panic handler logging without the
// fields that identify the request.
func TestWithIsInheritedByEverythingDownstream(t *testing.T) {
	logger, records := log.Capture()
	ctx := log.With(log.Into(context.Background(), logger), "component", "worker", "queue", "default")

	deeper := func(ctx context.Context) { log.For(ctx).Info("job started") }
	deeper(log.With(ctx, log.Field("job_id", "j-1")))

	if records.Len() != 1 {
		t.Fatalf("records = %d, want 1", records.Len())
	}
	attrs := records.All()[0].Attrs
	for key, want := range map[string]any{"component": "worker", "queue": "default", "job_id": "j-1"} {
		if attrs[key] != want {
			t.Errorf("attr %q = %v, want %v (all: %v)", key, attrs[key], want, attrs)
		}
	}
}

func TestWithNoArgumentsChangesNothing(t *testing.T) {
	ctx := context.Background()
	if log.With(ctx) != ctx {
		t.Fatal("With with no fields returned a different context")
	}
}

// TestFieldsReportsWhatWasAttached exists because a *slog.Logger will not say
// what it carries. Anything that has to forward the request's fields somewhere
// that is not slog has no other way to read them back.
func TestFieldsReportsWhatWasAttached(t *testing.T) {
	if got := log.Fields(context.Background()); got != nil {
		t.Fatalf("Fields on a bare context = %v, want nil", got)
	}

	ctx := log.With(context.Background(), "tenant", "t-1", log.Field("user", "u-9"))
	fields := log.Fields(ctx)
	if len(fields) != 2 {
		t.Fatalf("fields = %v, want the two that were attached", fields)
	}
	if fields[0].Key != "tenant" || fields[1].Key != "user" {
		t.Errorf("fields = %v, want them in the order they were attached", fields)
	}

	// Two contexts derived from the same parent must not see each other's
	// fields: append onto a shared backing array is how one request ends up
	// logging another request's tenant.
	left := log.With(ctx, "branch", "left")
	right := log.With(ctx, "branch", "right")
	if log.Fields(left)[2].Value.String() != "left" || log.Fields(right)[2].Value.String() != "right" {
		t.Errorf("sibling contexts share a slice: left = %v, right = %v", log.Fields(left), log.Fields(right))
	}
	if len(log.Fields(ctx)) != 2 {
		t.Errorf("the parent context grew to %v", log.Fields(ctx))
	}
}

// TestAValueWithoutAKeyIsNotDropped: slog names it !BADKEY and logs it anyway,
// and Fields has to report exactly what the logger recorded or it is a second
// account of the same line.
func TestAValueWithoutAKeyIsNotDropped(t *testing.T) {
	fields := log.Fields(log.With(context.Background(), "lonely"))
	if len(fields) != 1 || fields[0].Key != "!BADKEY" {
		t.Fatalf("fields = %v, want slog's !BADKEY", fields)
	}
}

func TestParseLevelKnowsTheEightNames(t *testing.T) {
	for name, want := range map[string]slog.Level{
		"debug":     log.LevelDebug,
		"info":      log.LevelInfo,
		"notice":    log.LevelNotice,
		"warning":   log.LevelWarning,
		"error":     log.LevelError,
		"critical":  log.LevelCritical,
		"alert":     log.LevelAlert,
		"emergency": log.LevelEmergency,
	} {
		got, err := log.ParseLevel(name)
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestTheEightNamesAreTheOnlyEight: no case folding, no trimming, and no "warn"
// beside "warning". Two spellings of one level is two spellings of one
// configuration value, and LOG_LEVEL=warn would name a level the handler never
// prints under that name.
func TestTheEightNamesAreTheOnlyEight(t *testing.T) {
	for _, name := range []string{"warn", "WARNING", " info ", "Debug", "INFO", ""} {
		if _, err := log.ParseLevel(name); err == nil {
			t.Errorf("ParseLevel(%q) was accepted, and PHP throws for it", name)
		}
	}
}

// TestAnUnknownLevelIsAnError: a typo in LOG_LEVEL that quietly restores the
// default is how a production process ends up logging more than it was told to.
//
// The level beside the error is debug, which is what LogManager falls back to
// for a configuration with no level in it: it used to be info, and two different
// defaults for the same case is one of them being wrong.
func TestAnUnknownLevelIsAnError(t *testing.T) {
	got, err := log.ParseLevel("verbose")
	if err == nil {
		t.Fatal("ParseLevel accepted a name outside the eight")
	}
	if got != log.LevelDebug {
		t.Errorf("level = %v, want debug -- the same fallback LogManager uses", got)
	}
	if !strings.Contains(err.Error(), "verbose") || !strings.Contains(err.Error(), "emergency") {
		t.Errorf("error = %q, want it to name the input and the accepted set", err)
	}
}

// TestTheExtraLevelsPrintUnderTheirOwnName: slog would render notice as
// "INFO+2", and a level that prints under a name ParseLevel would not accept
// back is a level nobody trusts.
func TestTheExtraLevelsPrintUnderTheirOwnName(t *testing.T) {
	for _, tc := range []struct {
		env   string
		level slog.Level
		want  string
	}{
		{"production", log.LevelNotice, `"level":"NOTICE"`},
		{"production", log.LevelCritical, `"level":"CRITICAL"`},
		{"production", log.LevelAlert, `"level":"ALERT"`},
		{"production", log.LevelEmergency, `"level":"EMERGENCY"`},
		{"dev", log.LevelNotice, "level=NOTICE"},
		{"dev", log.LevelWarning, "level=WARNING"},
	} {
		out := captureStdout(t, func() {
			log.New(tc.env, log.LevelDebug).Log(context.Background(), tc.level, "disk filling up")
		})
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s at %v emitted %q, want it to contain %q", tc.env, tc.level, out, tc.want)
		}
	}
}

func TestNewFiltersBelowTheConfiguredLevel(t *testing.T) {
	out := captureStdout(t, func() {
		logger := log.New("production", log.LevelNotice)
		logger.Info("routine")
		logger.Log(context.Background(), log.LevelNotice, "worth reading")
	})

	if strings.Contains(out, "routine") {
		t.Errorf("a line below the configured level was emitted:\n%s", out)
	}
	if !strings.Contains(out, "worth reading") {
		t.Errorf("the line at the configured level was dropped:\n%s", out)
	}
}

// TestDevelopmentIsTextAndTheRestIsJSON: production parses these lines with an
// aggregator, and text with spaces in the values is what makes that fragile.
func TestDevelopmentIsTextAndTheRestIsJSON(t *testing.T) {
	dev := captureStdout(t, func() { log.New("dev", log.LevelInfo).Info("up", "port", 8080) })
	if !strings.Contains(dev, "port=8080") {
		t.Errorf("development is not text:\n%s", dev)
	}

	prod := captureStdout(t, func() { log.New("production", log.LevelInfo).Info("up", "port", 8080) })
	if !strings.Contains(prod, `"port":8080`) {
		t.Errorf("production is not JSON:\n%s", prod)
	}
}
