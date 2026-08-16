// Package concerns is intentionally empty.
//
// The status-check methods it might have held as a mixin -- Ok, Created,
// NotFound and the rest -- are plain methods on Response instead: Go has no
// mechanism for mixing a set of methods into a type from outside it.
package concerns
