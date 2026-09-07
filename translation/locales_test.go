package translation_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every shipped locale answers the same questions.
//
// A catalogue that is missing a key falls back, which is the right behaviour and
// the wrong outcome to discover in production: a Brazilian product showed the
// permission screen in Portuguese and the validation message under the same
// form in English, because the modules shipped pt-BR and this package shipped
// only en. Adding a locale is adding a file per group with the same keys, and
// this is what says so before anybody notices on a screen.
//
// It reads the directory rather than a list, so a locale added without a test
// being told about it is still checked.

// placeholder is what a message substitutes at render time: ":attribute",
// ":min". They are not words and are never translated -- a message that drops
// one renders a sentence with a hole in it, and one that invents another
// renders the placeholder itself.
var placeholder = regexp.MustCompile(`:[a-z_]+`)

func TestEveryLocaleAnswersTheSameKeys(t *testing.T) {
	t.Parallel()

	locales := shippedLocales(t)
	if len(locales) < 2 {
		t.Fatalf("locales = %v: this test compares them, and there is nothing to compare", locales)
	}

	// English is the reference because it is the one every message is written
	// in first.
	reference := "en"
	for _, group := range groupsOf(t, reference) {
		want := flatten(t, filepath.Join("lang", reference, group))

		for _, locale := range locales {
			if locale == reference {
				continue
			}
			path := filepath.Join("lang", locale, group)
			if _, err := os.Stat(path); err != nil {
				t.Errorf("%s has no %s, so every message in it falls back to %s", locale, group, reference)
				continue
			}
			got := flatten(t, path)

			for key, text := range want {
				translated, ok := got[key]
				if !ok {
					t.Errorf("%s/%s is missing %q", locale, group, key)
					continue
				}
				if strings.TrimSpace(translated) == "" {
					t.Errorf("%s/%s has %q empty, which renders as nothing", locale, group, key)
				}
				if a, b := placeholders(text), placeholders(translated); !reflect.DeepEqual(a, b) {
					t.Errorf("%s/%s %q substitutes %v and the English one substitutes %v",
						locale, group, key, b, a)
				}
			}
			for key := range got {
				if _, ok := want[key]; !ok {
					t.Errorf("%s/%s has %q, which %s does not: a message nothing asks for", locale, group, key, reference)
				}
			}
		}
	}
}

// TestEveryLocaleShipsEveryGroup holds the other direction: a locale with three
// of the four files is a form that answers in two languages.
func TestEveryLocaleShipsEveryGroup(t *testing.T) {
	t.Parallel()

	want := groupsOf(t, "en")
	for _, locale := range shippedLocales(t) {
		got := groupsOf(t, locale)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s ships %v and en ships %v", locale, got, want)
		}
	}
}

// shippedLocales is every directory under lang.
func shippedLocales(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir("lang")
	if err != nil {
		t.Fatalf("reading the catalogue: %v", err)
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// groupsOf is every json file a locale ships, sorted.
func groupsOf(t *testing.T, locale string) []string {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join("lang", locale))
	if err != nil {
		t.Fatalf("reading %s: %v", locale, err)
	}
	var out []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			out = append(out, entry.Name())
		}
	}
	sort.Strings(out)
	return out
}

// flatten reads one catalogue file into dotted keys, so a nested group is
// compared entry by entry rather than as one value.
func flatten(t *testing.T, path string) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	out := map[string]string{}
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for key, value := range node {
			switch v := value.(type) {
			case string:
				out[prefix+key] = v
			case map[string]any:
				walk(prefix+key+".", v)
			default:
				t.Errorf("%s: %q is neither a message nor a group of them", path, prefix+key)
			}
		}
	}
	walk("", tree)
	return out
}

// placeholders is what a message substitutes, sorted and deduplicated.
func placeholders(text string) []string {
	found := map[string]bool{}
	for _, match := range placeholder.FindAllString(text, -1) {
		found[match] = true
	}
	out := make([]string, 0, len(found))
	for match := range found {
		out = append(out, match)
	}
	sort.Strings(out)
	return out
}
