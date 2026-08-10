package pagination

import (
	"encoding/json"
	"iter"
	"net/url"
)

// This file holds what Illuminate keeps in AbstractPaginator and
// AbstractCursorPaginator: the behaviour the three paginators share.
//
// Go has no inheritance, so each paginator declares its own methods and each of
// them is one line over a helper here. The methods are declared per type rather
// than promoted from an embedded struct because every mutator in Illuminate
// returns $this, and a promoted method would return the embedded value instead
// of the paginator -- which breaks the chaining those methods exist for.

// addQuery answers AbstractPaginator::addQuery(). It records one query string
// parameter, and drops the one holding the position: the paginator writes that
// itself, and letting a caller append it would make two of them fight over the
// same key.
func addQuery(o *Options, reserved, key, value string) {
	if key == reserved {
		return
	}
	if o.Query == nil {
		o.Query = url.Values{}
	}
	o.Query.Set(key, value)
}

// appendQuery answers AbstractPaginator::appends().
//
// PHP declares the parameter as array|string|null, a union Go has no type for,
// so it arrives as any and is opened here. A nil key is ignored, exactly as PHP
// returns $this untouched for null.
func appendQuery(o *Options, reserved string, key any, value []string) {
	first := ""
	if len(value) > 0 {
		first = value[0]
	}

	switch k := key.(type) {
	case nil:
		return
	case string:
		addQuery(o, reserved, k, first)
	case map[string]string:
		for name, v := range k {
			addQuery(o, reserved, name, v)
		}
	case url.Values:
		mergeQuery(o, reserved, k)
	case map[string][]string:
		mergeQuery(o, reserved, url.Values(k))
	}
}

// mergeQuery records every value of a url.Values, keeping repeated parameters
// repeated -- a checkbox filter is several values under one name, and reducing
// it to the last one silently changes the page the link leads to.
func mergeQuery(o *Options, reserved string, values url.Values) {
	for name, list := range values {
		if name == reserved {
			continue
		}
		if o.Query == nil {
			o.Query = url.Values{}
		}
		o.Query[name] = append([]string(nil), list...)
	}
}

// seq2 is the body of every GetIterator: the items, with their offsets, as a
// range-over-func sequence.
func seq2[T any](items []T) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, item := range items {
			if !yield(i, item) {
				return
			}
		}
	}
}

// nullable renders a zero value as JSON null, which is what Illuminate's
// toArray() puts in "from", "to", "next_page_url" and "prev_page_url" when
// there is no such item and no such page.
func nullable[T comparable](value T) any {
	var zero T
	if value == zero {
		return nil
	}
	return value
}

// prettyJSON answers toPrettyJson(). PHP's JSON_PRETTY_PRINT indents with four
// spaces, so this does too: the two outputs diff cleanly against each other.
func prettyJSON(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "    ")
}
