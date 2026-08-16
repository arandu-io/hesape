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

// ErrPackageExists is returned when the package directory is already there.
var ErrPackageExists = errors.New("workbench: package exists")

// DefaultGoVersion is the go directive written into a generated go.mod.
//
// It tracks the go directive of the hesape module itself: a generated module
// that asks for a newer toolchain than the framework it imports fails to build
// for a reason that names neither.
const DefaultGoVersion = "1.26"

// PackageCreator turns a [Package] into a directory of files.
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
//	LICENSE          MIT
//	.gitignore       build output, compiled views, .env
//
// # What it does not write
//
// go.mod declares the module and the go directive and nothing else: the require
// lines are what `go mod tidy` writes, and a generator that guessed a version of
// hesape would be pinning one it cannot know.
//
// There is no test configuration file, because `go test ./...` needs none, and
// no CI file, because that picks the author's forge for them on the first minute
// of a package that has no code in it yet.
//
// The test is module_test.go at the module root, beside the code it tests,
// rather than in a directory of its own.
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

// NewPackageCreator builds a PackageCreator.
//
// It takes nothing and uses the os package directly, because the framework's own
// filesystem.Disk carries an auth.Grant and a tenant prefix, and a code
// generator writes into the developer's working tree, which has neither.
func NewPackageCreator() *PackageCreator { return &PackageCreator{} }

// basicBlocks is what a plain package gets.
var basicBlocks = []string{"SupportFiles", "TestDirectory", "ServiceProvider"}

// blocks is what --resources adds on top of basicBlocks.
var blocks = []string{"SupportFiles", "SupportDirectories", "PublicDirectory", "TestDirectory", "ServiceProvider"}

// Create creates path/vendor/name, writes the blocks into it and returns the
// directory.
//
// plain true writes only the basic blocks; [PackageCreator.CreateWithResources]
// is the other half.
//
// A directory that already exists is [ErrPackageExists], and nothing is written
// in that case: the directory is created first, so a second run over an existing
// package cannot overwrite the first.
func (c *PackageCreator) Create(p *Package, path string, plain bool) (string, error) {
	directory, err := c.createDirectory(p, path)
	if err != nil {
		return "", err
	}

	// Spin through the building blocks that make up a package and call the
	// method that builds each. The switch is written out rather than
	// dispatched by name: reflection is the mechanism this framework rejects,
	// and this is the only place that knows the block names.
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

// CreateWithResources is Create with the resource directories.
func (c *PackageCreator) CreateWithResources(p *Package, path string) (string, error) {
	return c.Create(p, path, false)
}

// getBlocks answers which building blocks a package gets.
func (c *PackageCreator) getBlocks(plain bool) []string {
	if plain {
		return basicBlocks
	}
	return blocks
}

// WriteSupportFiles writes go.mod, README.md, LICENSE and .gitignore.
//
// See the type comment for what is deliberately not written beside them.
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

// writeGoModFile writes the go.mod.
//
// There is one stub rather than a plain and a full one: a go.mod does not change
// shape when the package ships views, and the require lines that would differ
// are the ones `go mod tidy` writes.
func (c *PackageCreator) writeGoModFile(p *Package, directory string) error {
	return c.writeStub(p, "go.mod.stub", filepath.Join(directory, "go.mod"))
}

// writeReadmeFile writes the README.
//
// A Go module without one is a page on pkg.go.dev with an install command
// missing from it.
func (c *PackageCreator) writeReadmeFile(p *Package, directory string) error {
	return c.writeStub(p, "README.md.stub", filepath.Join(directory, "README.md"))
}

// writeLicenseFile writes MIT, which is what everything in this ecosystem uses.
// An author who wants another license replaces one file.
func (c *PackageCreator) writeLicenseFile(p *Package, directory string) error {
	return c.writeStub(p, "LICENSE.stub", filepath.Join(directory, "LICENSE"))
}

// WriteIgnoreFile writes the .gitignore.
//
// The stub is named gitignore.stub because a file called .gitignore inside the
// generator would apply to the generator.
func (c *PackageCreator) WriteIgnoreFile(p *Package, directory string, plain bool) error {
	return c.writeStub(p, "gitignore.stub", filepath.Join(directory, ".gitignore"))
}

// WriteSupportDirectories writes the migrations and views directories.
//
// It writes those two and no others, and each absence is a decision:
//
//   - config is a typed struct, validated at boot, not a file the package
//     publishes into the application.
//   - controllers is not a directory of its own. A module is the vertical
//     slice: its handlers live beside its repository, in the package root.
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

// writeSupportDirectory writes one support directory and a .gitkeep in it.
//
// An empty directory is not a thing git stores, so without the .gitkeep the
// author pushes a package that is missing the directories the generator just
// made.
func (c *PackageCreator) writeSupportDirectory(p *Package, support, directory string) error {
	path := filepath.Join(directory, support)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, ".gitkeep"), nil, 0o644)
}

// WritePublicDirectory writes the assets directory.
//
// There is no public/ to symlink or copy out of: an asset enters the binary
// through go:embed and is served by the framework at a hashed URL, so the
// directory a package ships assets in is assets/, and nothing outside the module
// ever reads it from disk.
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

// WriteTestDirectory writes module_test.go at the module root, beside the code
// it tests.
//
// The test it writes runs: it pins the module name and registers the routes. A
// generated test that asserts nothing teaches the author that generated tests
// assert nothing.
func (c *PackageCreator) WriteTestDirectory(p *Package, directory string) error {
	return c.writeStub(p, "module_test.go.stub", filepath.Join(directory, "module_test.go"))
}

// WriteServiceProvider writes module.go and doc.go at the module root.
//
// There is no directory-to-namespace rule to satisfy, and foundation.Module is
// what a package implements to be composable.
func (c *PackageCreator) WriteServiceProvider(p *Package, directory string, plain bool) error {
	if err := c.writeStub(p, "module.go.stub", filepath.Join(directory, "module.go")); err != nil {
		return err
	}
	return c.writeStub(p, "doc.go.stub", filepath.Join(directory, "doc.go"))
}

// createDirectory makes the package directory, refusing one that already exists.
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

// formatPackageStub replaces every {{placeholder}} in a stub with its value.
//
// The pairs are written out rather than walked by reflection, which is the
// mechanism this framework rejects. The last four have no field behind them --
// they come from [Package.ModulePath] and from the creator itself.
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
