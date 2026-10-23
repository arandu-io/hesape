package log

import (
	"context"
	"log/slog"
	"time"
)

// DecisionRecord is one Policy decision made during the request: who asked,
// for which action, and what the answer was.
type DecisionRecord struct {
	// Subject is the id of the subject the policy was asked about, empty for
	// a guest or an anonymous subject.
	Subject string
	// Tenant is the subject's tenant.
	Tenant string
	// Action is the action asked for.
	Action string
	// Allowed is whether a Grant was issued.
	Allowed bool
	// Reason is why the policy refused, empty when it allowed.
	Reason string
	// Caller is where the authorization was asked from.
	Caller Frame
	// At is the offset since the start of the request.
	At time.Duration
}

// RecordDecision stores one Policy decision.
//
// It is the call the code that runs a policy makes after the answer is known,
// with the answer: it cannot change it. The skip value walks past this method
// and the function that ran the policy, so Caller points at the code that
// asked for the Grant.
func (c *Collector) RecordDecision(subject, tenant, action string, allowed bool, reason string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decisions = append(c.decisions, DecisionRecord{
		Subject: subject, Tenant: tenant, Action: action, Allowed: allowed, Reason: reason,
		Caller: caller(3), At: time.Since(c.Start),
	})
}

// Decisions returns a copy of the recorded Policy decisions.
func (c *Collector) Decisions() []DecisionRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]DecisionRecord(nil), c.decisions...)
}

// RecordLog stores one line the request logged. The request logger records
// its own lines; a caller writes one directly only when it logs somewhere
// that is not the request logger.
func (c *Collector) RecordLog(r Record) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = append(c.logs, r)
}

// Logs returns a copy of the lines the request logged, oldest first.
func (c *Collector) Logs() []Record {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Record(nil), c.logs...)
}

// collecting answers l writing into c as well. A logger that already writes
// into c is answered as it is, and one that writes into another collector is
// moved to c, so a line is recorded once, by the collector the request holds
// now.
func collecting(l *slog.Logger, c *Collector) *slog.Logger {
	if h, ok := l.Handler().(*collectingHandler); ok {
		if h.collector == c {
			return l
		}
		return slog.New(&collectingHandler{next: h.next, collector: c, attrs: h.attrs, prefix: h.prefix})
	}
	return slog.New(&collectingHandler{next: l.Handler(), collector: c})
}

// collectingHandler hands every line to the handler it wraps and records the
// lines that handler accepts in the collector, flattened the way Capture
// flattens them.
type collectingHandler struct {
	next      slog.Handler
	collector *Collector
	attrs     []captured
	prefix    string
}

func (h *collectingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *collectingHandler) Handle(ctx context.Context, r slog.Record) error {
	rec := Record{Time: r.Time, Level: r.Level, Message: r.Message, Attrs: map[string]any{}}
	for _, c := range h.attrs {
		flatten(rec.Attrs, c.prefix, c.attr)
	}
	r.Attrs(func(a slog.Attr) bool {
		flatten(rec.Attrs, h.prefix, a)
		return true
	})
	h.collector.RecordLog(rec)
	return h.next.Handle(ctx, r)
}

func (h *collectingHandler) WithAttrs(as []slog.Attr) slog.Handler {
	if len(as) == 0 {
		return h
	}
	next := make([]captured, 0, len(h.attrs)+len(as))
	next = append(next, h.attrs...)
	for _, a := range as {
		next = append(next, captured{prefix: h.prefix, attr: a})
	}
	return &collectingHandler{next: h.next.WithAttrs(as), collector: h.collector, attrs: next, prefix: h.prefix}
}

func (h *collectingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &collectingHandler{next: h.next.WithGroup(name), collector: h.collector, attrs: h.attrs, prefix: h.prefix + name + "."}
}
