package log

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/arandu-io/hesape/log/events"
)

// Dispatcher is the slice of Illuminate\Contracts\Events\Dispatcher that this
// package needs: something to fire an event into and something to register a
// listener with.
//
// It is declared here, on the side that consumes it, rather than imported from
// the events package, so that one concrete dispatcher can serve every package
// that fires an event -- a Go method cannot be overloaded, so Dispatch has to
// take any rather than one concrete event type.
type Dispatcher interface {
	// Dispatch fires the event. Illuminate\Contracts\Events\Dispatcher::dispatch.
	Dispatch(event any)

	// Listen registers a listener that receives every event.
	// Illuminate\Contracts\Events\Dispatcher::listen, which selects by class
	// name; here the listener selects with a type assertion, which is what
	// Logger.Listen wraps.
	Listen(listener func(event any))
}

// ErrNoDispatcher is what Listen answers on a Logger built without a dispatcher.
//
// The dispatcher is optional, so this is not a broken Logger: it writes every
// line it is given and fires no event. What it cannot do is register a listener,
// because there is nothing to register with -- and answering an error there is
// better than accepting a callback that would never be called.
//
// Answers the RuntimeException Illuminate\Log\Logger::listen throws.
var ErrNoDispatcher = errors.New("log: events dispatcher has not been set")

// Logger answers Illuminate\Log\Logger: the eight PSR-3 levels, an accumulated
// context that every future line carries, and a MessageLogged event per line.
//
// It wraps a *slog.Logger the way Illuminate wraps a Monolog logger. There is
// no equivalent of Monolog here and there does not need to be one: slog already
// is the chainable handler, and it is stdlib rather than a dependency.
//
// A Logger is safe for concurrent use.
type Logger struct {
	mu         sync.RWMutex
	logger     *slog.Logger
	dispatcher Dispatcher
	logContext map[string]any
}

// NewLogger answers Illuminate\Log\Logger::__construct.
//
// dispatcher may be nil, exactly as Illuminate's second argument is optional: a
// Logger without one writes lines and fires no event, and Listen then reports
// ErrNoDispatcher. A nil logger falls back to slog.Default rather than panicking
// on the first line.
func NewLogger(logger *slog.Logger, dispatcher Dispatcher) *Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return &Logger{logger: logger, dispatcher: dispatcher, logContext: map[string]any{}}
}

// Emergency answers Illuminate\Log\Logger::emergency: system is unusable.
//
// ctx is Go's and has no counterpart in the PHP signature: slog hands it to the
// handler, which is how the request a line belongs to reaches the output. fields
// is PSR-3's $context array, and the variadic is how PHP's `array $context = []`
// default is spelled here; several maps merge left to right, the last winning.
// Both notes hold for the other seven levels and for Log and Write.
func (l *Logger) Emergency(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelEmergency, message, fields)
}

// Alert answers Illuminate\Log\Logger::alert: action must be taken immediately.
//
// Example: entire website down, database unavailable. This should trigger the
// alerts and wake you up.
func (l *Logger) Alert(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelAlert, message, fields)
}

// Critical answers Illuminate\Log\Logger::critical: critical conditions.
//
// Example: application component unavailable, unexpected exception.
func (l *Logger) Critical(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelCritical, message, fields)
}

// Error answers Illuminate\Log\Logger::error: runtime errors that do not require
// immediate action but should typically be logged and monitored.
func (l *Logger) Error(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelError, message, fields)
}

// Warning answers Illuminate\Log\Logger::warning: exceptional occurrences that
// are not errors.
//
// Example: use of deprecated APIs, poor use of an API, undesirable things that
// are not necessarily wrong.
func (l *Logger) Warning(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelWarning, message, fields)
}

// Notice answers Illuminate\Log\Logger::notice: normal but significant events.
func (l *Logger) Notice(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelNotice, message, fields)
}

// Info answers Illuminate\Log\Logger::info: interesting events.
//
// Example: user logs in, SQL logs.
func (l *Logger) Info(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelInfo, message, fields)
}

// Debug answers Illuminate\Log\Logger::debug: detailed debug information.
func (l *Logger) Debug(ctx context.Context, message any, fields ...map[string]any) {
	l.writeLog(ctx, LevelDebug, message, fields)
}

// Log answers Illuminate\Log\Logger::log: log a message at an arbitrary level.
//
// PHP takes the level as its PSR-3 name because that is what it can dispatch a
// method call on. Here it is a slog.Level, which is the value the handler
// compares against; ParseLevel turns a configured name into one.
func (l *Logger) Log(ctx context.Context, level slog.Level, message any, fields ...map[string]any) {
	l.writeLog(ctx, level, message, fields)
}

// Write answers Illuminate\Log\Logger::write.
//
// It is the alias of Log that Illuminate has carried since Laravel 4, and it
// behaves identically -- both reach writeLog with the same three arguments.
func (l *Logger) Write(ctx context.Context, level slog.Level, message any, fields ...map[string]any) {
	l.writeLog(ctx, level, message, fields)
}

// writeLog answers Illuminate\Log\Logger::writeLog.
//
// The order is Illuminate's, and the order is the behaviour: a level the handler
// does not handle returns before writing and before firing the event, so a
// listener never sees a line that was never written.
func (l *Logger) writeLog(ctx context.Context, level slog.Level, message any, fields []map[string]any) {
	if l == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	l.mu.RLock()
	logger, dispatcher := l.logger, l.dispatcher
	merged := make(map[string]any, len(l.logContext))
	maps.Copy(merged, l.logContext)
	l.mu.RUnlock()

	if !logger.Enabled(ctx, level) {
		return
	}

	for _, f := range fields {
		maps.Copy(merged, f)
	}

	text := formatMessage(message)
	logger.LogAttrs(ctx, level, text, contextAttrs(merged)...)
	l.fireLogEvent(dispatcher, level, text, merged)
}

// WithContext answers Illuminate\Log\Logger::withContext: add context to all
// future logs.
//
// It merges rather than replaces, and returns the receiver so that calls chain
// the way PHP's `return $this` does. PHP's `array $context = []` default is the
// variadic: no argument merges nothing and is not an error.
func (l *Logger) WithContext(fields ...map[string]any) *Logger {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.logContext == nil {
		l.logContext = map[string]any{}
	}
	for _, f := range fields {
		maps.Copy(l.logContext, f)
	}
	return l
}

// WithoutContext answers Illuminate\Log\Logger::withoutContext: drop the given
// keys from the accumulated context, or drop all of it.
//
// PHP's `?array $keys = null` is the variadic here: calling it with no key is
// PHP's null and clears everything, calling it with keys drops exactly those and
// leaves the rest. A key that is not there is not an error, because
// array_diff_key is not either.
func (l *Logger) WithoutContext(keys ...string) *Logger {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(keys) == 0 {
		l.logContext = map[string]any{}
		return l
	}
	for _, key := range keys {
		delete(l.logContext, key)
	}
	return l
}

// Listen answers Illuminate\Log\Logger::listen: register a callback for when a
// log event is fired.
//
// Illuminate throws RuntimeException when no dispatcher was set; this returns
// ErrNoDispatcher instead, which is the (T, error) shape a thrown exception
// takes here. The callback receives only MessageLogged, which is the class
// Illuminate listens for.
func (l *Logger) Listen(callback func(events.MessageLogged)) error {
	if l == nil {
		return ErrNoDispatcher
	}
	l.mu.RLock()
	dispatcher := l.dispatcher
	l.mu.RUnlock()

	if dispatcher == nil {
		return ErrNoDispatcher
	}
	dispatcher.Listen(func(event any) {
		if logged, ok := event.(events.MessageLogged); ok {
			callback(logged)
		}
	})
	return nil
}

// fireLogEvent answers Illuminate\Log\Logger::fireLogEvent.
//
// Illuminate guards against firing twice when the wrapped logger is itself a
// LogManager that has a dispatcher. Here the wrapped logger is a *slog.Logger
// and never a LogManager, so that guard is structural: LogManager holds Loggers,
// a Logger never holds a LogManager, and one line can only be fired once.
func (l *Logger) fireLogEvent(dispatcher Dispatcher, level slog.Level, message string, fields map[string]any) {
	if dispatcher == nil {
		return
	}
	dispatcher.Dispatch(events.MessageLogged{Level: level, Message: message, Context: fields})
}

// GetLogger answers Illuminate\Log\Logger::getLogger: the underlying logger
// implementation, which here is the *slog.Logger rather than a Monolog one.
func (l *Logger) GetLogger() *slog.Logger {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.logger
}

// GetEventDispatcher answers Illuminate\Log\Logger::getEventDispatcher. It
// returns nil when none was set, as PHP returns null.
func (l *Logger) GetEventDispatcher() Dispatcher {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.dispatcher
}

// SetEventDispatcher answers Illuminate\Log\Logger::setEventDispatcher.
func (l *Logger) SetEventDispatcher(dispatcher Dispatcher) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dispatcher = dispatcher
}

// formatMessage answers Illuminate\Log\Logger::formatMessage.
//
// PHP matches on four shapes: an array becomes var_export, a Jsonable becomes
// its JSON, an Arrayable becomes var_export of its array, and anything else is
// cast to string. The Go equivalents are the same four ideas: a string is
// itself, a []byte and an error and a fmt.Stringer have one obvious rendering,
// and a structure becomes JSON -- which is what both var_export and toJson are
// there for, a printable rendering of the shape.
func formatMessage(message any) string {
	switch m := message.(type) {
	case nil:
		return ""
	case string:
		return m
	case []byte:
		return string(m)
	case error:
		return m.Error()
	case fmt.Stringer:
		return m.String()
	}
	if encoded, err := json.Marshal(message); err == nil {
		return string(encoded)
	}
	return fmt.Sprint(message)
}

// contextAttrs turns PSR-3's $context array into slog attributes.
//
// The keys are sorted because a Go map has no order and a PHP array does:
// without this, two identical lines would print their fields in a different
// order each run, and a test that reads the output would be flaky.
func contextAttrs(fields map[string]any) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	out := make([]slog.Attr, 0, len(keys))
	for _, key := range keys {
		out = append(out, slog.Any(key, fields[key]))
	}
	return out
}
