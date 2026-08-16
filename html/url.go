package html

import "net/url"

// UrlGenerator is the minimum surface [HtmlBuilder] and [FormBuilder] need
// to resolve a URL.
//
// It is declared here rather than imported because hesape has no UrlGenerator
// yet: routing.Routes answers Route by name and view.URL answers an embedded
// asset, and nothing joins them behind one contract. Declaring the narrow shape
// in the package that needs it is what a Go package does instead of waiting.
//
// The name is Url, not URL: it does not follow the usual Go initialism
// spelling. The methods do take the Go spelling where an initialism
// appears in them.
//
// # The two contracts an implementation has to keep
//
//   - To returns an absolute URL unchanged. [HtmlBuilder.LinkAsset] resolves
//     the asset first and then hands the result to [HtmlBuilder.Link], which
//     calls To on it; without it every asset link comes back with the host
//     written twice.
//   - Route and Action return an error for a name that is not registered,
//     rather than panicking.
type UrlGenerator interface {
	// To resolves path to an absolute URL. extra is a list of extra path
	// segments.
	To(path string, extra ...string) string

	// Secure is [UrlGenerator.To] over https.
	//
	// Go has no null bool, and a three-state flag -- forcing plain http on a
	// page served over https -- is a mixed-content warning nobody asks for
	// on purpose, so the builders keep a plain `secure bool` parameter and
	// dispatch to this method rather than passing a flag down.
	Secure(path string, extra ...string) string

	// Asset resolves path to the URL of an asset.
	Asset(path string) string

	// SecureAsset is [UrlGenerator.Asset] over https.
	SecureAsset(path string) string

	// Route resolves a named route to a URL. parameters are positional,
	// which is the shape routing.Routes.Route already takes.
	Route(name string, parameters ...string) (string, error)

	// Action resolves a controller action to a URL.
	Action(action string, parameters ...string) (string, error)

	// Current returns the address of the request being served, which is
	// where a form posts when it says nothing else.
	Current() string
}

// Store is the minimum surface [FormBuilder] needs from a session: the old
// input a rejected submission left behind, and the CSRF token.
//
// Go has no function overloading, so what would be one method with an
// optional argument is two: GetOldInput(key) for one field, and
// [Store.OldInputIsEmpty] for whether there is any old input at all.
type Store interface {
	// GetOldInput returns what was typed into one field of the rejected
	// submission.
	//
	// It returns a slice rather than a single value, because a checkbox
	// group posts many values under one name. Nil means the field was not
	// in the old input at all; an empty non-nil slice means it was there
	// and empty.
	GetOldInput(key string) []string

	// OldInputIsEmpty reports whether there is no old input at all.
	OldInputIsEmpty() bool

	// GetToken returns the CSRF token.
	GetToken() string
}

// OldInput is a [Store] over the url.Values the framework flashes.
//
// The old input and the token arrive on view.Page as plain values, and
// this is the adapter that lets a form read them without this package
// importing the view layer.
type OldInput struct {
	// Values is what was typed on the attempt that was sent back, keyed by
	// input name. It is view.Page.Old.
	Values url.Values

	// Token is the CSRF token issued for this session. It is view.Page.Token.
	Token string
}

// GetOldInput returns what was typed into one field.
func (o OldInput) GetOldInput(key string) []string { return o.Values[key] }

// OldInputIsEmpty reports that nothing was flashed.
func (o OldInput) OldInputIsEmpty() bool { return len(o.Values) == 0 }

// GetToken returns the CSRF token.
func (o OldInput) GetToken() string { return o.Token }

// Model is the minimum surface [FormBuilder.Model] needs from whatever it
// is given.
//
// Reaching a field by a name held in a string is reflection, which this
// framework rejects, so a model says what it holds by answering a method
// instead.
type Model interface {
	// GetAttribute returns the value for key, and false when the model has
	// nothing for it. A field with nothing leaves the input empty rather
	// than filling it with the empty string, and the two differ for a
	// checkbox.
	GetAttribute(key string) (string, bool)
}

// ModelAttributes is a [Model] backed by a map, the shape a handler reaches
// for when it has a row rather than an entity.
type ModelAttributes map[string]string

// GetAttribute returns the value for key, and whether it was present.
func (m ModelAttributes) GetAttribute(key string) (string, bool) {
	value, ok := m[key]
	return value, ok
}
