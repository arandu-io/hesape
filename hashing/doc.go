// Package hashing mirrors Illuminate\Hashing.
//
// The files it answers to, in the clone at
// laravel_illuminate/hashing:
//
//	AbstractHasher.php
//	Argon2IdHasher.php
//	ArgonHasher.php
//	BcryptHasher.php
//	HashManager.php
//	HashServiceProvider.php
//
// Make writes argon2id. Check reads back argon2id, argon2i and bcrypt, which is
// what a users table imported from a PHP application holds, so those rows
// authenticate on the first sign-in and NeedsRehash walks them forward from
// there. Info reports the parameters a stored hash was written with, and
// IsHashed answers the one question worth asking before writing a password
// column: whether a plaintext leaked into it.
//
// The four hasher classes are here as well, because a users table imported from
// a PHP application was written by one of them and the parameters it was written
// with have to be read back and compared. AbstractHasher, ArgonHasher,
// Argon2IdHasher and BcryptHasher carry the names of their methods: Make, Check,
// Info, NeedsRehash, VerifyConfiguration, SetMemory, SetTime, SetThreads and
// SetRounds. Their defaults are the PHP class properties, and the $options array
// every one of them accepts is the Options struct.
//
// The package-level Make is not one of them and is not configurable: its
// argon2id parameters are compiled in and deliberately not reachable from the
// environment, because an insecure hash configuration is the most common way to
// break authentication without noticing. A hasher is the mirror of the PHP
// class and takes its numbers from the caller; Make is the framework's own path.
//
// HashManager has no counterpart: there is no driver to select, because there is
// no container to select it from. HashServiceProvider is ADR 0001.
//
// The package knows nothing about users, sessions or requests. It takes a
// string and returns a string, and the only third-party import is
// golang.org/x/crypto.
package hashing
