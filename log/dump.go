package log

import (
	"context"
	"time"
)

// Dump is Dumpable::dump, which is the dump() helper in method form, with the
// output redirected: PHP writes the value into the response through VarDumper,
// and this records it.
//
// Dump records a value for the debug page. It is the print statement you reach
// for while chasing something, with the difference that matters: it does not
// write to stdout and does not corrupt the HTML of the response. The
// value is recorded in the Collector and shown on the debug page.
//
// In production, where the Collector is nil, it is a no-op.
func Dump(ctx context.Context, label string, value any) {
	recordDump(ctx, label, value, caller(2))
}

// recordDump is the half Dump and DumpDie share.
//
// The frame is passed in rather than taken here, because it is the one thing the
// two callers cannot share: DumpDie used to reach the Collector through Dump,
// and runtime.Caller counted that hop -- so every dd() on the page pointed at
// log/dump.go instead of at the line somebody wants to open.
func recordDump(ctx context.Context, label string, value any, at Frame) {
	c := FromContext(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dumps = append(c.dumps, DumpRecord{
		Label:  label,
		Value:  value,
		Caller: at,
		At:     time.Since(c.Start),
	})
}

// DumpDie is Dumpable::dd, which is the dd() helper in method form. PHP dumps
// into the response and calls exit; this records and panics with a sentinel, so
// the abort travels through the middleware chain instead of ending the process.
//
// DumpDie records the value and aborts the request with the dump page. It
// panics with a sentinel the Recover middleware recognizes.
//
// # The die half is not optional
//
// It used to return without panicking when there was no Collector, which is
// every request outside development, and the reason given was that a forgotten
// call could then not take a production request down. It is the wrong trade, and
// PHP does not make it: dd() calls exit, always. What the no-op bought was a
// request that answered 200 with the dump written into the middle of the
// response body -- a page that is broken in a way nothing reports, on a code
// path somebody forgot about, which is exactly how it stays forgotten.
//
// Aborting is also what the rest of the chain already expects: Recover
// recognises the sentinel, renders the dump page in development, and outside it
// logs "dump-and-die sentinel outside development" before answering the error
// page. A forgotten call now fails once, loudly, with the line that did it.
//
// The recording half still needs a Collector, and still does nothing without
// one.
func DumpDie(ctx context.Context, label string, value any) {
	recordDump(ctx, label, value, caller(2))
	panic(dumpDie{})
}

type dumpDie struct{}

func (dumpDie) Error() string { return "arandu: dump and die" }

// IsDumpDie has no Illuminate counterpart: PHP's dd() ends the process with
// exit, so nothing downstream is left running to recognise it.
//
// IsDumpDie identifies the sentinel so the Recover middleware renders the dump
// page instead of treating the panic as a real 500.
func IsDumpDie(v any) bool {
	_, ok := v.(dumpDie)
	return ok
}
