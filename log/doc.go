// Package log mirrors Illuminate\Log, and it is the reason this framework
// exists.
//
// The files it answers to, in the clone at laravel_illuminate/log:
//
//	LogManager.php
//	LogServiceProvider.php
//	Logger.php
//	ParsesLogConfiguration.php
//	functions.php
//
// slog covers structured logging and OpenTelemetry covers production tracing.
// What is missing between the two is the development layer: the moment a request
// broke and you want the stack, the queries with their timing, what you dumped
// and what the framework thinks is wrong, on one screen.
//
// # Logging
//
// New builds the root logger -- readable text in development, JSON everywhere
// else, so it reaches the aggregator without fragile parsing. Middleware puts it
// at the top of the HTTP pipeline, For reads it back inside a handler or a
// service, and With attaches fields that everything downstream inherits. There
// is no exported global logger, on purpose: a log line without request_id and
// tenant is noise, and the only way to guarantee both is to force the context
// through. That is also where Illuminate's LogManager went -- a channel is a
// slog.Handler, chosen once where the application is wired, so there is no
// Log::channel() to reach for a second one from the middle of a service.
//
// ParseLevel reads the eight PSR-3 level names and New renders all eight back
// under those names. Capture is the same logger writing into memory, which is
// what a test asserts against.
//
// # The Collector
//
// The Collector is the development layer, and it is core rather than a plugin,
// so the error page knows the queries, the dumps and the routes without any
// extra install. It accumulates everything that happened inside one request;
// Recorder keeps the last of them in a ring; Console serves them at
// /_arandu/debug and says the probable cause out loud -- the N+1, the slow
// statement, the request that was three quarters database. Transport records
// outbound calls onto the same timeline, Dump records a value without writing to
// stdout or corrupting the response, and EditorLink turns a recorded frame into
// a link that opens the file in the IDE at the line.
//
// It costs nothing when it is off. Outside development, and without an
// authorized tracing header, FromContext returns nil and every Record method is
// a no-op on a nil receiver -- zero cost, not "low cost", and there are
// allocation tests that hold it to that.
//
// The error page itself is not here: it renders a failure rather than recording
// one, and it lives in the exception package.
package log
