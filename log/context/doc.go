// Package context mirrors Illuminate\Log\Context.
//
// The files it answers to, in the clone at
// laravel_illuminate/log/Context:
//
//	ContextLogProcessor.php
//	ContextServiceProvider.php
//	Repository.php
//
// The clone is the source. Repository.php was checked against
// reference_laravel/framework/src/Illuminate/Log/Context/Repository.php and the
// only difference is a `@return static` that became `@return $this` in a
// docblock, so nothing here comes from the second source.
//
// # What this is
//
// Repository is the context that crosses a whole request and lands in every log
// line it produces. Add a value once, at the edge, and every line after it
// carries the value without any call site passing it down:
//
//	ctx = context.Into(ctx, context.New(dispatcher))
//	context.For(ctx).Add("order_id", order.ID)
//
// It has a visible half and a hidden half. The visible half reaches the log
// line, through ContextLogProcessor. The hidden half never does -- it exists so
// that something can travel with a queued job without being printed.
//
// # The difference of mechanism
//
// The names are Illuminate's, without exception (ADR 0044): Add is add, AddIf
// is addIf, Push is push, Pop is pop, PullHidden is pullHidden, StackContains
// is stackContains, Dehydrate is dehydrate. What changed is where the
// repository lives, and this is the one thing a Laravel developer has to
// relearn here.
//
// In Laravel there is one repository per process, bound scoped in the container
// by ContextServiceProvider and reached from anywhere through the Context
// facade:
//
//	Context::add('order_id', $order->id);
//	Context::all();
//
// Both halves of that are rejected here -- the container by ADR 0001 and the
// facade by ADR 0002 -- and neither would survive Go anyway: PHP-FPM gives one
// request one process, so a process-wide singleton is request-scoped for free,
// while a Go server runs every request in the same process at the same time and
// a shared repository would be one request reading another request's context.
//
// So the carrier is the context.Context. Into puts a repository in, For reads
// it back, and the request scope that Laravel gets from the process model is
// the scope Go already has. Into and For are the only two exported names in
// this package with no counterpart in Illuminate, and this paragraph is why.
//
// Every method is safe on a nil receiver, so a caller that got a repository out
// of a context that carries none does not have to check: reading gives the zero
// value and writing goes nowhere.
//
// # Exceptions
//
// Where Illuminate throws, this returns an error (ADR 0044): Push, Pop,
// PushHidden, PopHidden, StackContains, HiddenStackContains, Dehydrate and
// Hydrate. The messages are Illuminate's RuntimeException messages.
//
// # What is not here
//
// ContextServiceProvider has no counterpart -- it is registration in the
// container, and the container is rejected. What it does at boot is worth
// knowing anyway, because it says what a queue integration has to call: on the
// way out, Dehydrate and attach the result to the job payload; on the way in,
// Hydrate from it.
package context
