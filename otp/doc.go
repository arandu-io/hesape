// Package otp computes one-time codes from a shared secret.
//
// [HOTP] is the event-based algorithm of RFC 4226: a secret and a counter go
// in, a code comes out. [TOTP] is the time-based variant of RFC 6238, which is
// the same algorithm with the counter taken from the clock -- and it is what an
// authenticator application on a phone computes.
//
// This package calculates and remembers nothing. It holds no secret, no
// account, no person, and no record of which codes have been seen; every
// function is a function of its arguments. That has one consequence the caller
// must handle rather than assume away: a code stays correct for the whole time
// step it belongs to, so accepting it twice inside that step is a replay.
// [TOTP.Verify] returns the step it matched exactly so the caller -- which is
// the thing that owns storage -- can refuse the second use.
//
// The digest is HMAC-SHA-1 and is not a parameter. The reason to compute codes
// this way rather than invent a scheme is that the application in the person's
// pocket computes the same ones, and those applications speak SHA-1; a stronger
// digest here would produce codes that nothing else can verify. The code length
// is a parameter, because RFC 6238 publishes its own test vectors at eight
// digits while six is what an authenticator shows by default.
package otp
