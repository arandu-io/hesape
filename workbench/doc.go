// Package workbench generates a Go module that an Arandu application can import.
//
// It answers to Illuminate\Workbench, which was six files and thirteen public
// methods (measured against laravel_illuminate/workbench, the archived
// illuminate/workbench at v5.0.0). Laravel deleted it in 5.1, and
// docs/31-reorganizacao-hesape.md:123 says what replaces it: a console command,
// `aru make:package`. That is what this is -- the generator, with the console
// command that drives it.
//
//	creator := workbench.NewPackageCreator()
//	config := workbench.Config{Path: "./workbench", Name: "Ada Lovelace", Email: "ada@example.com"}
//	registry.Add(workbench.NewWorkbenchMakeCommand(creator, config, nil).Command())
//
//	$ aru make:package acme/invoice-manager
//	Package workbench created: workbench/acme/invoice-manager
//	  module ......................... github.com/acme/invoice-manager
//
// # What it writes
//
//	workbench/acme/invoice-manager/
//	├── go.mod            module path and go directive; go mod tidy fills the rest
//	├── doc.go            the package comment pkg.go.dev publishes
//	├── module.go         the foundation.Module implementation
//	├── module_test.go    pins the name, registers the routes
//	├── README.md         install and wire
//	├── LICENSE           MIT (ADR 0008)
//	└── .gitignore
//
// With --resources it also writes migrations/, resources/views/ and assets/,
// each with a .gitkeep. [PackageCreator] documents which of PHP's five support
// directories those two replace, and why the other three are absent.
//
// The generated module compiles and its test passes before a line is added to
// it. That is the bar a generator has to clear: output that does not build
// teaches the author to distrust the tool on the first command they run.
//
// # The names
//
// [Package], [PackageCreator], [Start] and [WorkbenchMakeCommand] are PHP's,
// and so is every exported method on them -- [PackageCreator.Create],
// [PackageCreator.CreateWithResources], [PackageCreator.WriteSupportFiles],
// [PackageCreator.WriteSupportDirectories], [PackageCreator.WritePublicDirectory],
// [PackageCreator.WriteTestDirectory], [PackageCreator.WriteServiceProvider],
// [PackageCreator.WriteIgnoreFile], [Package.GetFullName] and
// [WorkbenchMakeCommand.Fire]. What each of them writes changed; what it is
// called did not (ADR 0044). Where the contents diverge from PHP, the method's
// own comment says so and says why.
//
// Two methods of the thirteen are absent, both reason 2 of ADR 0056 -- a method
// that only serves the container, a facade or a service provider. ADR 0001
// removed the container and ADR 0002 the facade:
//
//   - WorkbenchServiceProvider::register binds PackageCreator as
//     'package.creator' and WorkbenchMakeCommand as 'command.workbench', then
//     hands the second name to $this->commands(). Constructing the value and
//     passing it is the whole of that: [NewPackageCreator] and
//     [NewWorkbenchMakeCommand], with [WorkbenchMakeCommand.Command] added to
//     the console application.
//   - WorkbenchServiceProvider::provides lists those same two names. Nothing is
//     deferred when nothing is resolved by name.
//
// # Where the surface differs from PHP, in one list
//
//   - [Package] gains Host, PackageName and [Package.ModulePath]. A PHP package
//     is "vendor/name" and Packagist resolves it; a Go module is a URL, and the
//     package clause is an identifier that a hyphen is not legal in.
//   - [NewPackage] returns (*Package, error). PHP cannot fail and produces a
//     module path that go(1) rejects several steps later.
//   - [Start] returns the module directories instead of loading them. There is
//     no runtime require in Go; the equivalent is a go.work entry, and its own
//     comment has the command.
//   - [WorkbenchMakeCommand.Fire] runs `go mod tidy` where PHP runs
//     `composer install --dev`, through hesape/process so a test can fake it.
package workbench
