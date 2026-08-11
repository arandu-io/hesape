package exception

import (
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"time"
)

// ReportableHandler is one callback registered with Reportable.
//
// It is returned so the registration can be continued: Stop says that reporting
// ends with this callback rather than falling through to the log, which is the
// PHP's ->stop().
type ReportableHandler struct {
	callback   any
	shouldStop bool
}

// Stop indicates that report handling should stop after invoking this callback.
func (r *ReportableHandler) Stop() *ReportableHandler {
	r.shouldStop = true
	return r
}

// Handles reports whether the callback handles the given error.
//
// The PHP reads the closure's type hint. A Go closure carries the same
// information in the type of its first parameter, and it is read the same way:
// a callback written func(*HTTPError) bool handles whatever errors.As can pull
// an *HTTPError out of, and one written func(error) bool handles everything.
func (r *ReportableHandler) Handles(err error) bool { return handles(r.callback, err) }

// invoke calls the callback and reports whether reporting continues.
func (r *ReportableHandler) invoke(err error) bool {
	if answered, ok := callHandler(r.callback, err).(bool); ok && !answered {
		return false
	}
	return !r.shouldStop
}

// Reportable registers a reportable callback.
//
//	h.Reportable(func(err *QueryError) bool {
//		telemetry.Record(err)
//		return true
//	}).Stop()
//
// A callback that returns false stops the reporting there, which is what the
// PHP's `=== false` means; returning true, or nothing, lets it carry on to the
// log unless Stop was called.
func (h *Handler) Reportable(reportUsing any) *ReportableHandler {
	handler := &ReportableHandler{callback: reportUsing}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.reportCallbacks = append(h.reportCallbacks, handler)
	return handler
}

// Renderable registers a renderable callback.
//
//	h.Renderable(func(err *PaymentDeclined, w http.ResponseWriter, r *http.Request) bool {
//		...
//		return true
//	})
//
// The PHP callback takes ($e, $request) and returns a response or null; there
// is no response value in this package -- the answer is written to the
// ResponseWriter -- so the callback takes the writer as well and returns
// whether it answered. Returning false is the PHP's null: the next callback
// gets a turn, and the built-in pages answer if none of them do.
func (h *Handler) Renderable(renderUsing any) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.renderCallbacks = append(h.renderCallbacks, renderUsing)
	return h
}

// Map registers a new exception mapping.
//
//	h.Map(sql.ErrNoRows, func(err error) error {
//		return exception.Abort(http.StatusNotFound, "")
//	})
//
// The PHP keys the map by class name and matches with is_a. There are no class
// names in Go, so the key is the sentinel and the match is errors.Is, which is
// how this collection asks "is this that error" everywhere else.
func (h *Handler) Map(from error, to func(err error) error) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.exceptionMap = append(h.exceptionMap, mapping{from: from, to: to})
	return h
}

// Ignore indicates that the given errors should not be reported.
func (h *Handler) Ignore(errs ...error) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, err := range errs {
		if !containsError(h.dontReport, err) {
			h.dontReport = append(h.dontReport, err)
		}
	}
	return h
}

// DontReport indicates that the given errors should not be reported.
//
// It is the alias of Ignore, which is what the PHP says of it as well.
func (h *Handler) DontReport(errs ...error) *Handler { return h.Ignore(errs...) }

// DontReportWhen registers a callback that decides whether an error is
// reported.
func (h *Handler) DontReportWhen(dontReportWhen func(err error) bool) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dontReportCallbacks = append(h.dontReportCallbacks, dontReportWhen)
	return h
}

// StopIgnoring removes the given errors from the list of ignored ones.
func (h *Handler) StopIgnoring(errs ...error) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()

	kept := h.dontReport[:0]
	for _, ignored := range h.dontReport {
		if !containsError(errs, ignored) {
			kept = append(kept, ignored)
		}
	}
	h.dontReport = kept
	return h
}

// DontReportDuplicates reports an error at most once.
//
// The PHP keys a WeakMap by the exception instance. Go has no weak map and no
// exception instance: the key is the error value, which is the same identity
// for the pointer errors a Go program raises, and an error value that cannot be
// a map key is simply always reported.
func (h *Handler) DontReportDuplicates() *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.withoutDuplicates = true
	return h
}

// Level sets the log level for the given error.
//
// The PHP keys by exception class and takes a PSR level string; here the key is
// the sentinel errors.Is compares against, and the level is slog's, which is
// what this collection logs through.
func (h *Handler) Level(target error, level slog.Level) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.levels = append(h.levels, leveled{target: target, level: level})
	return h
}

// ShouldRenderJSONWhen registers the callback that decides whether a failure is
// answered as JSON.
//
// The PHP spells it shouldRenderJsonWhen; Go spells an initialism in capitals,
// which is the one change ADR 0044 asks to be said out loud.
func (h *Handler) ShouldRenderJSONWhen(callback func(r *http.Request, err error) bool) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shouldRenderJSONWhenCallback = callback
	return h
}

// RespondUsing registers the callback that prepares the final response.
//
// The PHP hands the callback the response and returns a new one. Nothing here
// returns a response: the callback is given the writer the answer was written
// to, and it adds what it wants -- a header, a trace id -- after the fact.
func (h *Handler) RespondUsing(callback func(w http.ResponseWriter, r *http.Request, err error)) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.finalizeResponse = callback
	return h
}

// BuildContextUsing registers a callback that builds the fields logged with an
// error.
func (h *Handler) BuildContextUsing(contextCallback func(err error, context map[string]any) map[string]any) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.contextCallbacks = append(h.contextCallbacks, contextCallback)
	return h
}

// DontFlash indicates that the given attributes are never carried back to a
// form after a validation failure.
//
// The three the PHP starts with -- password, password_confirmation and
// current_password -- are already excluded, and this adds to them.
func (h *Handler) DontFlash(attributes ...string) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, attribute := range attributes {
		if !containsString(h.dontFlash, attribute) {
			h.dontFlash = append(h.dontFlash, attribute)
		}
	}
	return h
}

// neverFlashed are the inputs that are never carried back, whatever DontFlash
// was told. It is the PHP's $dontFlash default.
var neverFlashed = []string{"current_password", "password", "password_confirmation"}

// FlashableInput returns the input with everything DontFlash named removed.
//
// The PHP does the removal inside invalid(), where it builds the redirect that
// carries the old input back to the form. This package answers failures, it
// does not route, so the removal is a method and the redirect belongs to
// whoever builds it. There is no PHP name for it because the PHP reads the
// property directly, which Go cannot do from another package.
func (h *Handler) FlashableInput(input map[string]any) map[string]any {
	h.mu.Lock()
	hidden := append(append([]string(nil), neverFlashed...), h.dontFlash...)
	h.mu.Unlock()

	out := make(map[string]any, len(input))
	for key, value := range input {
		if !containsString(hidden, key) {
			out[key] = value
		}
	}
	return out
}

// Throttle is how often an error may be reported.
//
// It answers Illuminate\Cache\RateLimiting\Limit, without the cache: the count
// is kept in this process. Two replicas therefore throttle separately, which is
// the honest cost of not depending on a shared store from here.
//
// The zero value is Limit::none(): no throttling at all.
type Throttle struct {
	// Key groups the errors that share a budget. Empty means one budget per
	// error type, which is what the PHP defaults to.
	Key string
	// MaxAttempts is how many reports fit in the window. Zero or less is
	// unlimited.
	MaxAttempts int
	// Decay is how long the window lasts. Zero means a minute, which is the
	// PHP's default decay.
	Decay time.Duration
}

// throttleWindow is one open budget: when it started and how much of it is left.
type throttleWindow struct {
	started time.Time
	count   int
}

// ThrottleUsing registers a callback that decides how often an error may be
// reported.
//
//	h.ThrottleUsing(func(err *QueryError) exception.Throttle {
//		return exception.Throttle{MaxAttempts: 10, Decay: time.Minute}
//	})
//
// The callback is matched to the error by the type of its first parameter, the
// same way Reportable's is.
func (h *Handler) ThrottleUsing(throttleUsing any) *Handler {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.throttleCallbacks = append(h.throttleCallbacks, throttleUsing)
	return h
}

// throttled reports whether this error has used up its budget.
func (h *Handler) throttled(err error) bool {
	h.mu.Lock()
	callbacks := append([]any(nil), h.throttleCallbacks...)
	h.mu.Unlock()

	var throttle Throttle
	for _, callback := range callbacks {
		if !handles(callback, err) {
			continue
		}
		if answer, ok := callHandler(callback, err).(Throttle); ok && answer.MaxAttempts > 0 {
			throttle = answer
			break
		}
	}
	if throttle.MaxAttempts <= 0 {
		return false
	}

	key := throttle.Key
	if key == "" {
		key = reflect.TypeOf(err).String()
	}
	decay := throttle.Decay
	if decay <= 0 {
		decay = time.Minute
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	window, ok := h.throttles[key]
	if !ok || time.Since(window.started) > decay {
		h.throttles[key] = &throttleWindow{started: time.Now(), count: 1}
		return false
	}
	window.count++
	return window.count > throttle.MaxAttempts
}

func containsError(list []error, target error) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// firstParameterType is the type of a callback's first parameter, which is
// where the PHP puts the type hint that says which exceptions it handles.
func firstParameterType(fn any) reflect.Type {
	if fn == nil {
		return nil
	}
	t := reflect.TypeOf(fn)
	if t.Kind() != reflect.Func || t.NumIn() == 0 {
		return nil
	}
	return t.In(0)
}

// handles reports whether err is what the callback's first parameter names.
//
// It answers ReflectsClosures::firstClosureParameterTypes plus the is_a the
// PHP does with the result, in one step: errors.As is the only way to ask a Go
// error chain the same question.
func handles(fn any, err error) bool {
	want := firstParameterType(fn)
	switch {
	case want == nil:
		return false
	case want == errorType:
		return true
	case want.Kind() == reflect.Interface, want.Implements(errorType):
		target := reflect.New(want)
		return errors.As(err, target.Interface())
	default:
		return false
	}
}

// callHandler calls the callback with the error narrowed to the type its first
// parameter names, and the rest of the arguments after it.
//
// An argument the callback did not ask for is dropped and one it asked for and
// did not get is the zero value, because a reflect.Call with the wrong arity
// panics and a mistyped callback should not take the process down while it is
// already answering a failure.
func callHandler(fn any, err error, extra ...any) any {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return nil
	}
	t := v.Type()

	in := make([]reflect.Value, 0, t.NumIn())
	for i := range t.NumIn() {
		if i == 0 {
			in = append(in, errorAs(err, t.In(0)))
			continue
		}
		in = append(in, argumentFor(extra, i-1, t.In(i)))
	}

	out := v.Call(in)
	if len(out) == 0 {
		return nil
	}
	return out[0].Interface()
}

// errorAs narrows the error to the type the parameter names.
func errorAs(err error, want reflect.Type) reflect.Value {
	if want == errorType {
		if err == nil {
			return reflect.Zero(want)
		}
		return reflect.ValueOf(err)
	}
	if want.Kind() == reflect.Interface || want.Implements(errorType) {
		target := reflect.New(want)
		if errors.As(err, target.Interface()) {
			return target.Elem()
		}
	}
	return reflect.Zero(want)
}

// argumentFor prepares one argument for the parameter it is about to fill.
func argumentFor(args []any, i int, want reflect.Type) reflect.Value {
	if i >= len(args) || args[i] == nil {
		return reflect.Zero(want)
	}
	v := reflect.ValueOf(args[i])
	if v.Type().AssignableTo(want) {
		return v
	}
	return reflect.Zero(want)
}
