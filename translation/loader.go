package translation

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"strings"
)

// Lines is one group of a catalogue, in one locale: the item path of every line
// mapped to the line itself.
//
// The item path is dotted. A file that nests "min" under "string" is read as
// the single item "min.string", so the file keeps the shape a human edits and
// the lookup keeps one flat form.
type Lines map[string]string

// Loader answers with the lines of a group in a locale.
//
// It returns nil for a group it does not carry, which is not an error: a
// catalogue is not required to translate everything, and the [Translator] falls
// through to the next locale and then to the English lines that ship here.
//
// A Loader is read by every request at once and must not be written to after it
// is built. Both implementations in this package read their whole catalogue up
// front for that reason.
type Loader interface {
	Load(locale, group string) Lines
}

// catalogue is the one representation both loaders reduce to: locale, then
// group, then the flattened lines.
type catalogue map[string]map[string]Lines

func (c catalogue) Load(locale, group string) Lines { return c[locale][group] }

// NewFS reads a catalogue laid out as <locale>/<group>.json.
//
// Every locale is a directory at the root of fsys and every group is a .json
// file inside it, holding an object whose values are lines or further objects.
// Anything else in the tree -- a file that is not .json, a directory that is
// not a locale -- is ignored, so a lang directory can hold a README.
//
// It reads and parses everything before returning. A malformed file, or a value
// that is not a line, is an error here rather than a missing sentence later.
func NewFS(fsys fs.FS) (Loader, error) {
	locales, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("translation: reading the catalogue root: %w", err)
	}

	c := make(catalogue)
	for _, locale := range locales {
		if !locale.IsDir() {
			continue
		}
		groups, err := fs.ReadDir(fsys, locale.Name())
		if err != nil {
			return nil, fmt.Errorf("translation: reading locale %q: %w", locale.Name(), err)
		}
		for _, group := range groups {
			name := group.Name()
			if group.IsDir() || path.Ext(name) != ".json" {
				continue
			}
			file := path.Join(locale.Name(), name)
			raw, err := fs.ReadFile(fsys, file)
			if err != nil {
				return nil, fmt.Errorf("translation: reading %s: %w", file, err)
			}
			var nested map[string]any
			if err := json.Unmarshal(raw, &nested); err != nil {
				return nil, fmt.Errorf("translation: parsing %s: %w", file, err)
			}
			lines := make(Lines)
			if err := flatten(lines, "", nested); err != nil {
				return nil, fmt.Errorf("translation: %s: %w", file, err)
			}
			if c[locale.Name()] == nil {
				c[locale.Name()] = make(map[string]Lines)
			}
			c[locale.Name()][strings.TrimSuffix(name, ".json")] = lines
		}
	}
	return c, nil
}

// NewMap returns a Loader over a catalogue written in Go, keyed by locale and
// then by group. It copies what it is given, so the caller keeps no way to
// write to a loader that requests are reading.
func NewMap(lines map[string]map[string]Lines) Loader {
	c := make(catalogue, len(lines))
	for locale, groups := range lines {
		c[locale] = make(map[string]Lines, len(groups))
		for group, l := range groups {
			c[locale][group] = maps.Clone(l)
		}
	}
	return c
}

// flatten writes every line of a parsed group under its dotted item path.
func flatten(out Lines, prefix string, in map[string]any) error {
	for key, value := range in {
		item := key
		if prefix != "" {
			item = prefix + "." + key
		}
		switch v := value.(type) {
		case string:
			out[item] = v
		case map[string]any:
			if err := flatten(out, item, v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("item %q is %T, and a translation line is a string", item, value)
		}
	}
	return nil
}
