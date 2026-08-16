// Package events holds the announcements authentication makes: an attempt, a
// sign-in, a sign-out, a failure, a lockout, a registration, a reset and a
// verification.
//
// Each event is a struct with exported fields and no constructor function: a
// composite literal says everything a New would, and thirteen of those would be
// thirteen functions nobody can get wrong in a way the literal can.
//
// [Attempting] and [Failed] carry the credentials, which hold the plain
// password. Nothing redacts them on the way past, so whoever logs one of those
// two events logs a password.
package events
