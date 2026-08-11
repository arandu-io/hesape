package log_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/log"
	"github.com/arandu-io/hesape/log/events"
)

// bus is the smallest thing that satisfies log.Dispatcher.
type bus struct {
	mu        sync.Mutex
	listeners []func(any)
	logged    []events.MessageLogged
}

func (b *bus) Dispatch(event any) {
	b.mu.Lock()
	listeners := make([]func(any), len(b.listeners))
	copy(listeners, b.listeners)
	if message, ok := event.(events.MessageLogged); ok {
		b.logged = append(b.logged, message)
	}
	b.mu.Unlock()

	for _, listen := range listeners {
		listen(event)
	}
}

func (b *bus) Listen(listener func(any)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, listener)
}

func (b *bus) messages() []events.MessageLogged {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]events.MessageLogged(nil), b.logged...)
}

// leveled is a capturing logger that actually refuses a level, which
// log.Capture does not: Capture takes everything on purpose, and the filter is
// exactly what these tests are about.
func leveled(level slog.Level) (*slog.Logger, *log.Records) {
	logger, records := log.Capture()
	return slog.New(&levelFilter{next: logger.Handler(), level: level}), records
}

type levelFilter struct {
	next  slog.Handler
	level slog.Level
}

func (h *levelFilter) Enabled(_ context.Context, level slog.Level) bool { return level >= h.level }

func (h *levelFilter) Handle(ctx context.Context, record slog.Record) error {
	return h.next.Handle(ctx, record)
}

func (h *levelFilter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelFilter{next: h.next.WithAttrs(attrs), level: h.level}
}

func (h *levelFilter) WithGroup(name string) slog.Handler {
	return &levelFilter{next: h.next.WithGroup(name), level: h.level}
}

// TestTheEightLevelsWriteUnderTheirOwnLevel is the PSR-3 surface: each name
// writes at the level the name says, and nothing renames one.
func TestTheEightLevelsWriteUnderTheirOwnLevel(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil)

	calls := []struct {
		name  string
		write func(context.Context, any, ...map[string]any)
		level slog.Level
	}{
		{"Emergency", logger.Emergency, log.LevelEmergency},
		{"Alert", logger.Alert, log.LevelAlert},
		{"Critical", logger.Critical, log.LevelCritical},
		{"Error", logger.Error, log.LevelError},
		{"Warning", logger.Warning, log.LevelWarning},
		{"Notice", logger.Notice, log.LevelNotice},
		{"Info", logger.Info, log.LevelInfo},
		{"Debug", logger.Debug, log.LevelDebug},
	}
	for _, call := range calls {
		call.write(context.Background(), call.name)
	}

	written := records.All()
	if len(written) != len(calls) {
		t.Fatalf("%d lines were written, want %d", len(written), len(calls))
	}
	for i, call := range calls {
		if written[i].Level != call.level {
			t.Errorf("%s wrote at %v, want %v", call.name, written[i].Level, call.level)
		}
		if written[i].Message != call.name {
			t.Errorf("%s wrote the message %q", call.name, written[i].Message)
		}
	}
}

// TestLogAndWriteTakeTheLevel: Write is the alias Illuminate has carried since
// Laravel 4, and it has to behave identically.
func TestLogAndWriteTakeTheLevel(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil)

	logger.Log(context.Background(), log.LevelNotice, "through Log")
	logger.Write(context.Background(), log.LevelNotice, "through Write")

	written := records.All()
	if len(written) != 2 {
		t.Fatalf("%d lines were written", len(written))
	}
	for _, record := range written {
		if record.Level != log.LevelNotice {
			t.Fatalf("the line %q wrote at %v", record.Message, record.Level)
		}
	}
}

func TestTheContextArrayReachesTheLine(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil)

	logger.Info(context.Background(), "the message", map[string]any{"order": "abc"}, map[string]any{"user": 1})

	// slog widens an int to int64 on the way into a value, which is why the
	// want is int64 and not int.
	fields := records.All()[0].Attrs
	if fields["order"] != "abc" || fields["user"] != int64(1) {
		t.Fatalf("the line carries %v", fields)
	}
}

// TestSeveralContextMapsMergeLeftToRight is how the variadic stands in for
// PHP's single `array $context = []`.
func TestSeveralContextMapsMergeLeftToRight(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil)

	logger.Info(context.Background(), "m", map[string]any{"key": "first"}, map[string]any{"key": "second"})

	if got := records.All()[0].Attrs["key"]; got != "second" {
		t.Fatalf("the line carries %v, want the last map to win", got)
	}
}

func TestWithContextIsCarriedByEveryFutureLine(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil).WithContext(map[string]any{"request_id": "r-1"})

	logger.Info(context.Background(), "first")
	logger.WithContext(map[string]any{"tenant": "t-1"})
	logger.Info(context.Background(), "second")

	written := records.All()
	if written[0].Attrs["request_id"] != "r-1" {
		t.Fatalf("the first line carries %v", written[0].Attrs)
	}
	if written[1].Attrs["request_id"] != "r-1" || written[1].Attrs["tenant"] != "t-1" {
		t.Fatalf("the second line carries %v, want both", written[1].Attrs)
	}
	// WithContext merges rather than replacing, and with no argument it merges
	// nothing rather than failing.
	logger.WithContext()
	logger.Info(context.Background(), "third")
	if records.All()[2].Attrs["request_id"] != "r-1" {
		t.Fatal("WithContext with no argument cleared the accumulated context")
	}
}

// TestTheCallWinsOverTheAccumulatedContext is array_merge($this->context,
// $context): the second argument wins.
func TestTheCallWinsOverTheAccumulatedContext(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil).WithContext(map[string]any{"key": "accumulated"})

	logger.Info(context.Background(), "m", map[string]any{"key": "per call"})

	if got := records.All()[0].Attrs["key"]; got != "per call" {
		t.Fatalf("the line carries %v", got)
	}
	// And the accumulated value survives the call that overrode it.
	logger.Info(context.Background(), "m")
	if got := records.All()[1].Attrs["key"]; got != "accumulated" {
		t.Fatalf("the next line carries %v", got)
	}
}

func TestWithoutContextDropsKeysOrAllOfThem(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil).
		WithContext(map[string]any{"a": 1, "b": 2, "c": 3})

	// Named keys drop exactly those, and a key that is not there is not an
	// error -- array_diff_key is not either.
	logger.WithoutContext("a", "never-was")
	logger.Info(context.Background(), "first")
	if got := records.All()[0].Attrs; len(got) != 2 || got["b"] != int64(2) || got["c"] != int64(3) {
		t.Fatalf("the line carries %v", got)
	}

	// No key at all is PHP's null, which clears everything.
	logger.WithoutContext()
	logger.Info(context.Background(), "second")
	if got := records.All()[1].Attrs; len(got) != 0 {
		t.Fatalf("WithoutContext left %v", got)
	}
}

// TestALineBelowTheLevelIsNeitherWrittenNorFired is the order in writeLog, and
// the order is the behaviour: a listener must never see a line nobody wrote.
func TestALineBelowTheLevelIsNeitherWrittenNorFired(t *testing.T) {
	filtered, records := leveled(log.LevelWarning)
	dispatcher := &bus{}
	logger := log.NewLogger(filtered, dispatcher)

	logger.Info(context.Background(), "dropped")
	if records.Len() != 0 {
		t.Fatalf("a line below the level was written: %v", records.All())
	}
	if len(dispatcher.messages()) != 0 {
		t.Fatal("a line below the level fired the event")
	}

	logger.Error(context.Background(), "kept")
	if records.Len() != 1 {
		t.Fatal("a line above the level was dropped")
	}
	if len(dispatcher.messages()) != 1 {
		t.Fatal("a line above the level did not fire the event")
	}
}

func TestTheEventCarriesWhatWasWritten(t *testing.T) {
	captured, _ := log.Capture()
	dispatcher := &bus{}
	logger := log.NewLogger(captured, dispatcher).WithContext(map[string]any{"tenant": "t-1"})

	logger.Warning(context.Background(), "the message", map[string]any{"order": "abc"})

	messages := dispatcher.messages()
	if len(messages) != 1 {
		t.Fatalf("%d events were fired", len(messages))
	}
	if messages[0].Level != log.LevelWarning {
		t.Fatalf("the event carries the level %v", messages[0].Level)
	}
	if messages[0].Message != "the message" {
		t.Fatalf("the event carries the message %q", messages[0].Message)
	}
	want := map[string]any{"tenant": "t-1", "order": "abc"}
	if !reflect.DeepEqual(messages[0].Context, want) {
		t.Fatalf("the event carries the context %v, want %v", messages[0].Context, want)
	}
}

func TestListenSelectsTheMessageLoggedEvent(t *testing.T) {
	captured, _ := log.Capture()
	dispatcher := &bus{}
	logger := log.NewLogger(captured, dispatcher)

	var seen []string
	if err := logger.Listen(func(message events.MessageLogged) { seen = append(seen, message.Message) }); err != nil {
		t.Fatalf("Listen: %v", err)
	}

	logger.Info(context.Background(), "the line")
	dispatcher.Dispatch("something that is not a MessageLogged")

	if !reflect.DeepEqual(seen, []string{"the line"}) {
		t.Fatalf("the listener saw %v", seen)
	}
}

// TestListenWithoutADispatcher is the RuntimeException Illuminate throws, in
// the shape this ecosystem gives a thrown exception.
func TestListenWithoutADispatcher(t *testing.T) {
	captured, _ := log.Capture()

	err := log.NewLogger(captured, nil).Listen(func(events.MessageLogged) {})
	if !errors.Is(err, log.ErrNoDispatcher) {
		t.Fatalf("Listen returned %v, want ErrNoDispatcher", err)
	}

	var nilLogger *log.Logger
	if err := nilLogger.Listen(func(events.MessageLogged) {}); !errors.Is(err, log.ErrNoDispatcher) {
		t.Fatalf("Listen on a nil logger returned %v", err)
	}
}

// stringer and jsonable stand for the two shapes formatMessage recognises
// beyond a plain string.
type stringer struct{}

func (stringer) String() string { return "from String" }

type jsonable struct {
	Order string `json:"order"`
}

func TestFormatMessage(t *testing.T) {
	captured, records := log.Capture()
	logger := log.NewLogger(captured, nil)

	cases := []struct {
		name    string
		message any
		want    string
	}{
		{"a string is itself", "plain", "plain"},
		{"nil is empty", nil, ""},
		{"bytes are their text", []byte("bytes"), "bytes"},
		{"an error is its message", errors.New("it broke"), "it broke"},
		{"a Stringer is its String", stringer{}, "from String"},
		{"a structure is its JSON", jsonable{Order: "abc"}, `{"order":"abc"}`},
		{"a map is its JSON", map[string]int{"one": 1}, `{"one":1}`},
		{"a number prints", 42, "42"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logger.Info(context.Background(), c.message)
			if got := records.All()[i].Message; got != c.want {
				t.Fatalf("the line reads %q, want %q", got, c.want)
			}
		})
	}
}

func TestTheDispatcherCanBeSetAfterTheFact(t *testing.T) {
	captured, _ := log.Capture()
	logger := log.NewLogger(captured, nil)

	if logger.GetEventDispatcher() != nil {
		t.Fatal("a logger built without a dispatcher reported one")
	}
	if logger.GetLogger() != captured {
		t.Fatal("GetLogger did not return the logger it was built with")
	}

	dispatcher := &bus{}
	logger.SetEventDispatcher(dispatcher)
	if logger.GetEventDispatcher() != dispatcher {
		t.Fatal("SetEventDispatcher did not take")
	}
	logger.Info(context.Background(), "the line")
	if len(dispatcher.messages()) != 1 {
		t.Fatal("the dispatcher set after the fact was not used")
	}
}

// TestANilLoggerFallsBackToTheDefault: a nil argument is not a panic on the
// first line.
func TestANilLoggerFallsBackToTheDefault(t *testing.T) {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logger := log.NewLogger(nil, nil)
	if logger.GetLogger() == nil {
		t.Fatal("NewLogger(nil) left the logger nil")
	}
	logger.Info(context.Background(), "this must not panic")
}

func TestEveryLoggerMethodIsSafeOnANilReceiver(t *testing.T) {
	var logger *log.Logger

	logger.Emergency(context.Background(), "m")
	logger.Alert(context.Background(), "m")
	logger.Critical(context.Background(), "m")
	logger.Error(context.Background(), "m")
	logger.Warning(context.Background(), "m")
	logger.Notice(context.Background(), "m")
	logger.Info(context.Background(), "m")
	logger.Debug(context.Background(), "m")
	logger.Log(context.Background(), log.LevelInfo, "m")
	logger.Write(context.Background(), log.LevelInfo, "m")
	logger.SetEventDispatcher(&bus{})
	if logger.WithContext(map[string]any{"k": 1}) != nil {
		t.Fatal("WithContext on a nil logger returned something")
	}
	if logger.WithoutContext("k") != nil {
		t.Fatal("WithoutContext on a nil logger returned something")
	}
	if logger.GetLogger() != nil || logger.GetEventDispatcher() != nil {
		t.Fatal("a nil logger reported a logger or a dispatcher")
	}
}

// TestANilContextIsTheBackground: ctx is Go's addition, and passing nil is the
// mistake it invites.
func TestANilContextIsTheBackground(t *testing.T) {
	captured, records := log.Capture()

	log.NewLogger(captured, nil).Info(nil, "the line") //nolint:staticcheck // a nil context is exactly the case under test

	if records.Len() != 1 {
		t.Fatal("a line written with a nil context was lost")
	}
}

func TestTheLoggerIsSafeUnderConcurrentUse(t *testing.T) {
	captured, records := log.Capture()
	dispatcher := &bus{}
	logger := log.NewLogger(captured, dispatcher)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.WithContext(map[string]any{fmt.Sprintf("key-%d", i): i})
			logger.Info(context.Background(), "the line", map[string]any{"i": i})
			_ = logger.GetLogger()
		}()
	}
	wg.Wait()

	if records.Len() != 32 {
		t.Fatalf("%d lines were written, want 32", records.Len())
	}
	if len(dispatcher.messages()) != 32 {
		t.Fatalf("%d events were fired, want 32", len(dispatcher.messages()))
	}
}
