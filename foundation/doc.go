// Package foundation boots the application. It is Arandu's Foundation.
//
// It is the single place where an application is composed. One difference
// matters: what this package builds is built ONCE, at process start, not per
// request, so nothing here may assume request scope.
//
// # What lives here
//
// The Module contract and its optional interfaces -- Bootable, Background,
// Closable, Diagnostic, Health, Schedulable, Migratable -- are the whole
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
// # What is not here yet
//
// Kernel itself, the Module interface, RendererProvider and Builtins are
// waiting on the layer below: they name a router, a view renderer and a console
// command, and hesape/routing, hesape/httpx and hesape/console are still empty.
// Declaring those three surfaces from up here to unblock the kernel would be a
// second definition of each (RULE 9), thrown away the day the real one lands.
// Until then they stay in github.com/arandu-io/framework/kernel, which is what
// docs/31-reorganizacao-hesape.md means by moving in phases.
package foundation
