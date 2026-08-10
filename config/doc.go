// Package config mirrors Illuminate\Config.
//
// The files it answers to, in the clone at
// laravel_illuminate/config:
//
//	Repository.php
//
// It holds: App, Env, LoadDotenv, String, MustString, Bool, Int, Seconds.
//
// # There is no Repository, and that is the whole difference
//
// Illuminate\Config is one class: a map with dotted keys, read as
// config("app.name"). The mirror of it here is a typed struct, so a wrong key
// is a compile error instead of a nil that surfaces on the first request that
// happens to need it. Nothing in this package looks a setting up by string at
// the point of use, nothing here is a map, and there is no set: configuration
// is read at boot and never written.
//
// # Load is the one entry point
//
// [Load] reads a .env from the working directory when there is one -- filling
// only what the environment has not already defined -- and then validates the
// result. There is no second loader and no flag that moves the file.
//
// It fails at boot, not on the first request. A configuration that cannot be
// served is a process that must not start.
//
// # What this package does not carry
//
// [App] is the identity of the application and the key it signs with, and
// nothing else. The connection belongs to the database package, the store URL
// to cache and session, the TTLs to session, the level and the tracing secret
// to log. Each of those parses what it needs from the environment this package
// has already populated, which is why [LoadDotenv] and the readers below are
// exported: they are the mechanism, and the mechanism is shared.
package config
