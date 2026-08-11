// Package concerns mirrors Illuminate\Http\Client\Concerns.
//
// It holds traits that are composed into the HTTP client classes.
//
//	DeterminesStatusCode.php — status-check methods (ok, created, notFound, etc.)
//
// In Go, these are methods on the Response struct rather than traits;
// the package exists for parity with the Illuminate surface.
package concerns
