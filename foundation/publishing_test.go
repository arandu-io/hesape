package foundation_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/routing"
)

// publisher is a module that publishes whatever it was built with.
type publisher struct {
	name         string
	publications []foundation.Publication
}

func (p publisher) Name() string                        { return p.name }
func (p publisher) Routes(r *routing.Router)            {}
func (p publisher) Publishes() []foundation.Publication { return p.publications }

// quiet is a module that publishes nothing, and is a module all the same.
type quiet struct{}

func (quiet) Name() string             { return "quiet" }
func (quiet) Routes(r *routing.Router) {}

func files() fstest.MapFS {
	return fstest.MapFS{"views/home.kyse.go": &fstest.MapFile{Data: []byte("package views\n")}}
}

func TestTheTagsAreSix(t *testing.T) {
	tags := foundation.PublishTags()
	if len(tags) != 6 {
		t.Fatalf("got %d tags, want 6: %v", len(tags), tags)
	}
	for _, tag := range tags {
		if !tag.Valid() {
			t.Errorf("%q is listed and not valid", tag)
		}
	}
	if foundation.PublishTag("panel").Valid() {
		t.Error("a tag nobody declared is valid")
	}
}

func TestAModuleThatPublishesNothingIsStillAModule(t *testing.T) {
	publications, err := foundation.Publications(quiet{})
	if err != nil {
		t.Fatalf("Publications: %v", err)
	}
	if publications != nil {
		t.Errorf("got %v, want nothing", publications)
	}
}

func TestPublicationsAreReturnedAsDeclared(t *testing.T) {
	module := publisher{name: "billing", publications: []foundation.Publication{
		{Tag: foundation.PublishView, Files: files(), To: "resources/views"},
		{Tag: foundation.PublishMigration, Files: files(), To: "database/migrations"},
	}}

	publications, err := foundation.Publications(module)
	if err != nil {
		t.Fatalf("Publications: %v", err)
	}
	if len(publications) != 2 {
		t.Fatalf("got %d publications, want 2", len(publications))
	}
}

// TestATagFromOutsideTheSetIsRefused. The module compiles either way, so the
// refusal has to happen where the publication is read.
func TestATagFromOutsideTheSetIsRefused(t *testing.T) {
	module := publisher{name: "billing", publications: []foundation.Publication{
		{Tag: "panel", Files: files()},
	}}

	_, err := foundation.Publications(module)
	if err == nil {
		t.Fatal("a tag nobody declared was accepted")
	}
	if !strings.Contains(err.Error(), "billing") || !strings.Contains(err.Error(), "panel") {
		t.Errorf("error = %v, want the module and the tag named", err)
	}
}

func TestAPublicationWithNoFilesIsRefused(t *testing.T) {
	module := publisher{name: "billing", publications: []foundation.Publication{
		{Tag: foundation.PublishAsset},
	}}

	if _, err := foundation.Publications(module); err == nil {
		t.Fatal("a publication with no tree was accepted")
	}
}

// The declaration is an interface a module satisfies, and a module satisfies it
// without importing the engine that writes the files.
var (
	_ foundation.Module      = publisher{}
	_ foundation.Publishable = publisher{}
	_ foundation.Module      = quiet{}
)

// vendored builds a publication whose archive carries a directory named vendor.
func vendored() fstest.MapFS {
	return fstest.MapFS{
		"resources/views/vendor/skeleton/index.kyse.go": &fstest.MapFile{Data: []byte("package views\n")},
	}
}

// TestAPublicationCannotCarryAVendorDirectory fixes the refusal that stands
// between a module and two rules of the go command.
//
// Both were reproduced before this was written. A file at
// resources/views/vendor/<name>/index.kyse.go is reported by zip.CheckDir as
// "file is in vendor directory" and never reaches the module zip, so the embed
// that names its directory matches nothing for anyone who downloads the module.
// And a package under storage/framework/views/vendor/<name> is refused at
// import with "use of vendored package not allowed", which is where a published
// view ends up once it is compiled.
func TestAPublicationCannotCarryAVendorDirectory(t *testing.T) {
	module := publisher{
		name:         "skeleton",
		publications: []foundation.Publication{{Tag: foundation.PublishView, Files: vendored()}},
	}

	_, err := foundation.Publications(module)
	if err == nil {
		t.Fatal("a view under a vendor directory was accepted, and it is dropped from the module zip")
	}
	for _, want := range []string{"skeleton", "vendor", "module zip"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %s", want, err)
		}
	}
}

// TestAViewDestinationCannotBeVendored covers the other half: a module whose
// archive is clean because it publishes with From and To, and whose destination
// is the vendor tree all the same. The files arrive, the view compiler turns
// them into Go, and the import the module asks for does not build.
func TestAViewDestinationCannotBeVendored(t *testing.T) {
	module := publisher{
		name: "permission",
		publications: []foundation.Publication{{
			Tag:   foundation.PublishView,
			Files: files(),
			From:  "views",
			To:    "resources/views/vendor/permission",
		}},
	}

	_, err := foundation.Publications(module)
	if err == nil {
		t.Fatal("a view destination under vendor was accepted, and nothing can import what it compiles to")
	}
	if !strings.Contains(err.Error(), "use of vendored package not allowed") {
		t.Errorf("the refusal does not quote the error the build gives: %s", err)
	}
}

// TestANonGoPublicationKeepsItsDestination keeps the refusal to what it is for.
//
// A catalogue of sentences is read at runtime and never named by an import
// path, and the application's own override tree for one is spelled with a
// vendor directory. Refusing that destination would refuse the layout the
// translation loader already reads.
func TestANonGoPublicationKeepsItsDestination(t *testing.T) {
	module := publisher{
		name: "skeleton",
		publications: []foundation.Publication{{
			Tag:   foundation.PublishTranslation,
			Files: files(),
			From:  "views",
			To:    "resources/lang/vendor/skeleton",
		}},
	}

	if _, err := foundation.Publications(module); err != nil {
		t.Fatalf("a translation destination was refused: %v", err)
	}
}

// TestAPublicationWithoutTheReservedNameIsAccepted is the other side of the
// gate: the name it refuses is one element, not a substring.
func TestAPublicationWithoutTheReservedNameIsAccepted(t *testing.T) {
	module := publisher{
		name: "skeleton",
		publications: []foundation.Publication{{
			Tag: foundation.PublishView,
			Files: fstest.MapFS{
				"resources/views/modules/skeleton/index.kyse.go": &fstest.MapFile{Data: []byte("package views\n")},
				"resources/views/vendors/skeleton/list.kyse.go":  &fstest.MapFile{Data: []byte("package views\n")},
			},
		}},
	}

	if _, err := foundation.Publications(module); err != nil {
		t.Fatalf("a publication with no reserved element was refused: %v", err)
	}
}
