package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func newTestPackage(t *testing.T) *Package {
	t.Helper()
	pkg, err := NewPackage("Acme", "InvoiceManager", "Ada Lovelace", "ada@example.com", "")
	if err != nil {
		t.Fatalf("NewPackage: %v", err)
	}
	return pkg
}

func TestNewPackageDerivesTheSlugsAndTheIdentifier(t *testing.T) {
	pkg := newTestPackage(t)

	for _, c := range []struct{ got, want, field string }{
		{pkg.LowerVendor, "acme", "LowerVendor"},
		{pkg.LowerName, "invoice-manager", "LowerName"},
		{pkg.PackageName, "invoicemanager", "PackageName"},
		{pkg.GetFullName(), "acme/invoice-manager", "GetFullName"},
		{pkg.ModulePath(), "github.com/acme/invoice-manager", "ModulePath"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestNewPackageRefusesAnEmptySegment(t *testing.T) {
	if _, err := NewPackage("", "InvoiceManager", "Ada", "ada@example.com", ""); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("NewPackage with no vendor = %v, want ErrInvalidPackage", err)
	}
	if _, err := NewPackage("Acme", "  ", "Ada", "ada@example.com", ""); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("NewPackage with no name = %v, want ErrInvalidPackage", err)
	}
}

// A digit cannot open a Go identifier, and a slug is allowed to start with one.
func TestPackageNameIsAlwaysALegalIdentifier(t *testing.T) {
	pkg, err := NewPackage("Acme", "2FA", "Ada", "ada@example.com", "")
	if err != nil {
		t.Fatalf("NewPackage: %v", err)
	}
	if first := pkg.PackageName[0]; first >= '0' && first <= '9' {
		t.Fatalf("PackageName = %q, which does not open a package clause", pkg.PackageName)
	}
}

func TestCreateWritesTheSevenFiles(t *testing.T) {
	directory := createInto(t, t.TempDir(), true)

	for _, name := range []string{"go.mod", "doc.go", "module.go", "module_test.go", "README.md", "LICENSE", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

// Plain is PHP's default, and it must not write the resource directories.
func TestCreatePlainWritesNoResourceDirectories(t *testing.T) {
	directory := createInto(t, t.TempDir(), true)

	for _, name := range []string{"migrations", "resources", "assets"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err == nil {
			t.Errorf("plain package has %s/, which only --resources writes", name)
		}
	}
}

func TestCreateWithResourcesWritesThem(t *testing.T) {
	pkg := newTestPackage(t)
	creator := &PackageCreator{Year: 2026}

	directory, err := creator.CreateWithResources(pkg, t.TempDir())
	if err != nil {
		t.Fatalf("CreateWithResources: %v", err)
	}

	for _, name := range []string{
		filepath.Join("migrations", ".gitkeep"),
		filepath.Join("resources", "views", ".gitkeep"),
		filepath.Join("assets", ".gitkeep"),
	} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
}

func TestCreateRefusesToOverwrite(t *testing.T) {
	path := t.TempDir()
	createInto(t, path, true)

	if _, err := NewPackageCreator().Create(newTestPackage(t), path, true); !errors.Is(err, ErrPackageExists) {
		t.Fatalf("second Create = %v, want ErrPackageExists", err)
	}
}

// Every placeholder has to be substituted. One left behind is a file that looks
// right in a diff and does not compile.
func TestNoPlaceholderSurvives(t *testing.T) {
	directory := createInto(t, t.TempDir(), false)

	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), "{{") {
			t.Errorf("%s still has a placeholder in it", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// The generated Go has to name the module path, the package and the slug in the
// places a compiler and a router will read them.
func TestGeneratedFilesCarryTheIdentity(t *testing.T) {
	directory := createInto(t, t.TempDir(), true)

	for _, c := range []struct{ file, want string }{
		{"go.mod", "module github.com/acme/invoice-manager"},
		{"go.mod", "go " + DefaultGoVersion},
		{"module.go", "package invoicemanager"},
		{"module.go", `return "invoice-manager"`},
		{"module.go", "var _ foundation.Module = (*Module)(nil)"},
		{"doc.go", "package invoicemanager"},
		{"module_test.go", "package invoicemanager"},
		{"LICENSE", "Copyright (c) 2026 Ada Lovelace"},
		{"README.md", "go get github.com/acme/invoice-manager"},
	} {
		contents, err := os.ReadFile(filepath.Join(directory, c.file))
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		if !strings.Contains(string(contents), c.want) {
			t.Errorf("%s does not contain %q", c.file, c.want)
		}
	}
}

// The custom blocks of RULE 15 are what survives regeneration. A generator that
// does not emit them gives the author nowhere to write that is safe.
func TestModuleCarriesTheCustomBlocks(t *testing.T) {
	directory := createInto(t, t.TempDir(), true)

	contents, err := os.ReadFile(filepath.Join(directory, "module.go"))
	if err != nil {
		t.Fatalf("read module.go: %v", err)
	}
	if got := strings.Count(string(contents), "// arandu:begin custom"); got != 2 {
		t.Errorf("module.go has %d custom blocks, want 2", got)
	}
}

func TestStartFindsTheModules(t *testing.T) {
	path := t.TempDir()
	directory := createInto(t, path, true)

	found, err := Start(path)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !slices.Contains(found, directory) {
		t.Fatalf("Start = %v, want it to contain %q", found, directory)
	}
}

// A project with no workbench is not a failure, it is the common case.
func TestStartOnAMissingPathIsNotAnError(t *testing.T) {
	found, err := Start(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if found != nil {
		t.Fatalf("Start = %v, want nil", found)
	}
}

func TestGetPackageSegmentsStudlyCasesBoth(t *testing.T) {
	command := NewWorkbenchMakeCommand(nil, Config{Email: "ada@example.com"}, nil)

	vendor, name, err := command.getPackageSegments([]string{"acme/invoice-manager"})
	if err != nil {
		t.Fatalf("getPackageSegments: %v", err)
	}
	if vendor != "Acme" || name != "InvoiceManager" {
		t.Fatalf("getPackageSegments = %q, %q, want %q, %q", vendor, name, "Acme", "InvoiceManager")
	}
}

func TestBuildPackageRefusesWithoutAnEmail(t *testing.T) {
	command := NewWorkbenchMakeCommand(nil, Config{}, nil)

	if _, err := command.buildPackage([]string{"acme/billing"}); !errors.Is(err, ErrMissingEmail) {
		t.Fatalf("buildPackage = %v, want ErrMissingEmail", err)
	}
}

func TestBuildPackageRefusesAnArgumentWithoutASlash(t *testing.T) {
	command := NewWorkbenchMakeCommand(nil, Config{Email: "ada@example.com"}, nil)

	if _, err := command.buildPackage([]string{"billing"}); !errors.Is(err, ErrMissingPackage) {
		t.Fatalf("buildPackage = %v, want ErrMissingPackage", err)
	}
	if _, err := command.buildPackage(nil); !errors.Is(err, ErrMissingPackage) {
		t.Fatalf("buildPackage with no argument = %v, want ErrMissingPackage", err)
	}
}

func TestCommandIsNamedForTheVerb(t *testing.T) {
	if got := NewWorkbenchMakeCommand(nil, Config{}, nil).Command().Name; got != "make:package" {
		t.Fatalf("Command().Name = %q, want %q", got, "make:package")
	}
}

func createInto(t *testing.T, path string, plain bool) string {
	t.Helper()
	creator := &PackageCreator{Year: 2026}

	directory, err := creator.Create(newTestPackage(t), path, plain)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return directory
}
