// Package exception is where a failed request stops.
//
// # Two sources, and which is which
//
// The clone at laravel_illuminate/exception is the legacy illuminate/exception
// package, from before the handler moved into the foundation. Both are answered
// here, because both are still the vocabulary somebody arriving from Laravel
// has.
//
// From the clone -- laravel_illuminate/exception, the legacy package:
//
//	Handler.php                    Register, Error, PushError, Missing, Fatal,
//	                               HandleException, HandleUncaughtException,
//	                               HandleConsole, RunningInConsole, SetDebug
//	ExceptionDisplayerInterface.php Displayer
//	PlainDisplayer.php             PlainDisplayer
//	SymfonyDisplayer.php,
//	WhoopsDisplayer.php            DebugDisplayer
//	ExceptionServiceProvider.php   nothing: it binds into the container (ADR 0001)
//
// From the current framework -- reference_laravel/framework/src/Illuminate/
// Foundation/Exceptions:
//
//	Handler.php            Report, ShouldReport, Render, RenderForConsole,
//	                       Reportable, Renderable, Map, Ignore, DontReport,
//	                       DontReportWhen, StopIgnoring, DontReportDuplicates,
//	                       ShouldRenderJSONWhen, RespondUsing, Level,
//	                       ThrottleUsing, BuildContextUsing, DontFlash
//	ReportableHandler.php  ReportableHandler, with Handles and Stop
//
// Two methods of the legacy Handler have no equivalent, and both are reason 1
// of ADR 0044 -- a PHP language feature Go does not have. Handler::handleError
// is the callback set_error_handler installs, which promotes a warning or a
// notice into an exception, and Go has neither. Handler::handleShutdown is what
// register_shutdown_function installs to catch a fatal error on the way out;
// Go has no shutdown hook, and a runtime fatal cannot be observed at all. What
// is left of the three is recover(), which is Recover, and Register is where a
// kernel gets it from.
//
// # The two ways a request fails, and the one place they meet
//
// A handler either returns an error or panics. Both arrive here: Recover turns
// a panic into a value the Handler answers, and the routing layer hands the
// Handler whatever a controller action returned. One Handler, one decision about
// what the person in front of the browser sees.
//
// Report and Render are two calls and the caller makes both, which is what the
// kernel does in the PHP: it reports the throwable and then renders it. Render
// used to report on its way in as well, so an application written that way wrote
// every failure to the log twice. Recover is the exception and says so where it
// does it: a panic is news whatever it classifies as, so it logs what it caught
// itself.
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
// AbortIf and AbortUnless are abort_if() and abort_unless(), and they return
// nil when the condition does not call for a failure -- which is what lets the
// caller write one line instead of the same if statement twice. They are kept
// under their own names by ADR 0044: docs/31 refused them by name under RULE 9
// and that line predates the ADR.
//
// Abort builds an *HTTPError, StatusOf reads the status back out of an error
// chain, and classify is the closed table that says what the collection's own
// sentinels mean -- auth.ErrForbidden is 403, an expired CSRF token is 419.
// Before this existed, every error leaving a handler became 500, including the
// ones that had already said exactly what they were.
//
// The table is closed on purpose (RULE 15). An application that wants a status
// says so with Abort; it does not get a second mechanism for the same sentence.
// Map is not that second mechanism: it turns somebody else's error -- a
// driver's, a library's -- into one of these, and the answer still comes from
// the one table.
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
// The two are the two Displayers: PlainDisplayer and DebugDisplayer. Both are
// html/template inline in this package rather than kyse views, because they
// have to render when the view build is what broke (RULE 13). It is also why
// there is no WhoopsDisplayer and no SymfonyDisplayer: both name a renderer
// this collection does not carry.
//
// An application overrides a status page by providing a view named errors/404,
// errors/403 and so on, and wiring it through Config.Views. Nothing is required:
// the built-in pages answer until somebody wants their own.
//
// # Exceptions::fake, and what a test writes instead
//
// Exceptions::fake is reason 2 of ADR 0044 -- a method that only serves the
// container, a facade or a service provider. It is Facade::swap putting an
// ExceptionHandlerFake where the handler contract was bound, so that a failure
// anywhere in the application is recorded instead of logged. There is no
// container (ADR 0001) and no facade (ADR 0002), so there is no binding to
// swap, and a package-level handler a test could swap would be shared mutable
// state that two tests calling t.Parallel would fight over (ADR 0045).
//
// The [Handler] a test holds is one it built, and the recording is a callback
// on it:
//
//	var reported []error
//	h := exception.NewHandler(exception.Config{})
//	h.Reportable(func(err error) { reported = append(reported, err) }).Stop()
//
//	placeOrder(ctx, g, h)
//
//	if len(reported) != 1 {
//		t.Fatalf("reported %d failures, want 1", len(reported))
//	}
//
// [ReportableHandler.Stop] is what makes it a fake rather than a bystander:
// reporting ends at the callback, so nothing reaches the log and the test
// output stays the failures the test itself printed. What the fake offers on
// top of recording is here under Illuminate's own names -- [Handler.Ignore],
// [Handler.Map], [Handler.Level] and [Handler.DontReportDuplicates] -- and each
// is set on the handler the test owns, so two handlers never see each other.
//
// # Absolute rule
//
// Nothing that reveals the inside of the process -- the debug page, a stack, an
// error string that is not an *HTTPError message -- may be reachable when
// Config.Dev is false.
package exception
