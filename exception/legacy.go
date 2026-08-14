package exception

import (
	"net/http"

	"github.com/arandu-io/hesape/pipeline"
)

// ErrorHandler is one callback on the handler stack.
//
// It is the Closure the PHP's error() and pushError() take, with the three
// arguments it is called with: the failure, the status it classified as, and
// whether the process is answering a command rather than a request. Returning
// anything other than nil is the PHP's "this handler answered": the ones behind
// it are not called.
type ErrorHandler func(err error, status int, fromConsole bool) any

// Error is Handler::error: it registers an application error handler, in front
// of the ones already registered.
//
// In front, because that is where the PHP puts it -- array_unshift -- and the
// order is the whole reason both this and PushError exist.
func (h *Handler) Error(callback ErrorHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = append([]ErrorHandler{callback}, h.handlers...)
}

// PushError is Handler::pushError: it registers an application error handler at
// the bottom of the stack.
func (h *Handler) PushError(callback ErrorHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handlers = append(h.handlers, callback)
}

// Missing is Handler::missing: it registers a 404 error handler.
//
// The PHP type-hints NotFoundHttpException, which is the class abort(404)
// throws. There are no classes here, so the filter is the status the error
// classified as.
func (h *Handler) Missing(callback func(err error) any) {
	h.Error(func(err error, status int, _ bool) any {
		if status != http.StatusNotFound {
			return nil
		}
		return callback(err)
	})
}

// Fatal is Handler::fatal: an error handler for fatal failures.
//
// A PHP fatal error is one the engine raised rather than the application: it
// has no status because nobody chose one. The same failure in Go is a panic, or
// an error no part of the collection claimed, and that is what this fires for.
func (h *Handler) Fatal(callback func(err error) any) {
	h.Error(func(err error, _ int, _ bool) any {
		if _, known := classify(err); known {
			return nil
		}
		return callback(err)
	})
}

// HandleException is Handler::handleException: it handles an exception for the
// application.
//
// It runs the handler stack and, when none of them answered, hands the failure
// to the displayer. The PHP returns a Response; this writes one, because there
// is no response value in this package -- which is also why it takes the writer
// and the request the PHP does not.
func (h *Handler) HandleException(w http.ResponseWriter, r *http.Request, err error) any {
	if response := h.callCustomHandlers(err, false); response != nil {
		return response
	}

	h.displayException(w, r, err)
	return nil
}

// HandleUncaughtException is Handler::handleUncaughtException: the failure
// nobody caught.
//
// The PHP installs this with set_exception_handler, which Go has no equivalent
// of: the only place a Go program can catch what escaped is a deferred recover,
// and that is Recover. It is Recover that calls this.
func (h *Handler) HandleUncaughtException(w http.ResponseWriter, r *http.Request, err error) {
	h.HandleException(w, r, err)
}

// HandleConsole is Handler::handleConsole: an exception raised by a command.
//
// It runs the same handler stack with fromConsole set, which is exactly what
// the PHP does. The failure that nothing answered is written by
// RenderForConsole.
func (h *Handler) HandleConsole(err error) any {
	return h.callCustomHandlers(err, true)
}

// callCustomHandlers runs the handler stack until one of them answers.
//
// A handler that fails while handling a failure is not allowed to take the
// process with it: the PHP wraps each one in try/catch to avoid a white screen
// of death, and this recovers for the same reason.
func (h *Handler) callCustomHandlers(err error, fromConsole bool) (response any) {
	h.mu.Lock()
	handlers := append([]ErrorHandler(nil), h.handlers...)
	h.mu.Unlock()

	status, known := classify(err)
	if !known {
		status = http.StatusInternalServerError
	}

	for _, handler := range handlers {
		answer := func() (answer any) {
			defer func() {
				if v := recover(); v != nil {
					answer = h.formatException(v)
				}
			}()
			return handler(err, status, fromConsole)
		}()

		if answer != nil {
			return answer
		}
	}
	return nil
}

// formatException is what a handler that failed leaves behind.
func (h *Handler) formatException(v any) string {
	if h.cfg.Dev {
		return "Error in exception handler: " + headline(v)
	}
	return "Error in exception handler."
}

// displayException shows the failure with the displayer for the current mode.
func (h *Handler) displayException(w http.ResponseWriter, r *http.Request, err error) {
	h.Displayer().Display(w, r, err)
}

// Displayer is the choice Handler::displayException makes before it draws: the
// debug displayer when the application is in debug mode, the plain one
// otherwise.
//
// The PHP takes both in its constructor and picks between them in
// displayException(). There is only one of each here, so there is nothing to
// inject and the choice is the same choice.
func (h *Handler) Displayer() Displayer {
	if h.cfg.Dev {
		return &DebugDisplayer{handler: h}
	}
	return &PlainDisplayer{handler: h}
}

// RunningInConsole is Handler::runningInConsole: whether the process is running
// a command rather than serving requests.
//
// The PHP asks php_sapi_name(), because one installation serves both and the
// SAPI is the only way to tell them apart. A Go binary knows which of the two
// it started as, so the kernel says so in Config.Console and this reads it.
func (h *Handler) RunningInConsole() bool { return h.cfg.Console }

// SetDebug is Handler::setDebug: it sets the debug level for the handler.
//
// It is the same switch as Config.Dev, and it must be false anywhere the
// application is reachable by somebody who is not running it.
func (h *Handler) SetDebug(debug bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.Dev = debug
}

// Register is Handler::register: it installs the exception handling for the
// environment, and returns the middleware that catches what escapes.
//
// The PHP registers three runtime hooks here: set_error_handler,
// set_exception_handler and register_shutdown_function. Go has none of the
// three -- there are no warnings to promote to exceptions, no hook for what
// escaped, and no hook at shutdown. What is left is recover(), and Recover is
// the middleware that calls it, so this is where a kernel gets it from.
//
// The environment means what it means in the PHP: in "testing" the process is
// not meant to be protected from itself, so a panic is left to travel up to the
// test that provoked it instead of being drawn as a page.
func (h *Handler) Register(environment string) pipeline.Middleware[http.Handler] {
	h.mu.Lock()
	h.environment = environment
	h.mu.Unlock()
	return Recover(h)
}

// runningInTests reports whether Register was told the environment is
// "testing".
func (h *Handler) runningInTests() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.environment == "testing"
}

// Displayer draws a failure.
//
// It is Illuminate's ExceptionDisplayerInterface, whose one method is
// display(). The PHP returns a Response and this writes one, for the same
// reason HandleException does.
type Displayer interface {
	Display(w http.ResponseWriter, r *http.Request, err error)
}

// PlainDisplayer draws the status page: the standard sentence for the status
// and nothing about the inside of the process.
//
// It is what answers anywhere the application is reachable by somebody who is
// not running it, which is why it is the one that leaks nothing.
type PlainDisplayer struct{ handler *Handler }

// Display is PlainDisplayer::display, which is ExceptionDisplayerInterface's
// one method: it draws the status page for the failure.
//
// The PHP serves a fixed resources/plain.html and copies the exception's
// headers onto the response. This draws the same page the status path draws,
// with the sentence for the status, and it copies the headers too: the sentence
// said there were none to copy, and an *HTTPError has carried them since --
// Retry-After on a 429 is the difference between a client that backs off and one
// that hammers.
func (d *PlainDisplayer) Display(w http.ResponseWriter, r *http.Request, err error) {
	status, known := classify(err)
	applyErrorHeaders(w, err)
	d.handler.renderStatus(w, r, statusOr500(status, known), messageFor(err, status))
}

// DebugDisplayer draws the debug page: the stack with source, the queries with
// their timing, the dumps, the events, and the hints that name the probable
// cause.
//
// The PHP has two of these -- SymfonyDisplayer and WhoopsDisplayer -- and both
// are a third-party renderer this collection does not carry (RULE 13: the page
// has to draw when the view build is what broke, so it is html/template inline
// in this package). One debug page, named for what it is.
type DebugDisplayer struct{ handler *Handler }

// Display is WhoopsDisplayer::display and SymfonyDisplayer::display, which are
// the same ExceptionDisplayerInterface method behind two renderers: it draws
// the debug page for the failure.
//
// Both of the PHP ones answer with the exception's own status, and so does this
// one. It used to answer 500 always, which made a 404 in development a 500 to
// every client and to every test written against it -- the page is the same page
// either way, and the status is the error's answer rather than the page's.
func (d *DebugDisplayer) Display(w http.ResponseWriter, r *http.Request, err error) {
	status, known := classify(err)
	applyErrorHeaders(w, err)
	d.handler.renderDebug(w, r, statusOr500(status, known), err, Capture(3, d.handler.cfg.AppModule))
}
