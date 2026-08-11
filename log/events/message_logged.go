package events

import "log/slog"

// MessageLogged answers Illuminate\Log\Events\MessageLogged.
//
// A Logger dispatches it once per written line, after the handler accepted the
// line, and never for a line the level filter dropped -- Illuminate returns
// from writeLog before firing when the handler is not handling the level, and
// so does this. It is what a profiler, a request timeline or a test listens to
// in order to see every message one request produced.
type MessageLogged struct {
	// Level is the log "level".
	//
	// Illuminate carries the PSR-3 name as a string; here it is the slog level,
	// because that is the value the handler was asked about and a listener that
	// compares against log.LevelError should not have to parse a word to do it.
	Level slog.Level

	// Message is the log message, already formatted -- Illuminate fires the
	// event with the return of formatMessage, not with the original argument.
	Message string

	// Context is the log context: PSR-3's $context array, with the logger's own
	// accumulated context already merged in, exactly as Illuminate fires it.
	//
	// It is the map the logger wrote, not a copy shared with the next line, so
	// a listener may read it freely.
	Context map[string]any
}
