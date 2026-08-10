// Package exception is where a failed request stops.
//
// It mirrors Illuminate\Foundation\Exceptions, and the files it answers to, in
// the clone at laravel_illuminate/exception:
//
//	ExceptionDisplayerInterface.php
//	ExceptionServiceProvider.php
//	Handler.php
//	PlainDisplayer.php
//	SymfonyDisplayer.php
//	WhoopsDisplayer.php
//
// # The two ways a request fails, and the one place they meet
//
// A handler either returns an error or panics. Both arrive here: Recover turns
// a panic into a value the Handler answers, and the routing layer hands the
// Handler whatever a controller action returned. One Handler, one decision about
// what the person in front of the browser sees.
//
// # Abort is a returned error, not a call that never comes back
//
// Laravel's abort() throws, and a global helper is reachable from anywhere.
// There is no throw here and no global helper, so the equivalent is a value:
//
//	if invoice == nil {
//		return exception.Abort(http.StatusNotFound, "no invoice with that number")
//	}
//
// Abort builds an *HTTPError, StatusOf reads the status back out of an error
// chain, and classify is the closed table that says what the collection's own
// sentinels mean -- auth.ErrForbidden is 403, an expired CSRF token is 419.
// Before this existed, every error leaving a handler became 500, including the
// ones that had already said exactly what they were.
//
// The table is closed on purpose (RULE 15). An application that wants a status
// says so with Abort; it does not get a second mechanism for the same sentence.
//
// # The pages
//
// Two kinds, and they never overlap. A status the framework recognises gets the
// status page -- 401, 403, 404, 405, 419, 429, 500, 503, and the standard text
// for anything else. An error nobody claimed gets the debug page in development,
// which is Ignition's idea with the Collector behind it: the stack with source
// snippets, the queries with their timing, the dumps, the events, and the
// hints -- the part that names the probable cause instead of only showing the
// data.
//
// Both are html/template inline in this package rather than kyse views, because
// they have to render when the view build is what broke (RULE 13).
//
// An application overrides a status page by providing a view named errors/404,
// errors/403 and so on, and wiring it through Config.Views. Nothing is required:
// the built-in pages answer until somebody wants their own.
//
// # Absolute rule
//
// Nothing that reveals the inside of the process -- the debug page, a stack, an
// error string that is not an *HTTPError message -- may be reachable when
// Config.Dev is false.
package exception
