// Package context mirrors Illuminate\Log\Context.
//
// The files it answers to, in the clone at
// laravel_illuminate/log/Context:
//
//	ContextLogProcessor.php
//	ContextServiceProvider.php
//	Repository.php
//
// Nothing is implemented here, and nothing should be: the component landed one
// directory up. Context::add is log.With, Context::all is log.Fields, and the
// carrier is a context.Context rather than a global repository, so there is
// nothing to dehydrate into a queued job -- the job already takes a context.
//
// A Repository here would be the second way to attach a field to a log line.
// See docs/31-reorganizacao-hesape.md, which lists With, Field and Fields on the
// log package itself.
package context
