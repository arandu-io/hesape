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
// DateFactory has five methods that choose where an instant comes from, and
// three of them are here: DateFactory::use is [Use], DateFactory::useCallable is
// [UseCallable] and DateFactory::useDefault is [UseDefault]. The two that are
// not are DateFactory::useClass and DateFactory::useFactory:
//
//   - DateFactory::useClass stores a class name and the factory later runs
//     `new $this->dateClass(...)` on it. Reason 1 of the porting rule: Go
//     builds no type out of a string, and the Carbon subclass it exists to
//     select does not exist either, because the value is time.Time.
//   - DateFactory::useFactory stores a nesbot/carbon Factory object and calls
//     through it. Reason 3: a library this ecosystem does not carry.
//
// Both of them are one seam in Go, and it is the seam the other two already
// are -- an interface the test replaces:
//
//	support.UseCallable(func() time.Time { return fixed })
//	defer support.UseDefault()
//
// # What was left out, and why
//
// Manager.php, MultipleInstanceManager.php, ServiceProvider.php,
// AggregateServiceProvider.php, DefaultProviders.php,
// Traits/CapsuleManagerTrait.php and Reflector.php serve the container, the
// facades and the service providers, which ADR 0001 and ADR 0002 rejected.
// Nothing in them is reachable from a package that has none of the three. That
// covers driver, getDrivers, forgetDrivers, purge, forgetInstance,
// getContainer, setContainer, setAsGlobal, booted, booting,
// callBootedCallbacks, callBootingCallbacks, register, provides, isDeferred,
// commands, pathsToPublish, publishableGroups, publishableMigrationPaths,
// publishableProviders, defaultProviders, addProviderToBootstrapFile and
// setApplication.
//
// useClass and useFactory used to be on that list, and they do not belong to
// any of those files: they are DateFactory.php, and "Carbon is not a type"
// above is where they are answered. The reason given here was false -- neither
// touches the container -- and it is written down rather than quietly deleted
// so the list is not repopulated from the old text.
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
// carries the helper. This is not a refusal -- every one of these is written --
// but the helper's own spelling is gone, so the measurement asks for it and this
// is the answer:
//
//   - optional() is [NewOptional], which is Optional::__construct with the
//     name the type took. The second form, optional($value, $callback), is a
//     nil check in Go, and where it is worth a name it is [Transform].
//   - fluent() is [NewFluent], which is Fluent::__construct, and [Make] is the
//     same constructor under the name Fluent::make.
//   - env() is [Env].Get, and Sleep::sleep is [For] with a unit.
//
// The one that goes the other way is once(): the class is a store nobody types,
// so [Once] is the helper and the store is [Instance].
//
// # The helpers of helpers.php with no counterpart
//
// Three, and all three are reason 1 of the porting rule: a PHP language feature
// Go does not have. Nothing here is skipped for any other reason.
//
// literal() takes named arguments and casts them to stdClass, so the caller gets
// a value with named fields without declaring a class. Go has neither named
// arguments nor an object built at runtime out of them, and what the helper
// stands in for is a literal the compiler checks:
//
//	// literal(name: 'Ana', age: 30)
//	person := struct {
//		Name string
//		Age  int
//	}{Name: "Ana", Age: 30}
//
// class_uses_recursive() and trait_uses_recursive() walk the traits of a class,
// its parents and their traits. Go has no traits: an embedded struct is a field,
// not a set the program can list. What the pair is used for in Laravel is asking
// at runtime whether a class carries a behaviour, and here that is a declared
// field -- the SoftDeletes trait is the SoftDeletes field of
// [github.com/arandu-io/hesape/database/eloquent.Model], and the check the
// helper existed for is the field:
//
//	// in_array(SoftDeletes::class, class_uses_recursive($model))
//	if model.SoftDeletes {
//		// deleted rows are filtered out and delete becomes an update
//	}
//
// Where the behaviour is a method set rather than a flag, the same question is a
// type assertion, and either way the compiler has already answered it.
//
// # php_binary and artisan_binary
//
// These two are not refused, and the note is here because the list above is
// where somebody would look for them. functions.php declares them so that a
// command can re-invoke itself: five call sites in the clone, and every one of
// them reaches the helper through a wrapper of its own.
//
// php_binary() and artisan_binary() are what Console\Application::phpBinary and
// Application::artisanBinary wrap, and that pair is the one that came across:
// [github.com/arandu-io/hesape/console.PhpBinary] is os.Executable, because a Go
// program is its own interpreter, and
// [github.com/arandu-io/hesape/console.ArtisanBinary] is the empty string,
// because the console script and the interpreter are one file. The other three
// call sites are wrappers that are not public surface and are not measured:
// Queue\Listener::phpBinary and ::artisanBinary are protected, and
// Composer::phpBinary is protected and goes with the rest of Composer.php
// above.
//
// They are in console rather than here for the reason RULE 9 gives: one of them
// is the whole implementation, and a copy under the helper's own spelling would
// be a second way to write the same call.
//
// # MultipleInstanceManager
//
// MultipleInstanceManager::getDefaultInstance,
// MultipleInstanceManager::setDefaultInstance and
// MultipleInstanceManager::getInstanceConfig are abstract: the base class has no
// body for any of them, and it is Manager with the instances keyed by name
// instead of by driver.
//
// They are not refused either. What fills them in is the concrete manager, and
// the clone has exactly one subclass -- Concurrency\ConcurrencyManager -- which
// is [github.com/arandu-io/hesape/concurrency.Manager], with
// GetDefaultInstance, SetDefaultInstance and GetInstanceConfig on it under those
// names. Nothing was left to write in this package, because in PHP there is
// nothing in this package either.
//
// The base class around them does not come across: resolve dispatches on
// 'create'.ucfirst($driver).'Driver' with method_exists, which is reason 1, and
// it reads the container for both $app and $config, which is reason 2. That
// covers purge, forgetInstance and setApplication above, and
// Manager::getDefaultDriver, the abstract each Manager subclass fills in with a
// lookup into the container's config.
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
