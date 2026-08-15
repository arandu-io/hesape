package casts

import (
	"fmt"

	"github.com/arandu-io/hesape/str"
	"github.com/arandu-io/hesape/support"
)

// AsStringable answers Illuminate\Database\Eloquent\Casts\AsStringable: a text
// column read as a str.Stringable.
type AsStringable struct{}

// CastUsing answers AsStringable::castUsing.
func (AsStringable) CastUsing(arguments []string) (CastsAttributes, error) {
	return stringableCast{}, nil
}

type stringableCast struct{}

// Get answers the anonymous caster's get.
func (stringableCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	text, ok := asText(value)
	if !ok {
		return nil, nil
	}
	return str.Of(text), nil
}

// Set answers the anonymous caster's set: the string back, or null.
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
//
// Answers Illuminate\Database\Eloquent\Casts\AsHtmlString. The PHP spells the
// class AsHtmlString, so the type does too.
type AsHtmlString struct{}

// CastUsing answers AsHtmlString::castUsing.
func (AsHtmlString) CastUsing(arguments []string) (CastsAttributes, error) {
	return htmlStringCast{}, nil
}

type htmlStringCast struct{}

// Get answers the anonymous caster's get.
func (htmlStringCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	text, ok := asText(value)
	if !ok {
		return nil, nil
	}
	return support.NewHtmlString(text), nil
}

// Set answers the anonymous caster's set.
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
//
// Answers Illuminate\Database\Eloquent\Casts\AsUri. The PHP spells the class
// AsUri, so the type does too, rather than AsURI.
type AsUri struct{}

// CastUsing answers AsUri::castUsing.
func (AsUri) CastUsing(arguments []string) (CastsAttributes, error) {
	return uriCast{}, nil
}

type uriCast struct{}

// Get answers the anonymous caster's get. The PHP's Uri constructor throws on
// a malformed string, and support.NewUri returns the error instead.
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

// Set answers the anonymous caster's set: the URI back as its string.
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

// AsFluent answers Illuminate\Database\Eloquent\Casts\AsFluent: a JSON object
// column read as a support.Fluent.
type AsFluent struct{}

// CastUsing answers AsFluent::castUsing.
func (AsFluent) CastUsing(arguments []string) (CastsAttributes, error) {
	return fluentCast{}, nil
}

type fluentCast struct{}

// Get answers the anonymous caster's get.
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

// Set answers the anonymous caster's set.
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

// asText answers the PHP's isset($value) followed by (string) $value: a stored
// column reaches a cast as a string from one driver and as []byte from another,
// and null from either.
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
