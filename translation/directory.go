package translation

import (
	"io/fs"
	"path"
	"sort"
	"strings"
)

// NewFromDirectory returns the translator an application answers in, built
// out of its catalogue directory: a [FileLoader] over dir in files, and [New]
// over that loader with the locale to answer in and the locale to fall back
// to.
//
//	tr, err := translation.NewFromDirectory(os.DirFS("."), "lang", "pt-BR", "en")
//
// It is the one call wiring needs, so every application builds its
// translator the same way instead of each repeating the two steps around its
// own lang directory. A directory that does not exist is not an error -- a
// project that ships no catalogue still translates, out of the English
// embedded here -- and a file under it that does not parse is, here, at boot.
func NewFromDirectory(files fs.FS, dir, locale, fallback string) (*Translator, error) {
	loader, err := NewFileLoader(files, dir)
	if err != nil {
		return nil, err
	}
	return New(loader, locale, fallback), nil
}

// Locales lists the locales the catalogue directory carries, sorted: one per
// locale directory and one per sentence-keyed <locale>.json beside them, each
// once, and never the vendor tree of module overrides.
//
// It is the list [Middleware] negotiates against, read from the files rather
// than written a second time in configuration, so a locale added to lang is a
// locale the application can answer in. A directory that does not exist lists
// nothing.
func Locales(files fs.FS, dir string) []string {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case entry.IsDir() && name != "vendor" && !strings.HasPrefix(name, "."):
			seen[name] = true
		case !entry.IsDir() && path.Ext(name) == ".json":
			seen[strings.TrimSuffix(name, ".json")] = true
		}
	}
	locales := make([]string, 0, len(seen))
	for locale := range seen {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	return locales
}
