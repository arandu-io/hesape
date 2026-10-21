package http

import (
	"errors"
	"fmt"
	stdhttp "net/http"

	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
)

// NewAdapter returns the function that turns a controller action into the
// http.Handler a route dispatches to.
//
//	adapt := hhttp.NewAdapter(renderer, router.Table(), flash)
//	r.Get("/dashboard", adapt(dashboard.Index)).Name("dashboard")
//	routing.Resource(r, "invoices", InvoiceController{}, adapt)
//
// The function it returns is a routing.Adapter of Context, the parameter
// hesape/routing takes so that it can register a controller action without
// importing the type the action receives. It is declared with the underlying
// function type rather than that name because hesape/routing imports this
// package, and naming its type here would be a cycle; the value assigns to it
// as it stands.
//
// render draws the views Context.View asks for, urls answers Context.URL, and
// flash is where a rejected form and Context.With write. Each may be nil: a
// missing renderer makes Context.View return an error naming it, a missing
// table makes Context.URL return empty, and a missing flash is described
// below.
//
// An error reaching the adapter that nothing below claims is one the action
// could not handle, so it goes to the panic path: the error page in
// development, 500 in production. Swallowing it would answer 200 with an empty
// body, which is the failure nobody debugs.
//
// Two kinds of error are not that, and each is answered in the representation
// the request asked for:
//
//	                    a page                 a request that wants JSON
//	validation.Errors   Reject: flash, back    422 problem, errors member
//	a named status      Refuse, that status    problem, that status
//
// validation.Errors is the answer rather than a failure, and a failed
// Validate reads as one. A page gets the flash and the redirect back, because
// a 422 with the messages in its body is thrown away by htmx and posted again
// by a reload. A client that wants JSON has no form to go back to, so it gets
// the messages keyed by field, through exception.WriteValidationProblem. An
// empty validation.Errors returned as an error panics, naming the fix: the
// action returned its error set without asking whether anything failed, and
// answering it would send the person back to the form with no reason given.
// A rejection on a page with no flash wired panics too, since a rejection
// nobody will see is a defect rather than an answer.
//
// An error that says which status it is -- a record that does not exist, a
// refusal, an expired form, a duplicate key, or an error declaring HTTPStatus,
// which is how a body over the size limit arrives as 413 -- is answered with
// that status, as exception.StatusOf reads it, so the same error is answered
// with the same status here and in the exception handler. A status outside
// 400-599 panics: it is a defect in the error type. The sentence is the
// standard text for the status, never the error's own string, which is written
// for the log and carries what is not for whoever made the request. The
// headers the error asks to carry -- the Retry-After of a 429 -- go out with
// it in both representations, through the GetHeaders method of the first
// error in the chain that has one.
//
// Which representation is Context.WantsJSON's answer, which is also the
// exception handler's default rule for a failure that never reached an action.
//
// The flash is put on the request before the action runs, so an action whose
// form worked says so with Context.With and redirects: the sentence travels in
// the same cookie, under the same key, as the messages of a form that did not.
func NewAdapter(render Renderer, urls URLGenerator, flash *session.Flash) func(func(*Context) error) stdhttp.Handler {
	return func(action func(*Context) error) stdhttp.Handler {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
			if flash != nil {
				req = req.WithContext(WithFlash(req.Context(), flash))
			}
			ctx := NewContext(w, req, render, urls)
			err := action(ctx)
			if err == nil {
				return
			}
			answerActionError(ctx, w, req, flash, err)
		})
	}
}

// answerActionError writes the answer to an error a controller action
// returned, or panics when the error is one the action could not handle. See
// NewAdapter for the table it follows.
func answerActionError(ctx *Context, w stdhttp.ResponseWriter, req *stdhttp.Request, flash *session.Flash, err error) {
	wantsJSON := ctx.WantsJSON()

	var rejected validation.Errors
	if errors.As(err, &rejected) {
		if !rejected.Any() {
			panic("http: a handler returned an empty validation.Errors. " +
				"Return nil when nothing failed: `if errs.Any() { return errs }`")
		}
		if wantsJSON {
			exception.WriteValidationProblem(w, req, rejected)
			return
		}
		if flash == nil {
			panic(err)
		}
		Reject(w, req, flash, rejected)
		return
	}

	status, ok := exception.StatusOf(err)
	if !ok {
		panic(err)
	}
	if status < 400 || status > 599 {
		panic(fmt.Errorf("http: %T asked to be answered with status %d, and an error is answered with 400-599: %w", err, status, err))
	}
	for key, values := range errorHeaders(err) {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if wantsJSON {
		exception.WriteProblem(w, req, status, statusSentence(status))
		return
	}
	Refuse(w, req, status, statusSentence(status))
}

// errorHeaders answers the headers an error asks its answer to carry: the
// GetHeaders of the first error in the chain that has the method, matched by
// method set the way exception.StatusOf matches HTTPStatus, so the error type
// needs no import of this package. They are written before the status,
// because a header set after WriteHeader is a header nobody receives.
func errorHeaders(err error) stdhttp.Header {
	var carrier interface{ GetHeaders() stdhttp.Header }
	if !errors.As(err, &carrier) {
		return nil
	}
	return carrier.GetHeaders()
}

// statusSentence is what a person reads when an error is answered with a
// status: the standard text for it, never the error's own string.
func statusSentence(status int) string {
	if status == exception.StatusPageExpired {
		return "this page has expired: reload it and submit again"
	}
	if text := stdhttp.StatusText(status); text != "" {
		return text
	}
	return "the request could not be completed"
}
