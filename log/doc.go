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
// The clone is the source. LogManager.php was checked against
// reference_laravel/framework/src/Illuminate/Log/LogManager.php, which is
// ahead of it in three places that matter: the `monthly` driver and the
// `max_files` key, which are here and are the only things taken from the second
// source; enum channel names, which Go does not need because a Go constant is
// already a string; and LogManager::stack, whose channel name the clone passes
// under a key parseChannel never reads -- a bug fixed upstream, so Stack here
// names the stack the way the current Laravel does. The other four files are
// identical in both.
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
// through.
//
// ParseLevel reads the eight PSR-3 level names and New renders all eight back
// under those names. Capture is the same logger writing into memory, which is
// what a test asserts against.
//
// # Logger and LogManager
//
// Logger answers Illuminate\Log\Logger: the eight PSR-3 levels under their own
// names -- Emergency, Alert, Critical, Error, Warning, Notice, Info, Debug --
// plus Log and Write for an arbitrary level, WithContext and WithoutContext for
// the context every future line carries, and Listen for the MessageLogged event
// each written line fires.
//
// LogManager answers Illuminate\Log\LogManager: Channel and Driver resolve a
// channel by name out of config/logging.php and cache it, Stack fans one line
// out to several, Build makes a channel that is not in the file, Extend
// registers a driver the manager does not know, and ShareContext gives every
// channel the same fields. It is a logger itself, writing to the default
// channel, exactly as Illuminate's __call forwards to driver().
//
// Two things Illuminate resolves from the container arrive as arguments here
// instead, because the container is rejected (ADR 0001): the configuration,
// which is Config rather than $app['config'], and the event dispatcher, which
// is the Dispatcher interface rather than $app['events'].
//
// Where Illuminate throws, this returns an error (ADR 0044). It also still
// returns the emergency logger alongside it, which is the object Illuminate
// builds so that a broken logging configuration does not take the request down
// with it: a caller who wants to keep logging ignores the error, and a caller
// who wants to know reads it.
//
// # No Monolog
//
// There is no equivalent of Monolog here, and there does not need to be one.
// Monolog exists because PHP has no logging in its standard library; slog is
// already the chainable handler that Monolog's handler stack is, and it is
// stdlib rather than a dependency. A channel is a slog.Handler, a stack is a
// handler that writes into several, and the context processor is a handler that
// wraps another.
//
// That is why ChannelConfig has no `handler`, `processors`, `formatter`,
// `tap` or `action_level` field: each of the five names a Monolog class to
// build. What they were for survives under slog's own names -- a formatter is
// the Format field, which is slog's two handlers, and a processor is a wrapping
// handler, which is what log/context.ContextLogProcessor is.
//
// The eight drivers of config/logging.php that this ecosystem does carry are
// single, daily, monthly, stack, stderr, errorlog, null and custom. Slack,
// syslog and monolog are not among them: the first two are Monolog handlers
// with no stdlib counterpart, and the third exists only to name a Monolog class.
//
// # Context
//
// Illuminate\Log\Context is the subpackage log/context: the context that crosses
// a whole request and lands in every line of it. Its names are Illuminate's --
// Add, AddHidden, Push, Pop, Pull, Only, Except, Increment, Scope, Dehydrate --
// and what changed is where the repository lives, which is the context.Context
// rather than a container singleton reached through a facade. That package's
// documentation says why.
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
