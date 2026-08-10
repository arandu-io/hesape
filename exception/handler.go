package exception

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/log"
)

// Config is everything the Handler needs from the application.
//
// The zero value is a working production handler: no debug page, built-in
// status pages, everything reported. Development turns Dev on, which is the
// only switch that decides whether the inside of the process is visible.
type Config struct {
	// Dev enables the debug page. It must be false anywhere the application is
	// reachable by somebody who is not running it.
	Dev bool

	// Editor is the target of the "open in IDE" links: vscode, cursor, goland
	// or zed.
	Editor string

	// AppModule is the module path of the application, used to tell app frames
	// from collection and stdlib frames.
	AppModule string

	// Diagnose collects what the registered modules have to say about the state
	// of the system right now. Pass the kernel's own.
	//
	// It exists because the most useful hint is often about something that
	// happened outside this request: the outbox has been stuck for four minutes,
	// the scheduler last ran an hour ago. A page that only looks at the request
	// cannot see any of it, and that is exactly the state where somebody is
	// staring at an error wondering what changed.
	Diagnose func(ctx context.Context) []string

	// Views is the application's own error pages, when it has any. Nil means the
	// built-in ones answer.
	Views Views

	// DontReport are the errors never written to the log.
	//
	// It is Laravel's $dontReport, and it is a list of sentinels compared with
	// errors.Is -- not a list of statuses. "Do not log 404" is the wrong shape:
	// a 404 from a bad link and a 404 from a repository that lost a row are the
	// same status and different news.
	DontReport []error

	// RenderJSONWhen decides whether a failure is answered as JSON rather than
	// as a page. Nil means the default: the request asked for JSON.
	RenderJSONWhen func(r *http.Request) bool
}

// Handler decides what a failed request answers.
//
// It is Illuminate's exception handler: Report writes the failure down, Render
// turns it into a response, and the two are separate because a failure that is
// answered is still a failure that happened.
type Handler struct{ cfg Config }

// NewHandler returns the handler for a configuration.
func NewHandler(cfg Config) *Handler { return &Handler{cfg: cfg} }

// Report writes the failure to the log, unless the configuration excludes it.
//
// The level comes from the status and not from a knob: below 500 the
// application answered on purpose and it is a warning, 500 and above nobody
// meant it and it is an error. A framework where every 404 arrives at ERROR is
// a framework whose alerts get switched off.
func (h *Handler) Report(ctx context.Context, err error) {
	if err == nil || h.dontReport(err) {
		return
	}
	status, known := classify(err)
	l := log.For(ctx)
	if known && status < http.StatusInternalServerError {
		l.Warn("request refused", "status", status, "error", err)
		return
	}
	l.Error("request failed", "status", statusOr500(status, known), "error", err)
}

func (h *Handler) dontReport(err error) bool {
	for _, ignored := range h.cfg.DontReport {
		if errors.Is(err, ignored) {
			return true
		}
	}
	return false
}

// Render answers an error a handler returned.
//
// This is the path that did not exist: every error leaving a controller became
// a panic, and every panic became 500, so an authorization refusal and a
// database being down were the same page. Now the error says what it is --
// through Abort, or through the sentinel table in classify -- and what it says
// is the answer.
//
// It reports before it renders, because a response written to a client that
// went away is still a failure somebody has to know about.
func (h *Handler) Render(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}
	h.Report(r.Context(), err)
	// A returned error carries no stack: nothing captured one where it was
	// created. The frames from here show where the handler gave up, which is
	// worth more than an empty Stack section and less than the truth, and the
	// page does not claim otherwise.
	h.answer(w, r, err, err, Capture(3, h.cfg.AppModule))
}

// RenderForConsole answers a failure outside a request: a command, a job, a
// scheduled task.
//
// The same classification, none of the HTML. A status is printed when the error
// claimed one, because a command that hits a 403 from a policy should say so
// rather than print a stack about a Grant.
func (h *Handler) RenderForConsole(w io.Writer, err error) {
	if err == nil {
		return
	}
	if status, known := classify(err); known {
		fmt.Fprintf(w, "error: %s (%d)\n", messageFor(err, status), status)
		return
	}
	fmt.Fprintf(w, "error: %s\n", err.Error())
}

// answer writes the response. value is what failed -- an error, or whatever was
// panicked with -- and err is the same thing when it happened to be an error.
func (h *Handler) answer(w http.ResponseWriter, r *http.Request, value any, err error, frames []StackFrame) {
	status, known := classify(err)

	if h.wantsJSON(r) {
		h.renderJSON(w, r, statusOr500(status, known), messageFor(err, status))
		return
	}

	// Nobody claimed it, so it is a defect rather than an answer, and in
	// development a defect is worth a page that says where it came from.
	if !known && h.cfg.Dev {
		h.renderDebug(w, r, value, frames)
		return
	}

	h.renderStatus(w, r, statusOr500(status, known), messageFor(err, status))
}

// renderStatus draws the page for a status: the application's own if it has
// one, and the built-in one otherwise.
func (h *Handler) renderStatus(w http.ResponseWriter, r *http.Request, status int, message string) {
	d := PageData{
		Status:    status,
		Title:     statusTitle(status),
		Message:   message,
		RequestID: requestID(w, r),
	}

	name := "errors/" + strconv.Itoa(status)
	if h.cfg.Views != nil && h.cfg.Views.Has(name) {
		// The probe is what makes the fallback safe. A view that failed before
		// writing anything can still be replaced by the built-in page; one that
		// failed halfway through cannot, and appending a second document to it
		// would turn a broken page into two broken pages.
		p := &probe{ResponseWriter: w}
		err := h.cfg.Views.Render(r.Context(), p, status, name, d)
		if err == nil {
			return
		}
		log.For(r.Context()).Error("the application's error page failed to render", "view", name, "error", err)
		if p.wrote {
			return
		}
	}

	statusPage(w, d)
}

func (h *Handler) renderJSON(w http.ResponseWriter, r *http.Request, status int, message string) {
	body := struct {
		Status    int    `json:"status"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	}{Status: status, Message: message, RequestID: requestID(w, r)}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *Handler) wantsJSON(r *http.Request) bool {
	if h.cfg.RenderJSONWhen != nil {
		return h.cfg.RenderJSONWhen(r)
	}
	return wantsJSON(r)
}

// wantsJSON is the default of Config.RenderJSONWhen: the request said it wanted
// JSON, either by asking for it or by being an XHR that is not htmx.
//
// htmx is deliberately excluded. It sends X-Requested-With and it swaps HTML;
// answering it with JSON would put a JSON document inside a div.
func wantsJSON(r *http.Request) bool {
	if r.Header.Get("HX-Request") == "true" {
		return false
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		return true
	}
	return r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// messageFor is the one rule about what leaves the process.
//
// An *HTTPError message was written by the developer for the person reading it,
// so it is shown. Anything else -- a driver's text, a policy's reason, the
// contents of a nil dereference -- is replaced by the standard sentence for the
// status. There is no environment flag on this: an error string is not written
// for a stranger even in development, and development already has the debug
// page, which shows everything.
func messageFor(err error, status int) string {
	var he *HTTPError
	if errors.As(err, &he) && he.Message != "" {
		return he.Message
	}
	return statusMessage(statusOr500(status, status != 0))
}

func statusOr500(status int, known bool) int {
	if !known || status == 0 {
		return http.StatusInternalServerError
	}
	return status
}

// requestID is the thread from this page back to the log line.
//
// The Collector has it in development; in production there is no Collector and
// the header set by the observability middleware is the only copy.
func requestID(w http.ResponseWriter, r *http.Request) string {
	if col := log.FromContext(r.Context()); col != nil && col.RequestID != "" {
		return col.RequestID
	}
	return w.Header().Get("X-Request-ID")
}

// probe records whether anything reached the client.
type probe struct {
	http.ResponseWriter
	wrote bool
}

func (p *probe) WriteHeader(status int) {
	p.wrote = true
	p.ResponseWriter.WriteHeader(status)
}

func (p *probe) Write(b []byte) (int, error) {
	p.wrote = true
	return p.ResponseWriter.Write(b)
}
