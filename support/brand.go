package support

import "github.com/arandu-io/hesape/str"

// DefaultBrand is the slug an application is given when it has no name of its
// own: the one the collection's cookies, storage keys and internal paths carried
// before they could be named.
const DefaultBrand = "arandu"

// Brand is the application's name as a visitor's browser is given it: the
// prefix of every cookie the framework writes, of the keys its scripts keep in
// localStorage and of the address its static files and its own endpoints are
// served under.
//
// It is one value and not one setting per item, because the items have to agree
// with each other. The session cookie is written by one package and read by
// another, the token refresh is asked by a script and answered by a middleware,
// and a stylesheet names a font by the same path the asset route serves: a name
// configured in two places is a name that drifts in one of them. The value is
// built once, from the application's name, and handed to whatever writes or
// reads something the browser sees, at the wiring, the way the key and the
// Secure flag are.
//
// The zero Brand is the default one, so a value nobody set names things as the
// collection always did and an application that configures nothing is not
// changed. It has no state and no methods that write: it is safe to copy and to
// share.
type Brand struct {
	slug string
}

// NewBrand returns the Brand of an application name.
//
// The name is what APP_NAME holds, in any spelling: "Peráta", "peráta", "My
// App" and "my-app" are "perata" and "my_app". Accents are folded to the letter
// they are built on and every run of anything that is not a letter or a digit
// becomes one underscore, so the result is safe in a cookie name, in a storage
// key and in a path segment alike. A name with no letter or digit in it -- empty,
// or written in an alphabet the fold does not cover -- is the default Brand,
// because a prefix of nothing would name every cookie "_session".
func NewBrand(name string) Brand {
	slug := str.Slug(name, "_")
	if slug == "" || slug == DefaultBrand {
		return Brand{}
	}
	return Brand{slug: slug}
}

// Slug is the brand as one word of lowercase letters, digits and underscores:
// "perata", or "arandu" for the default.
func (b Brand) Slug() string {
	if b.slug == "" {
		return DefaultBrand
	}
	return b.slug
}

// IsDefault reports whether this is the collection's own name, which is what an
// application that configured nothing has.
func (b Brand) IsDefault() bool { return b.slug == "" }

// Cookie is the name of one of the cookies the framework writes: Cookie
// ("session") is "perata_session".
func (b Brand) Cookie(kind string) string { return b.Slug() + "_" + kind }

// StorageKey is the key one of the framework's scripts keeps a preference
// under: StorageKey("theme") is "perata.theme".
func (b Brand) StorageKey(name string) string { return b.Slug() + "." + name }

// Path is an address under the brand's own prefix, which begins with an
// underscore so that no route of the application's can be mistaken for it:
// Path("assets/") is "/_perata/assets/".
func (b Brand) Path(rest string) string { return "/_" + b.Slug() + "/" + rest }
