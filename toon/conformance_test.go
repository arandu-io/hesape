package toon_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arandu-io/hesape/toon"
)

type specFile struct {
	Tests []specCase `json:"tests"`
}

type specCase struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Expected string          `json:"expected"`
	Options  json.RawMessage `json:"options"`
}

// TestMarshalPassesTheOfficialDefaultProfileCorpus runs every encode fixture
// from the MIT-licensed TOON specification tag v4.1.1 that does not request an
// option. Marshal deliberately has no options: it emits the canonical comma,
// two-space profile behind one stable interface.
func TestMarshalPassesTheOfficialDefaultProfileCorpus(t *testing.T) {
	entries, err := os.ReadDir("testdata/spec-v4.1.1/encode")
	if err != nil {
		t.Fatalf("read specification fixtures: %v", err)
	}

	run := 0
	skipped := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		body, err := os.ReadFile(filepath.Join("testdata/spec-v4.1.1/encode", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var file specFile
		if err := json.Unmarshal(body, &file); err != nil {
			t.Fatalf("decode %s: %v", entry.Name(), err)
		}

		for _, test := range file.Tests {
			if len(test.Options) != 0 && string(test.Options) != "null" {
				skipped++
				continue
			}
			run++
			t.Run(entry.Name()+"/"+test.Name, func(t *testing.T) {
				got, err := toon.Marshal(test.Input)
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				if string(got) != test.Expected {
					t.Errorf("Marshal = %q, want %q", got, test.Expected)
				}
			})
		}
	}

	if run != 154 {
		t.Fatalf("ran %d default-profile fixtures, want the 154 cases pinned at v4.1.1", run)
	}
	if skipped != 25 {
		t.Fatalf("skipped %d option fixtures, want the 25 cases outside Marshal's fixed profile", skipped)
	}
}
