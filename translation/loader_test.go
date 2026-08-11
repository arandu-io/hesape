package translation_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/arandu-io/hesape/translation"
)

func fileLoader(t *testing.T, files fstest.MapFS, paths ...string) *translation.FileLoader {
	t.Helper()
	if len(paths) == 0 {
		paths = []string{"lang"}
	}
	l, err := translation.NewFileLoader(files, paths...)
	if err != nil {
		t.Fatalf("NewFileLoader: %v", err)
	}
	return l
}

func TestFileLoaderReadsLocaleDirectoriesAndGroupFiles(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"lang/en/auth.json":    {Data: []byte(`{"failed": "These credentials do not match our records."}`)},
		"lang/pt-BR/auth.json": {Data: []byte(`{"failed": "Estas credenciais não correspondem aos nossos registros."}`)},
	})

	if got, want := l.Load("en", "auth", "")["failed"], "These credentials do not match our records."; got != want {
		t.Errorf("Load = %q, want %q", got, want)
	}
	if got := l.Load("pt-BR", "auth", "*")["failed"]; !strings.HasPrefix(got, "Estas credenciais") {
		t.Errorf("Load in pt-BR = %q", got)
	}
	if got := l.Load("fr", "auth", ""); got != nil {
		t.Errorf("Load of an absent locale = %v, want nil", got)
	}
	if got := l.Load("en", "absent", ""); got != nil {
		t.Errorf("Load of an absent group = %v, want nil", got)
	}
}

// A nested object is how the four shapes of one validation message stay
// together in the file. The lookup only ever sees the dotted item.
func TestFileLoaderFlattensNestedObjects(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"lang/en/validation.json": {Data: []byte(`{
			"required": "The :attribute field is required.",
			"min": {"string": "at least :min characters", "numeric": "at least :min"}
		}`)},
	})

	lines := l.Load("en", "validation", "")
	for item, want := range map[string]string{
		"required":    "The :attribute field is required.",
		"min.string":  "at least :min characters",
		"min.numeric": "at least :min",
	} {
		if got := lines[item]; got != want {
			t.Errorf("item %q = %q, want %q", item, got, want)
		}
	}
	if _, ok := lines["min"]; ok {
		t.Error("the nested object survived as an item of its own")
	}
}

func TestFileLoaderIgnoresWhatIsNotACatalogue(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"lang/README.md":    {Data: []byte("How to translate this application.")},
		"lang/en/notes.txt": {Data: []byte("not a group")},
		"lang/en/auth.json": {Data: []byte(`{"failed": "no"}`)},
	})

	if got, want := l.Load("en", "auth", "")["failed"], "no"; got != want {
		t.Errorf("Load = %q, want %q", got, want)
	}
	if got := l.Load("en", "notes", ""); got != nil {
		t.Errorf("a .txt was read as a group: %v", got)
	}
}

// A language file that does not parse must stop the process that is starting,
// not go missing on the one request that needed the sentence.
func TestFileLoaderReportsAFileThatDoesNotParse(t *testing.T) {
	_, err := translation.NewFileLoader(fstest.MapFS{
		"lang/en/auth.json": {Data: []byte(`{"failed": `)},
	}, "lang")
	if err == nil {
		t.Fatal("NewFileLoader accepted a malformed file")
	}
	if !strings.Contains(err.Error(), "en/auth.json") {
		t.Errorf("the error does not name the file: %v", err)
	}
}

func TestFileLoaderReportsALineThatIsNotAString(t *testing.T) {
	_, err := translation.NewFileLoader(fstest.MapFS{
		"lang/en/pagination.json": {Data: []byte(`{"per_page": 15}`)},
	}, "lang")
	if err == nil {
		t.Fatal("NewFileLoader accepted a number as a line")
	}
	if !strings.Contains(err.Error(), "per_page") {
		t.Errorf("the error does not name the item: %v", err)
	}
}

// A later path replaces what an earlier one said, which is how a project
// overrides one line of a catalogue it did not write.
func TestFileLoaderReadsEveryPathInOrder(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"vendor/en/auth.json":  {Data: []byte(`{"failed": "no", "password": "wrong"}`)},
		"project/en/auth.json": {Data: []byte(`{"failed": "We do not know that email and password."}`)},
	}, "vendor", "project")

	lines := l.Load("en", "auth", "")
	if got, want := lines["failed"], "We do not know that email and password."; got != want {
		t.Errorf("failed = %q, want %q", got, want)
	}
	if got, want := lines["password"], "wrong"; got != want {
		t.Errorf("password = %q, want %q -- the earlier path was dropped", got, want)
	}
	if got, want := l.Paths(), []string{"vendor", "project"}; len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Paths = %v, want %v", got, want)
	}
}

// AddPath is read the first time a group under it is asked for, since PHP's
// addPath returns void and cannot report a malformed file.
func TestFileLoaderAddPathIsReadOnDemand(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"lang/en/auth.json":  {Data: []byte(`{"failed": "no"}`)},
		"extra/en/auth.json": {Data: []byte(`{"failed": "yes"}`)},
	})

	l.AddPath("extra")

	if got, want := l.Load("en", "auth", "")["failed"], "yes"; got != want {
		t.Errorf("Load = %q, want %q", got, want)
	}
	if got := l.Paths(); len(got) != 2 || got[1] != "extra" {
		t.Errorf("Paths = %v, want the added path last", got)
	}
}

// A namespace is a module shipping its own lines: the hint says where they are,
// and a group under vendor/<namespace> overrides one of them.
func TestFileLoaderReadsANamespaceAndItsOverrides(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"shop/en/orders.json":             {Data: []byte(`{"title": "Your orders", "empty": "No orders yet."}`)},
		"lang/vendor/shop/en/orders.json": {Data: []byte(`{"title": "Your purchases"}`)},
	})

	l.AddNamespace("shop", "shop")

	lines := l.Load("en", "orders", "shop")
	if got, want := lines["title"], "Your purchases"; got != want {
		t.Errorf("title = %q, want %q -- the published override was not read", got, want)
	}
	if got, want := lines["empty"], "No orders yet."; got != want {
		t.Errorf("empty = %q, want %q", got, want)
	}
	if got := l.Load("en", "orders", "unregistered"); got != nil {
		t.Errorf("Load of an unregistered namespace = %v, want nil", got)
	}
	if got, want := l.Namespaces()["shop"], "shop"; got != want {
		t.Errorf("Namespaces()[shop] = %q, want %q", got, want)
	}
}

// The JSON catalogue is one file per locale, keyed by the sentence itself, and
// the JSON paths are read before the ordinary ones.
func TestFileLoaderReadsTheJSONCatalogue(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"lang/en.json":  {Data: []byte(`{"Save changes": "Save changes"}`)},
		"other/en.json": {Data: []byte(`{"Discard": "Discard"}`)},
	})

	l.AddJSONPath("other")

	lines := l.Load("en", "*", "*")
	if got, want := lines["Save changes"], "Save changes"; got != want {
		t.Errorf("Load = %q, want %q", got, want)
	}
	if got, want := lines["Discard"], "Discard"; got != want {
		t.Errorf("Load of the added JSON path = %q, want %q", got, want)
	}
	if got := l.JSONPaths(); len(got) != 1 || got[0] != "other" {
		t.Errorf("JSONPaths = %v, want [other]", got)
	}
}

// A sentence in the JSON catalogue is found by the sentence itself, before any
// group is consulted, which is what __() does in PHP.
func TestTheJSONCatalogueAnswersASentenceKey(t *testing.T) {
	l := fileLoader(t, fstest.MapFS{
		"lang/pt-BR.json": {Data: []byte(`{"Save changes": "Salvar alterações"}`)},
	})
	tr := translation.New(l, "pt-BR", "en")

	if got, want := tr.Get("pt-BR", "Save changes", nil), "Salvar alterações"; got != want {
		t.Errorf("Get = %q, want %q", got, want)
	}
	if !tr.Has("pt-BR", "Save changes") {
		t.Error("Has = false, want true")
	}
}

func TestArrayLoaderCopiesWhatItIsGiven(t *testing.T) {
	lines := translation.Lines{"failed": "no"}
	l := translation.NewArrayLoader().AddMessages("en", "auth", lines, "")

	lines["failed"] = "written after the fact"

	if got, want := l.Load("en", "auth", "")["failed"], "no"; got != want {
		t.Errorf("Load = %q, want %q -- the loader shares the caller's map", got, want)
	}
	if got := l.Load("en", "auth", "shop"); got != nil {
		t.Errorf("Load of another namespace = %v, want nil", got)
	}
	if got := l.Namespaces(); len(got) != 0 {
		t.Errorf("Namespaces = %v, want empty", got)
	}
}
