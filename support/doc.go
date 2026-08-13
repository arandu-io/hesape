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
// of the three. That covers driver, getDrivers, forgetDrivers, purge,
// forgetInstance, getContainer, setContainer, setAsGlobal, useClass,
// useFactory, booted, booting, callBootedCallbacks, callBootingCallbacks,
// register, provides, isDeferred, commands, pathsToPublish, publishableGroups,
// publishableMigrationPaths, publishableProviders, defaultProviders,
// addProviderToBootstrapFile and setApplication.
//
// Composer.php -- dumpAutoloads, dumpOptimized, findComposer, hasPackage,
// requirePackages, removePackages, setWorkingPath, getVersion, modify -- drives
// the PHP package manager from application code, which is a thing to do when
// the autoloader is a file that has to be regenerated. Go links its packages at
// build time and the toolchain is `go mod`; there is nothing here to shell out
// to at runtime.
//
// Reflector.php -- getClassAttribute, getClassAttributes, getParameterClassName,
// getParameterClassNames, isCallable, isParameterSubclassOf,
// isParameterBackedEnumWithStringBackingType -- reads PHP attributes and
// parameter type hints to decide what to inject. It is the container's
// eyesight, and there is no container.
//
// Pluralizer::inflector returns the Doctrine Inflector instance. The str
// package carries the inflection itself; the getter exists in PHP so a caller
// can reach past it into a third-party object, and there is no third-party
// object to reach.
//
// getIterator, jsonSerialize, toHtmlString and toResponse are PHP interface
// methods -- IteratorAggregate, JsonSerializable, Htmlable and Responsable.
// Go's equivalents are range-over-func, MarshalJSON, and returning an
// http.Handler, and the types here satisfy those instead. Uri is the one that
// keeps both names: [Uri.ToHtml] and [Uri.ToResponse] are the PHP ones, because
// a caller coming from Laravel types them.
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
//
// # The rest of helpers.php
//
// literal() builds an anonymous object out of named arguments. Go has neither,
// and the thing it stands in for -- a value with named fields, declared where it
// is used -- is a struct literal.
//
// class_uses_recursive() and trait_uses_recursive() walk a class's traits. Go
// has no traits: an embedded struct is a field, and what these answer is
// answered by the type system.
//
// php_binary() and artisan_binary() find the interpreter and the console script
// so that a command can re-invoke itself. An Arandu application is one compiled
// binary and knows where it is: os.Executable.
//
// # MultipleInstanceManager
//
// getDefaultInstance, setDefaultInstance and getInstanceConfig belong to it, and
// it is Manager with the instances keyed by name instead of by driver. Both are
// container machinery, refused with the rest of it above.
//
// # The facades are not measured
//
// plans/cobertura.sh skips Support/Facades entirely, and this is the note that
// says why so the skip is not mistaken for an oversight. Every method on a
// facade forwards to a method on the package behind it -- Http::fakeSequence to
// the HTTP client's, DB::prohibitDestructiveCommands to the connection's -- so
// counting them here counts the same method twice, and counts it against a
// package that was never going to hold it.
package support
