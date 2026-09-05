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
