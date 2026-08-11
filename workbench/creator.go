package workbench

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed stubs
var stubs embed.FS

// ErrPackageExists answers to the InvalidArgumentException("Package exists.")
// that PackageCreator::createDirectory throws.
var ErrPackageExists = errors.New("workbench: package exists")

// DefaultGoVersion is the go directive written into a generated go.mod.
//
// It tracks the go directive of the hesape module itself: a generated module
// that asks for a newer toolchain than the framework it imports fails to build
// for a reason that names neither.
const DefaultGoVersion = "1.26"

// PackageCreator answers to Illuminate\Workbench\PackageCreator: it turns a
// [Package] into a directory of files.
//
// # What it emits, and why exactly this
//
// Six files make a Go module that an Arandu application can import, and there
// is a seventh that git needs:
//
//	go.mod           the module path and the go directive
//	doc.go           the package comment pkg.go.dev publishes
//	module.go        the foundation.Module implementation -- the contract
//	module_test.go   the test that pins the name and exercises Routes
//	README.md        install and wire, and nothing else
//	LICENSE          MIT (ADR 0008)
//	.gitignore       build output, compiled views, .env
//
// # How that maps onto the PHP
//
// PHP writes phpunit.xml, .travis.yml, composer.json, .gitignore, a tests/
// directory and src/{Vendor}/{Name}/{Name}ServiceProvider.php. The blocks keep
// their names here and their contents change:
//
//   - composer.json becomes go.mod. The generated file declares the module and
//     the go directive and nothing else: the require lines are what
//     `go mod tidy` writes, and a generator that guessed a version of hesape
//     would be pinning one it cannot know.
//   - phpunit.xml has no counterpart. `go test ./...` needs no configuration
//     file, and an empty one would be a file to maintain that answers nothing.
//   - .travis.yml has no counterpart either, and that one is a choice rather
//     than an absence: a CI file picks the author's forge for them, on the
//     first minute of a package that has no code in it yet.
//   - the ServiceProvider becomes module.go plus doc.go. foundation.Module is
//     what a package implements to plug into an application, and its own
//     documentation says why the name is Module and not ServiceProvider.
//   - tests/ becomes module_test.go, at the module root. A Go test lives beside
//     the code it tests; a tests/ directory would be an empty directory with a
//     .gitkeep in it, which is what PHP writes because PHPUnit needs the path
//     to exist.
//
// The zero value is usable: [PackageCreator.Create] fills GoVersion and Year
// from [DefaultGoVersion] and the clock when they are empty.
type PackageCreator struct {
	// GoVersion is the go directive written into go.mod. Empty means
	// [DefaultGoVersion].
	GoVersion string

	// Year is the copyright year in LICENSE. Zero means the current year.
	//
	// It is a field so a test can pin it. A generated file whose contents
	// change on 1 January is a generated file whose test fails on 1 January.
	Year int
}

// NewPackageCreator answers to PackageCreator::__construct.
//
// PHP takes an Illuminate\Filesystem\Filesystem. This takes nothing and uses
// the os package directly, because the framework's own filesystem.Disk carries
// an auth.Grant and a tenant prefix (RULE 14) and a code generator writes into
// the developer's working tree, which has neither a grant nor a tenant.
func NewPackageCreator() *PackageCreator { return &PackageCreator{} }

// basicBlocks answers to PackageCreator::$basicBlocks: what a plain package
// gets.
var basicBlocks = []string{"SupportFiles", "TestDirectory", "ServiceProvider"}

// blocks answers to PackageCreator::$blocks: what --resources adds.
var blocks = []string{"SupportFiles", "SupportDirectories", "PublicDirectory", "TestDirectory", "ServiceProvider"}

// Create answers to PackageCreator::create.
//
// It creates path/vendor/name, writes the blocks into it and returns the
// directory. plain is PHP's $plain, and it defaults to true there, which is the
// same as calling this with plain true rather than [PackageCreator.CreateWithResources].
//
// PHP returns the directory and throws when it already exists; this returns
// (string, error) and the error is [ErrPackageExists]. Nothing is written when
// it fails that way: the directory is created first, exactly as in PHP, so a
// second run over an existing package cannot overwrite the first.
func (c *PackageCreator) Create(p *Package, path string, plain bool) (string, error) {
	directory, err := c.createDirectory(p, path)
	if err != nil {
		return "", err
	}

	// Spin through the building blocks that make up a package and call the
	// method that builds each. PHP dispatches on the string with a variable
	// method name; Go has no such call, and reflection is the mechanism this
	// framework rejects, so the switch is written out. It is also the only
	// place that knows the block names, which is what the dynamic call bought.
	for _, block := range c.getBlocks(plain) {
		var blockErr error
		switch block {
		case "SupportFiles":
			blockErr = c.WriteSupportFiles(p, directory, plain)
		case "SupportDirectories":
			blockErr = c.WriteSupportDirectories(p, directory)
		case "PublicDirectory":
			blockErr = c.WritePublicDirectory(p, directory, plain)
		case "TestDirectory":
			blockErr = c.WriteTestDirectory(p, directory)
		case "ServiceProvider":
			blockErr = c.WriteServiceProvider(p, directory, plain)
		default:
			blockErr = fmt.Errorf("workbench: unknown block %q", block)
		}
		if blockErr != nil {
			return "", blockErr
		}
	}

	return directory, nil
}

// CreateWithResources answers to PackageCreator::createWithResources: Create
// with the resource directories.
func (c *PackageCreator) CreateWithResources(p *Package, path string) (string, error) {
	return c.Create(p, path, false)
}

// getBlocks answers to PackageCreator::getBlocks.
func (c *PackageCreator) getBlocks(plain bool) []string {
	if plain {
		return basicBlocks
	}
	return blocks
}

// WriteSupportFiles answers to PackageCreator::writeSupportFiles.
//
// PHP loops over PhpUnit, Travis, Composer and Ignore. Here it is go.mod,
// README.md, LICENSE and .gitignore -- see the type comment for which of the
// four PHP files survived and why the other two did not.
func (c *PackageCreator) WriteSupportFiles(p *Package, directory string, plain bool) error {
	if err := c.writeGoModFile(p, directory); err != nil {
		return err
	}
	if err := c.writeReadmeFile(p, directory); err != nil {
		return err
	}
	if err := c.writeLicenseFile(p, directory); err != nil {
		return err
	}
	return c.WriteIgnoreFile(p, directory, plain)
}

// writeGoModFile answers to PackageCreator::writeComposerFile.
//
// There is one stub rather than PHP's plain/full pair: a go.mod does not change
// shape when the package ships views, and the require lines that would differ
// are the ones `go mod tidy` writes.
func (c *PackageCreator) writeGoModFile(p *Package, directory string) error {
	return c.writeStub(p, "go.mod.stub", filepath.Join(directory, "go.mod"))
}

// writeReadmeFile has no direct PHP counterpart. PHP's package has no README at
// all, and a Go module without one is a page on pkg.go.dev with an install
// command missing from it.
func (c *PackageCreator) writeReadmeFile(p *Package, directory string) error {
	return c.writeStub(p, "README.md.stub", filepath.Join(directory, "README.md"))
}

// writeLicenseFile writes MIT, which is what ADR 0008 chose for everything in
// this ecosystem. An author who wants another license replaces one file.
func (c *PackageCreator) writeLicenseFile(p *Package, directory string) error {
	return c.writeStub(p, "LICENSE.stub", filepath.Join(directory, "LICENSE"))
}

// WriteIgnoreFile answers to PackageCreator::writeIgnoreFile.
//
// The stub is named gitignore.stub for the reason PHP names its own
// gitignore.txt: a file called .gitignore inside the generator would apply to
// the generator.
func (c *PackageCreator) WriteIgnoreFile(p *Package, directory string, plain bool) error {
	return c.writeStub(p, "gitignore.stub", filepath.Join(directory, ".gitignore"))
}

// WriteSupportDirectories answers to PackageCreator::writeSupportDirectories.
//
// PHP creates five: config, controllers, lang, migrations and views. Two
// survive, and the three that do not are each a decision rather than an
// oversight:
//
//   - config is a typed struct in Go, validated at boot, not a file the package
//     publishes into the application (RULE 9).
//   - controllers is not a directory of its own. A module is the vertical slice
//     (ADR 0003): its handlers live beside its repository, in the package root.
//   - lang is a directory of translation files, and a package with one string
//     in it does not need a directory to hold nothing.
func (c *PackageCreator) WriteSupportDirectories(p *Package, directory string) error {
	for _, support := range []string{"migrations", filepath.Join("resources", "views")} {
		if err := c.writeSupportDirectory(p, support, directory); err != nil {
			return err
		}
	}
	return nil
}

// writeSupportDirectory answers to PackageCreator::writeSupportDirectory.
//
// The .gitkeep is what PHP writes and for the same reason: an empty directory
// is not a thing git stores, so without it the author pushes a package that is
// missing the directories the generator just made.
func (c *PackageCreator) writeSupportDirectory(p *Package, support, directory string) error {
	path := filepath.Join(directory, support)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, ".gitkeep"), nil, 0o644)
}

// WritePublicDirectory answers to PackageCreator::writePublicDirectory.
//
// PHP creates public/, which the application later symlinks or copies out of so
// a web server can serve it. There is no such directory here: an asset enters
// the binary through go:embed and is served by the framework at a hashed URL
// (RULE 13), so the directory a package ships assets in is assets/, and nothing
// outside the module ever reads it from disk.
//
// It writes the directory and the .gitkeep and stops there. The embed line and
// the view.RegisterAsset call belong in the module's own code, once it has an
// asset -- a generated //go:embed over an empty directory does not compile, and
// a generator whose output does not build is worse than one that writes less.
func (c *PackageCreator) WritePublicDirectory(p *Package, directory string, plain bool) error {
	if plain {
		return nil
	}
	return c.writeSupportDirectory(p, "assets", directory)
}

// WriteTestDirectory answers to PackageCreator::writeTestDirectory.
//
// PHP makes tests/ with a .gitkeep in it, because PHPUnit needs the path to
// exist and there is nothing to put in it yet. Here it writes module_test.go at
// the root, beside the code it tests, and the test it writes runs: it pins the
// module name and registers the routes. A generated test that asserts nothing
// teaches the author that generated tests assert nothing.
func (c *PackageCreator) WriteTestDirectory(p *Package, directory string) error {
	return c.writeStub(p, "module_test.go.stub", filepath.Join(directory, "module_test.go"))
}

// WriteServiceProvider answers to PackageCreator::writeServiceProvider.
//
// PHP writes src/{Vendor}/{Name}/{Name}ServiceProvider.php. Here it is module.go
// and doc.go at the module root: Go has no PSR-4 directory-to-namespace rule to
// satisfy, and foundation.Module is what a package implements to be composable.
// The name of this method is Laravel's, and foundation.Module explains why the
// type it writes is not called ServiceProvider.
func (c *PackageCreator) WriteServiceProvider(p *Package, directory string, plain bool) error {
	if err := c.writeStub(p, "module.go.stub", filepath.Join(directory, "module.go")); err != nil {
		return err
	}
	return c.writeStub(p, "doc.go.stub", filepath.Join(directory, "doc.go"))
}

// createDirectory answers to PackageCreator::createDirectory.
func (c *PackageCreator) createDirectory(p *Package, path string) (string, error) {
	fullPath := filepath.Join(path, filepath.FromSlash(p.GetFullName()))

	if _, err := os.Stat(fullPath); err == nil {
		return "", fmt.Errorf("%w: %s", ErrPackageExists, fullPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(fullPath, 0o755); err != nil {
		return "", err
	}
	return fullPath, nil
}

// writeStub reads a stub, substitutes the placeholders and writes the file.
func (c *PackageCreator) writeStub(p *Package, stub, destination string) error {
	contents, err := stubs.ReadFile("stubs/" + stub)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, []byte(c.formatPackageStub(p, string(contents))), 0o644)
}

// formatPackageStub answers to PackageCreator::formatPackageStub.
//
// PHP walks get_object_vars($package) and replaces {{snake_case($key)}} with
// the value. Go has no such walk that is not reflection, and reflection is the
// mechanism this framework rejects, so the pairs are written out. The
// placeholder spelling is PHP's, and the last four have no property behind them
// -- they come from the [Package] fields Go added and from the creator itself.
func (c *PackageCreator) formatPackageStub(p *Package, stub string) string {
	year := c.Year
	if year == 0 {
		year = time.Now().Year()
	}
	goVersion := c.GoVersion
	if goVersion == "" {
		goVersion = DefaultGoVersion
	}

	return strings.NewReplacer(
		"{{vendor}}", p.Vendor,
		"{{lower_vendor}}", p.LowerVendor,
		"{{name}}", p.Name,
		"{{lower_name}}", p.LowerName,
		"{{author}}", p.Author,
		"{{email}}", p.Email,
		"{{package_name}}", p.PackageName,
		"{{module_path}}", p.ModulePath(),
		"{{go_version}}", goVersion,
		"{{year}}", strconv.Itoa(year),
	).Replace(stub)
}
