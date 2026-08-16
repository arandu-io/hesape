// Package container exports nothing, and exists to say why.
//
// There is no dependency injection container in this module. A type declares
// its collaborators as fields, a New function takes them as parameters, and the
// assembly is written out by hand in bootstrap/app.go. The interface is
// declared by the consumer rather than the provider, so a missing or mistyped
// collaborator is a build failure at the call site rather than a lookup that
// fails on the first request to reach it.
//
// Nothing is shared unless a line of code shares it, so there is no registry to
// reset between tests, and a request-scoped value is a local variable in the
// handler goroutine that the garbage collector reclaims when the handler
// returns.
package container
