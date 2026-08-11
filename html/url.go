package html

import "net/url"

// UrlGenerator is the minimum of Illuminate\Routing\UrlGenerator that
// [HtmlBuilder] and [FormBuilder] reach for.
//
// It is declared here rather than imported because hesape has no UrlGenerator
// yet: routing.Routes answers Route by name and view.URL answers an embedded
// asset, and nothing joins them behind one contract. Declaring the narrow shape
// in the package that needs it is what a Go package does instead of waiting.
//
// The name keeps PHP's spelling -- Url, not URL -- because it names the Laravel
// class rather than a Go identifier of ours (ADR 0044). The methods do take the
// Go spelling where PHP has an initialism.
//
// # The two contracts an implementation has to keep
//
//   - To returns an absolute URL unchanged. [HtmlBuilder.LinkAsset] resolves the
//     asset first and then hands the result to [HtmlBuilder.Link], which calls
//     To on it; Laravel's UrlGenerator::to has this property, and without it
//     every asset link comes back with the host written twice.
//   - Route and Action return an error for a name that is not registered. PHP
//     throws InvalidArgumentException there, which is the (T, error) ADR 0044
//     allows.
type UrlGenerator interface {
	// To answers to UrlGenerator::to. extra is the $extra array of extra path
	// segments.
	To(path string, extra ...string) string

	// Secure answers to UrlGenerator::secure: To over https.
	//
	// PHP passes $secure as a third argument that is null, true or false. Go
	// has no null bool, and the third state -- forcing plain http on a page
	// served over https -- is a mixed-content warning nobody asks for on
	// purpose, so the builders keep PHP's `secure bool` parameter and dispatch
	// to this method rather than passing a flag down.
	Secure(path string, extra ...string) string

	// Asset answers to UrlGenerator::asset.
	Asset(path string) string

	// SecureAsset answers to UrlGenerator::secureAsset.
	SecureAsset(path string) string

	// Route answers to UrlGenerator::route. parameters are positional, which is
	// the shape routing.Routes.Route already takes.
	Route(name string, parameters ...string) (string, error)

	// Action answers to UrlGenerator::action.
	Action(action string, parameters ...string) (string, error)

	// Current answers to UrlGenerator::current: the address of the request
	// being served, which is where a form posts when it says nothing else.
	Current() string
}

// Store is the minimum of Illuminate\Session\Store that [FormBuilder] reaches
// for: the old input a rejected submission left behind, and the CSRF token.
//
// PHP overloads getOldInput -- one argument for a field, none for the whole
// array. Go cannot, so the keyed form is GetOldInput and the question PHP asks
// of the whole array is [Store.OldInputIsEmpty], which is the name
// FormBuilder::oldInputIsEmpty already uses for it.
type Store interface {
	// GetOldInput answers to Store::getOldInput($key).
	//
	// It returns a slice where PHP returns a scalar or an array, because a
	// checkbox group posts many values under one name and
	// FormBuilder::getCheckboxCheckedState branches on which of the two it got.
	// Nil means the field was not in the old input at all, which is the null
	// PHP checks for; an empty non-nil slice means it was there and empty.
	GetOldInput(key string) []string

	// OldInputIsEmpty answers to count($session->getOldInput()) == 0.
	OldInputIsEmpty() bool

	// GetToken answers to Store::getToken.
	GetToken() string
}

// OldInput is a [Store] over the url.Values the framework flashes.
//
// It has no PHP counterpart: there the session is one object with everything on
// it. Here the old input and the token arrive on view.Page as plain values, and
// this is the adapter that lets a form read them without this package importing
// the view layer.
type OldInput struct {
	// Values is what was typed on the attempt that was sent back, keyed by
	// input name. It is view.Page.Old.
	Values url.Values

	// Token is the CSRF token issued for this session. It is view.Page.Token.
	Token string
}

// GetOldInput answers to Store::getOldInput($key).
func (o OldInput) GetOldInput(key string) []string { return o.Values[key] }

// OldInputIsEmpty reports that nothing was flashed.
func (o OldInput) OldInputIsEmpty() bool { return len(o.Values) == 0 }

// GetToken answers to Store::getToken.
func (o OldInput) GetToken() string { return o.Token }

// Model is the minimum of the $model [FormBuilder.Model] is given.
//
// PHP reaches the value with object_get($this->model, $key), which is
// reflection over a property name held in a string. That is the mechanism this
// framework rejects, so a model says what it holds by answering a method: the
// name is Eloquent's Model::getAttribute, which is what object_get lands on for
// a real model anyway.
type Model interface {
	// GetAttribute answers to Model::getAttribute. The bool is PHP's null: a
	// field the model has nothing for leaves the input empty rather than
	// filling it with the empty string, and the two differ for a checkbox.
	GetAttribute(key string) (string, bool)
}

// ModelAttributes is a [Model] backed by a map.
//
// It answers to the is_array($this->model) branch of
// FormBuilder::getModelValueAttribute, which is a supported shape in PHP and
// the one a handler reaches for when it has a row rather than an entity.
type ModelAttributes map[string]string

// GetAttribute answers to Model::getAttribute.
func (m ModelAttributes) GetAttribute(key string) (string, bool) {
	value, ok := m[key]
	return value, ok
}
