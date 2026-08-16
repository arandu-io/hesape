// Package listeners holds the listener authentication ships with.
//
// There is one: [SendEmailVerificationNotification] sends the confirmation link
// to a fresh registration whose account has an address to confirm. It is
// registered by the application rather than by this package -- nothing here
// registers itself.
package listeners
