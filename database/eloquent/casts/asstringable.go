package casts

import (
	"fmt"

	"github.com/arandu-io/hesape/str"
	"github.com/arandu-io/hesape/support"
)

// AsStringable casts a text column into a str.Stringable.
type AsStringable struct{}

// CastUsing returns the caster that reads and writes the column as a
// str.Stringable.
func (AsStringable) CastUsing(arguments []string) (CastsAttributes, error) {
	return stringableCast{}, nil
}

type stringableCast struct{}

// Get returns value's text form wrapped as a str.Stringable, or nil if value
// has no text form.
func (stringableCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	text, ok := asText(value)
	if !ok {
		return nil, nil
	}
	return str.Of(text), nil
}

// Set returns the column holding value's text form, or nil if value has no
// text form.
func (stringableCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	text, ok := asText(value)
	if !ok {
		return map[string]any{key: nil}, nil
	}
	return map[string]any{key: text}, nil
}

// AsHtmlString casts a text column into a support.HtmlString: markup the view
// layer prints as it stands, without escaping it again.
//
// Use it for a column the application itself produced -- rendered Markdown, a
// stored template fragment. Do not use it for anything a user typed: the whole
// meaning of the cast is that escaping is skipped, so a column carrying user
// input becomes stored cross-site scripting the moment it is rendered.
//
// Writing stores the string unchanged, and a null column reads as nil.
type AsHtmlString struct{}

// CastUsing returns the caster that reads and writes the column as a
// support.HtmlString.
func (AsHtmlString) CastUsing(arguments []string) (CastsAttributes, error) {
	return htmlStringCast{}, nil
}

type htmlStringCast struct{}

// Get returns value's text form wrapped as a support.HtmlString, or nil if
// value has no text form.
func (htmlStringCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	text, ok := asText(value)
	if !ok {
		return nil, nil
	}
	return support.NewHtmlString(text), nil
}

// Set returns the column holding value's text form, or nil if value has no
// text form.
func (htmlStringCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	text, ok := asText(value)
	if !ok {
		return map[string]any{key: nil}, nil
	}
	return map[string]any{key: text}, nil
}

// AsUri casts a text column into a *support.Uri, so an attribute holding a URL
// arrives with its scheme, host, path and query already parsed instead of as a
// string every caller parses again.
//
// Writing stores the URI's string form, and a null column reads as nil. A
// column the parser rejects returns an error rather than a half-built value:
// the malformed row is reported where it is read, not where it is later used.
type AsUri struct{}

// CastUsing returns the caster that reads and writes the column as a
// *support.Uri.
func (AsUri) CastUsing(arguments []string) (CastsAttributes, error) {
	return uriCast{}, nil
}

type uriCast struct{}

// Get parses value's text form as a *support.Uri, and returns an error if
// the text is malformed.
func (uriCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	text, ok := asText(value)
	if !ok {
		return nil, nil
	}
	uri, err := support.NewUri(text)
	if err != nil {
		return nil, fmt.Errorf("casts: %s: %w", key, err)
	}
	return uri, nil
}

// Set returns the column holding a *support.Uri's string form, or value's
// text form for anything else, or nil if it has none.
func (uriCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	if uri, ok := value.(*support.Uri); ok && uri != nil {
		return map[string]any{key: uri.String()}, nil
	}
	text, ok := asText(value)
	if !ok {
		return map[string]any{key: nil}, nil
	}
	return map[string]any{key: text}, nil
}

// AsFluent casts a JSON object column into a support.Fluent.
type AsFluent struct{}

// CastUsing returns the caster that reads and writes the column as a
// support.Fluent.
func (AsFluent) CastUsing(arguments []string) (CastsAttributes, error) {
	return fluentCast{}, nil
}

type fluentCast struct{}

// Get decodes the column and returns it as a support.Fluent, or nil if value
// is nil or does not decode to a JSON object.
func (fluentCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := decodeAttribute(value)
	if err != nil {
		return nil, fmt.Errorf("casts: decoding %s: %w", key, err)
	}
	bag, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	return support.NewFluent(bag), nil
}

// Set encodes value as JSON and returns the column holding it, or nil for a
// nil value.
func (fluentCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	return map[string]any{key: encoded}, nil
}

// asText normalizes a stored column to text and reports whether it had one.
// nil has none; a string or []byte is returned as is; a fmt.Stringer is
// rendered through String; anything else is formatted with fmt.Sprint.
func asText(value any) (string, bool) {
	switch stored := value.(type) {
	case nil:
		return "", false
	case string:
		return stored, true
	case []byte:
		return string(stored), true
	case fmt.Stringer:
		return stored.String(), true
	default:
		return fmt.Sprint(stored), true
	}
}
