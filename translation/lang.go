package translation

import (
	"embed"
	"io/fs"
)

// The English catalogue that ships with the framework: the sentences of
// validation, auth, passwords and pagination, which a framework produces before
// an application has written a line of its own.
//
// Illuminate publishes these into the project and Arandu does not: a copy in
// every project is a copy that drifts from the rule that produces it, and a
// rule that gains an argument leaves the published sentence saying the old
// thing. An application overrides one of them by defining the same key in its
// own catalogue, which is one file rather than all four.
//
//go:embed lang
var langFS embed.FS

// bundled is those lines, read once at startup. Every [Translator] consults it
// after the application catalogue and after the fallback locale.
var bundled = mustBundled()

func mustBundled() Loader {
	root, err := fs.Sub(langFS, "lang")
	if err != nil {
		panic("translation: the embedded catalogue has no lang directory: " + err.Error())
	}
	l, err := NewFS(root)
	if err != nil {
		panic("translation: the embedded catalogue does not parse: " + err.Error())
	}
	return l
}
