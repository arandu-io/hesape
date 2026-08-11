// Package support mirrors Illuminate\Support.
//
// The source is the clone at laravel_illuminate/support, which is the version
// this package was written against; nothing here was read from
// reference_laravel/framework.
//
// The files it answers to:
//
//	Benchmark.php              -> benchmark.go
//	Carbon.php                 -> clock.go
//	ConfigurationUrlParser.php -> configurationurlparser.go
//	DateFactory.php            -> clock.go
//	Defer/                     -> support/deferpkg
//	Env.php                    -> env.go
//	Fluent.php                 -> fluent.go
//	HtmlString.php             -> htmlstring.go
//	InteractsWithTime.php      -> clock.go
//	Js.php                     -> js.go
//	Lottery.php                -> lottery.go
//	MessageBag.php             -> messagebag.go
//	NamespacedItemResolver.php -> namespaceditemresolver.go
//	Once.php                   -> once.go
//	Onceable.php               -> once.go
//	Optional.php               -> optional.go
//	ProcessUtils.php           -> processutils.go
//	Sleep.php                  -> sleep.go
//	Timebox.php                -> timebox.go
//	Traits/                    -> interactswithdata.go, helpers.go
//	Uri.php                    -> uri.go
//	UriQueryString.php         -> uri.go
//	ValidatedInput.php         -> validatedinput.go
//	ViewErrorBag.php           -> viewerrorbag.go
//	functions.php              -> helpers.go
//	helpers.php                -> helpers.go
//
// Str.php, Stringable.php and Pluralizer.php are the str package; Number.php
// is the number package; Testing/Fakes is support/testing/fakes.
//
// # Carbon is not a type
//
// Carbon.php and DateFactory.php do not become a date type. time.Time already
// is the value, and a second one would be a second way to hold an instant. What
// they are for -- the seam a test moves -- is [Clock], [Travel], [TravelTo],
// [TravelBack] and [FreezeTime], which are the names a test types.
//
// # What was left out, and why
//
// Manager.php, MultipleInstanceManager.php, ServiceProvider.php,
// AggregateServiceProvider.php, DefaultProviders.php and Reflector.php serve
// the container, the facades and the service providers, which ADR 0001 and
// ADR 0002 rejected. Nothing in them is reachable from a package that has none
// of the three.
//
// # The facades, which are not built
//
// ADR 0002 rejected facades: a Go program has no __callStatic and no container
// to resolve against, so a facade is a global with a hidden lookup. The list is
// kept here because it is the exact index of what a Laravel application
// touches, and each entry names the package that answers for it:
//
//	App             Artisan         Auth            Blade
//	Broadcast       Bus             Cache           Concurrency
//	Config          Context         Cookie          Crypt
//	DB              Date            Event           Exceptions
//	Facade          File            Gate            Hash
//	Http            Lang            Log             Mail
//	Notification    ParallelTesting Password        Pipeline
//	Process         Queue           RateLimiter     Redirect
//	Redis           Request         Response        Route
//	Schedule        Schema          Session         Storage
//	URL             Validator       View            Vite
//
// # Names that Go has only one of
//
// PHP keeps class names and function names in two namespaces; Go keeps them in
// one. Where the clone has both, the type holds the name and the constructor
// carries the helper: optional() is [NewOptional], fluent() is [NewFluent],
// env() is [Env].Get, and Sleep::sleep is [For] with a unit. The one that goes
// the other way is once(): the class is a store nobody types, so [Once] is the
// helper and the store is [Instance].
package support
