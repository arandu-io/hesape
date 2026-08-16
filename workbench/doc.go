// Package workbench generates a Go module that an Arandu application can import.
//
// It is the generator behind `aru make:package`, with the console command that
// drives it.
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
//	├── LICENSE           MIT
//	└── .gitignore
//
// With --resources it also writes migrations/, resources/views/ and assets/,
// each with a .gitkeep. [PackageCreator] documents why those three and no
// others.
//
// The generated module compiles and its test passes before a line is added to
// it. That is the bar a generator has to clear: output that does not build
// teaches the author to distrust the tool on the first command they run.
//
// # Signatures worth knowing before reading the code
//
//   - [Package] carries Host, PackageName and [Package.ModulePath], because a
//     Go module is identified by a URL and the package clause is an identifier
//     that a hyphen is not legal in.
//   - [NewPackage] returns (*Package, error), so a name that cannot become a
//     module path fails here rather than several steps later inside go(1).
//   - [Start] returns the module directories instead of loading them. There is
//     no runtime require in Go; the equivalent is a go.work entry, and Start's
//     own comment has the command.
//   - [WorkbenchMakeCommand.Fire] runs `go mod tidy` through hesape/process, so
//     a test can fake it.
package workbench
