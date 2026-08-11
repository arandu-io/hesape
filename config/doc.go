// Package config mirrors Illuminate\Config.
//
// The files it answers to, in the clone at
// laravel_illuminate/config:
//
//	Repository.php
//
// It holds: Repository, App, Env, LoadDotenv, String, MustString, Bool, Int,
// Seconds.
//
// # Two readers, and the difference between them
//
// Illuminate\Config is one class: a map with dotted keys, read as
// config("app.name"). [Repository] is that class, method for method -- Has,
// Get, GetMany, String, Integer, Float, Boolean, Array, Collection, Set,
// Prepend, Push, All -- because a Laravel developer types config() and there is
// no second name for it.
//
// [App] is the same settings as a typed struct, and it is what the framework
// itself reads: a wrong field is a compile error there, instead of a nil that
// surfaces on the first request that happens to need it. A first-party package
// that looks a setting up by string is doing it wrong. An application that does
// is doing what the framework it came from does.
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
// # What is not ported, and why
//
// Repository::offsetExists, offsetGet, offsetSet and offsetUnset are the four
// methods of PHP's ArrayAccess -- the language interface behind
// $config['app.name'] and unset($config['app.name']). Go has no operator to
// overload, so there is nothing for them to be. Each is one line in the PHP and
// delegates to a method that is here: offsetExists is [Repository.Has],
// offsetGet is [Repository.Get], offsetSet is [Repository.Set], and offsetUnset
// is Set(key, nil). Nothing is missing; only the square brackets are.
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
