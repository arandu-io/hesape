package http

import "testing"

// TestJSONFieldPathDropsListPositions: encoding/json reports "events.0.qty" on
// a newer toolchain and "events.qty" on an older one, and the key of an error
// is the same on both.
func TestJSONFieldPathDropsListPositions(t *testing.T) {
	for in, want := range map[string]string{
		"events.0.qty":   "events.qty",
		"events.qty":     "events.qty",
		"a.12.b.3.c":     "a.b.c",
		"source":         "source",
		"":               JSONBodyKey,
		"0":              JSONBodyKey,
		"version2.items": "version2.items",
	} {
		if got := jsonFieldPath(in); got != want {
			t.Errorf("jsonFieldPath(%q) = %q, want %q", in, got, want)
		}
	}
}
