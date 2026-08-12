// Package listeners mirrors Illuminate\Auth\Listeners.
//
// The files it answers to, in the clone at
// laravel_illuminate/auth/Listeners:
//
//	SendEmailVerificationNotification.php -> [SendEmailVerificationNotification]
//
// One listener, for one event: a fresh registration whose account has an address
// to confirm gets the link sent to it. It is the only thing in Illuminate\Auth
// that listens rather than being listened to, and it is registered by the
// application, not by this package -- there is no service provider to register it
// from (ADR 0001).
package listeners
