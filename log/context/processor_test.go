package context_test

import (
	stdcontext "context"
	"log/slog"
	"testing"
	"time"

	logcontext "github.com/arandu-io/hesape/log/context"
)

// recordingHandler is the handler under the processor: it keeps what reached it
// so the test can assert on the record rather than on formatted text.
type recordingHandler struct {
	level   slog.Level
	records []slog.Record
	groups  []string
	attrs   []slog.Attr
}

func (h *recordingHandler) Enabled(_ stdcontext.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordingHandler) Handle(_ stdcontext.Context, record slog.Record) error {
	h.records = append(h.records, record)
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.attrs = append(h.attrs, attrs...)
	return h
}

func (h *recordingHandler) WithGroup(name string) slog.Handler {
	h.groups = append(h.groups, name)
	return h
}

// fields flattens the last record's attributes.
func (h *recordingHandler) fields(t *testing.T) map[string]any {
	t.Helper()
	if len(h.records) == 0 {
		t.Fatal("nothing reached the handler")
	}
	out := map[string]any{}
	h.records[len(h.records)-1].Attrs(func(a slog.Attr) bool {
		out[a.Key] = a.Value.Any()
		return true
	})
	return out
}

func newRecord(attrs ...slog.Attr) slog.Record {
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "the message", 0)
	record.AddAttrs(attrs...)
	return record
}

func TestTheVisibleContextReachesTheLine(t *testing.T) {
	next := &recordingHandler{}
	processor := logcontext.NewContextLogProcessor(next)

	repository := newRepository().Add("order", "abc").AddHidden("token", "secret")
	ctx := logcontext.Into(stdcontext.Background(), repository)

	if err := processor.Handle(ctx, newRecord()); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	fields := next.fields(t)
	if fields["order"] != "abc" {
		t.Fatalf("the line carries %v for order", fields["order"])
	}
	if _, ok := fields["token"]; ok {
		t.Fatal("the hidden half reached the log line")
	}
}

// TestTheLineWinsOverTheRequest: what the caller said about this one line beats
// what the request said about all of them.
func TestTheLineWinsOverTheRequest(t *testing.T) {
	next := &recordingHandler{}
	processor := logcontext.NewContextLogProcessor(next)

	ctx := logcontext.Into(stdcontext.Background(), newRepository().Add("order", "from-the-request"))
	if err := processor.Handle(ctx, newRecord(slog.String("order", "from-the-line"))); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if got := next.fields(t)["order"]; got != "from-the-line" {
		t.Fatalf("the line carries %v for order", got)
	}
}

func TestWithoutARepositoryTheRecordPassesThrough(t *testing.T) {
	next := &recordingHandler{}
	processor := logcontext.NewContextLogProcessor(next)

	if err := processor.Handle(stdcontext.Background(), newRecord(slog.Int("id", 1))); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := next.fields(t); len(got) != 1 || got["id"] != int64(1) {
		t.Fatalf("the record reached the handler as %v", got)
	}

	// An empty repository is the same case, and must not allocate a walk over
	// the record for nothing.
	ctx := logcontext.Into(stdcontext.Background(), newRepository())
	if err := processor.Handle(ctx, newRecord(slog.Int("id", 2))); err != nil {
		t.Fatalf("Handle with an empty repository: %v", err)
	}
	if got := next.fields(t); len(got) != 1 {
		t.Fatalf("an empty repository added %v", got)
	}
}

func TestEnabledIsTheWrappedHandlersAnswer(t *testing.T) {
	processor := logcontext.NewContextLogProcessor(&recordingHandler{level: slog.LevelWarn})

	if processor.Enabled(stdcontext.Background(), slog.LevelInfo) {
		t.Fatal("the processor said yes to a level the handler under it refuses")
	}
	if !processor.Enabled(stdcontext.Background(), slog.LevelError) {
		t.Fatal("the processor said no to a level the handler under it wants")
	}
}

// TestTheWrapperSurvivesWithAndWithGroup is what makes logger.With("channel",
// name) still carry the request context: a derived logger that dropped the
// wrapper would log without it.
func TestTheWrapperSurvivesWithAndWithGroup(t *testing.T) {
	next := &recordingHandler{}
	logger := slog.New(logcontext.NewContextLogProcessor(next)).With("channel", "single")

	ctx := logcontext.Into(stdcontext.Background(), newRepository().Add("order", "abc"))
	logger.InfoContext(ctx, "the message")

	if got := next.fields(t)["order"]; got != "abc" {
		t.Fatalf("the derived logger carries %v for order", got)
	}

	grouped := logcontext.NewContextLogProcessor(next).WithGroup("request")
	if _, ok := grouped.(*logcontext.ContextLogProcessor); !ok {
		t.Fatalf("WithGroup returned %T, want the processor", grouped)
	}
	withAttrs := logcontext.NewContextLogProcessor(next).WithAttrs([]slog.Attr{slog.Int("id", 1)})
	if _, ok := withAttrs.(*logcontext.ContextLogProcessor); !ok {
		t.Fatalf("WithAttrs returned %T, want the processor", withAttrs)
	}
}
