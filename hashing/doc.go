// Package hashing writes and verifies password hashes.
//
// Make writes argon2id. Check reads back argon2id, argon2i and bcrypt, which is
// what a users table imported from an existing application holds, so those rows
// authenticate on the first sign-in and NeedsRehash walks them forward from
// there. Info reports the parameters a stored hash was written with, and
// IsHashed answers the one question worth asking before writing a password
// column: whether a plaintext leaked into it.
//
// [AbstractHasher], [ArgonHasher], [Argon2IdHasher] and [BcryptHasher] are the
// hashers, and they are all here for the same reason: a users table written by
// one of them has to be read back and compared against the parameters it was
// written with. Each carries Make, Check, Info, NeedsRehash and
// VerifyConfiguration, plus the setters for its own cost parameters --
// SetMemory, SetTime and SetThreads on the argon2 pair, SetRounds on bcrypt.
// The per-call parameters every one of them accepts are [Options].
//
// The package-level [Make] is not one of them and is not configurable: its
// argon2id parameters are compiled in and deliberately not reachable from the
// environment, because an insecure hash configuration is the most common way to
// break authentication without noticing. A hasher takes its numbers from the
// caller; [Make] is the framework's own path.
//
// [HashManager] reads the configured driver name and hands back the hasher it
// names, which is a real choice an application makes: Driver, GetDefaultDriver,
// CreateBcryptDriver, CreateArgonDriver, CreateArgon2idDriver, and the Make,
// Check, Info, NeedsRehash, IsHashed and VerifyConfiguration that forward to
// the driver. It takes a [Config], which is where it reads that name from.
//
// The package knows nothing about users, sessions or requests. It takes a
// string and returns a string, and the only third-party import is
// golang.org/x/crypto.
package hashing
