package translation_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/arandu-io/hesape/translation"
)

func TestNewFSReadsLocaleDirectoriesAndGroupFiles(t *testing.T) {
	l, err := translation.NewFS(fstest.MapFS{
		"en/auth.json":    {Data: []byte(`{"failed": "These credentials do not match our records."}`)},
		"pt-BR/auth.json": {Data: []byte(`{"failed": "Estas credenciais não correspondem aos nossos registros."}`)},
	})
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	if got, want := l.Load("en", "auth")["failed"], "These credentials do not match our records."; got != want {
		t.Errorf("Load = %q, want %q", got, want)
	}
	if got := l.Load("pt-BR", "auth")["failed"]; !strings.HasPrefix(got, "Estas credenciais") {
		t.Errorf("Load in pt-BR = %q", got)
	}
	if got := l.Load("fr", "auth"); got != nil {
		t.Errorf("Load of an absent locale = %v, want nil", got)
	}
	if got := l.Load("en", "absent"); got != nil {
		t.Errorf("Load of an absent group = %v, want nil", got)
	}
}

// A nested object is how the four shapes of one validation message stay
// together in the file. The lookup only ever sees the dotted item.
func TestNewFSFlattensNestedObjects(t *testing.T) {
	l, err := translation.NewFS(fstest.MapFS{
		"en/validation.json": {Data: []byte(`{
			"required": "The :attribute field is required.",
			"min": {"string": "at least :min characters", "numeric": "at least :min"}
		}`)},
	})
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	lines := l.Load("en", "validation")
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

func TestNewFSIgnoresWhatIsNotACatalogue(t *testing.T) {
	l, err := translation.NewFS(fstest.MapFS{
		"README.md":    {Data: []byte("How to translate this application.")},
		"en/notes.txt": {Data: []byte("not a group")},
		"en/auth.json": {Data: []byte(`{"failed": "no"}`)},
	})
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	if got, want := l.Load("en", "auth")["failed"], "no"; got != want {
		t.Errorf("Load = %q, want %q", got, want)
	}
	if got := l.Load("en", "notes"); got != nil {
		t.Errorf("a .txt was read as a group: %v", got)
	}
}

// A language file that does not parse must stop the process that is starting,
// not go missing on the one request that needed the sentence.
func TestNewFSReportsAFileThatDoesNotParse(t *testing.T) {
	_, err := translation.NewFS(fstest.MapFS{
		"en/auth.json": {Data: []byte(`{"failed": `)},
	})
	if err == nil {
		t.Fatal("NewFS accepted a malformed file")
	}
	if !strings.Contains(err.Error(), "en/auth.json") {
		t.Errorf("the error does not name the file: %v", err)
	}
}

func TestNewFSReportsALineThatIsNotAString(t *testing.T) {
	_, err := translation.NewFS(fstest.MapFS{
		"en/pagination.json": {Data: []byte(`{"per_page": 15}`)},
	})
	if err == nil {
		t.Fatal("NewFS accepted a number as a line")
	}
	if !strings.Contains(err.Error(), "per_page") {
		t.Errorf("the error does not name the item: %v", err)
	}
}

func TestNewMapCopiesWhatItIsGiven(t *testing.T) {
	lines := translation.Lines{"failed": "no"}
	l := translation.NewMap(map[string]map[string]translation.Lines{"en": {"auth": lines}})

	lines["failed"] = "written after the fact"

	if got, want := l.Load("en", "auth")["failed"], "no"; got != want {
		t.Errorf("Load = %q, want %q -- the loader shares the caller's map", got, want)
	}
}
