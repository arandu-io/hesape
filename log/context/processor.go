package context

import (
	"context"
	"log/slog"
)

// ContextLogProcessor answers Illuminate\Log\Context\ContextLogProcessor.
//
// In Laravel it is a Monolog processor: a function every resolved channel gets
// pushed onto, which copies the context repository's visible entries into the
// record's extra field just before the line is written. That is what makes
// Context::add('order', $id) show up in every log line of the request without
// anybody passing it down.
//
// Here it is a slog.Handler that wraps another one and does the same thing on
// Handle. It has to be a handler rather than a func because slog has no
// processor hook -- a wrapping handler is where slog puts "run before the next
// one sees the record".
//
// Hidden entries are not copied. That is the whole point of hidden: it travels
// with the request and into a queued job, and it does not land in the log.
type ContextLogProcessor struct {
	next slog.Handler
}

// NewContextLogProcessor wraps a handler so that the context repository's
// visible entries reach every record it writes.
func NewContextLogProcessor(next slog.Handler) *ContextLogProcessor {
	return &ContextLogProcessor{next: next}
}

// Enabled reports whether the wrapped handler wants this level. It is asked
// before the attributes are gathered, so a line below the level costs nothing.
func (p *ContextLogProcessor) Enabled(ctx context.Context, level slog.Level) bool {
	return p.next.Enabled(ctx, level)
}

// Handle copies the visible context onto the record and passes it on.
//
// The context repository is read from the ctx, which is where a request-scoped
// value lives in Go -- Laravel reads it from a container singleton, and the
// mechanism is the difference, not the behaviour.
//
// An entry whose name a caller already used on the line itself is not
// overwritten: what the caller said about this one line beats what the request
// said about all of them.
func (p *ContextLogProcessor) Handle(ctx context.Context, record slog.Record) error {
	repository := For(ctx)
	if repository == nil {
		return p.next.Handle(ctx, record)
	}
	visible := repository.All()
	if len(visible) == 0 {
		return p.next.Handle(ctx, record)
	}

	spoken := make(map[string]bool, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		spoken[attr.Key] = true
		return true
	})

	for key, value := range visible {
		if !spoken[key] {
			record.AddAttrs(slog.Any(key, value))
		}
	}
	return p.next.Handle(ctx, record)
}

// WithAttrs answers slog.Handler.WithAttrs, keeping the wrapper in place so
// that a logger derived with .With still carries the context.
func (p *ContextLogProcessor) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextLogProcessor{next: p.next.WithAttrs(attrs)}
}

// WithGroup answers slog.Handler.WithGroup, keeping the wrapper in place for
// the same reason.
func (p *ContextLogProcessor) WithGroup(name string) slog.Handler {
	return &ContextLogProcessor{next: p.next.WithGroup(name)}
}
