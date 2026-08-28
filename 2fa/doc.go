// Package twofactor declares what a second factor is: how an authenticator
// application is enrolled, how the code it shows is checked, and what a
// recovery code has to be.
//
// The directory is named 2fa and the package is named twofactor because a Go
// identifier cannot begin with a digit, so "package 2fa" does not compile. The
// import path is what a reader goes looking for; the package name is what the
// compiler will accept.
//
// It stores nothing, and that is the shape of it rather than an omission. There
// is no table here, no migration, no account, and no assumption that a table of
// people exists: the secret and the recovery codes belong to whatever owns the
// schema, and this package says what that owner has to guarantee about them.
// What is here is computation -- [Provisioning.URI] and [GenerateRecoveryCodes]
// -- and the two interfaces the computation needs somebody else to implement,
// [ReplayGuard] and [RecoveryStore].
//
// A second factor proves possession of a device. It is not a code delivered to
// an address, which proves only that the address is reachable, and nothing in
// this package sends anything anywhere.
//
// The algorithm itself is in the otp package, which this one uses and which
// does not know this one exists.
package twofactor
