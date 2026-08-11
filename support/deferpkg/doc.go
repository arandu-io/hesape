// Package deferpkg mirrors Illuminate\Support\Defer.
//
// The files it answers to, in the clone at
// laravel_illuminate/support/Defer:
//
//	DeferredCallback.php
//	DeferredCallbackCollection.php
//
// Named deferpkg because `defer` is a Go keyword and cannot be a package
// name. It is the one place the mirror cannot be literal, and ADR 0044 allows
// the suffix for exactly this.
//
// The defer() function of the PHP's own functions.php lives in the support
// package, where its namespace puts it, as support.Defer.
package deferpkg
