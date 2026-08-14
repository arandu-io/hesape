// Package foundation boots the application. It is Arandu's Foundation.
//
// It is the single place where an application is composed. One difference
// matters: what this package builds is built ONCE, at process start, not per
// request, so nothing here may assume request scope.
//
// # What lives here
//
// [Module] and its optional interfaces -- Bootable, Background, Closable,
// Diagnostic, Health, Schedulable, Migratable -- are the whole
// vocabulary of composition. A module declares; the kernel collects; nothing
// runs until something asks. That shape is why [Task] and [Migration] are
// declared here rather than in the packages that execute them: the scheduler
// and the migration runner are consumers, and a module must be able to hand its
// declarations straight to either one.
//
// It also owns what the framework mounts for itself. Everything under
// internalPrefix -- the health probe, the debug console, the development
// reload -- answers to the framework rather than to the application, and
// exceptInternal is the one place that boundary is enforced. [Observe] installs
// the request id, the request logger and, in development or under an authorized
// tracing header, the Collector behind that console. The live reload in
// reload.go follows a restart in development and costs nothing anywhere else.
//
// # What is not here, and is not coming
//
// Kernel, RendererProvider and Builtins are not in this package, and as of
// 14/08/2026 that is settled rather than pending. [Module] is here for a reason
// that does not apply to them: it named a router, hesape/routing now has one,
// and it landed here because the four adapter packages ADR 0048 folds in
// implement it and cannot compile without it.
//
// The other three are the boot sequence, the view renderer and the console
// command, and they belong to the layer above. ADR 0049 fixed the line: of the
// 37 illuminate/* packages laravel/framework publishes, none is Foundation --
// it ships only inside the framework itself. So the boot lives in
// github.com/arandu-io/framework/foundation, where Kernel is now Application
// and RendererProvider is declared, and this package keeps the vocabulary a
// module needs to declare itself.
//
// The split is not aesthetic. If Module moved up with them, this module would
// have to require the framework to compile its own adapters -- a circular
// module dependency, and a second line in a go.mod that has one. The precedent
// is exact: Illuminate\Contracts\Foundation\Application is split out,
// Illuminate\Foundation\Application is not.
//
// Builtins is the one that has neither a home nor a shape yet. The reasons are
// written where the decision has to be taken, in that package's doc.go.
//
// This section used to say the three were "still waiting" and named
// framework/kernel as where they stayed. That was true when it was written and
// stopped being true when foundation landed; it is corrected rather than
// deleted, because a doc comment is published on pkg.go.dev and a reader who
// remembers the old sentence deserves to see what replaced it.
package foundation
