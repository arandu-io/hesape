package session

import "github.com/arandu-io/hesape/support"

// CookieNames are the names of the four cookies the framework writes to a
// visitor's browser: the session, the guest binding of a CSRF token, the one-shot
// flash and the address a guard remembered.
//
// They are derived from one [support.Brand], so that the part of the framework
// that writes a cookie and the part that reads it cannot disagree about the name:
// every constructor that touches one takes the same value through
// [WithCookieNames]. A store built without it uses [DefaultCookieNames], which
// are the names the collection has always written.
type CookieNames struct {
	// Session is the cookie the session id travels in.
	Session string

	// CSRFGuest is the cookie that binds a CSRF token to a visitor with no
	// session.
	CSRFGuest string

	// Flash is the cookie that carries the messages and the typed input of a
	// rejected request to the page it is sent back to.
	Flash string

	// Intended is the cookie that carries the address a guard turned somebody
	// away from, to the sign-in that follows.
	Intended string
}

// CookieNamesFor returns the names of an application's cookies: for the brand of
// "Peráta" they are perata_session, perata_csrf_guest, perata_flash and
// perata_intended.
func CookieNamesFor(brand support.Brand) CookieNames {
	return CookieNames{
		Session:   brand.Cookie("session"),
		CSRFGuest: brand.Cookie("csrf_guest"),
		Flash:     brand.Cookie("flash"),
		Intended:  brand.Cookie("intended"),
	}
}

// DefaultCookieNames returns the names a store uses when it was not given
// others: [CookieName], [GuestCookieName], [FlashCookieName] and, in hesape/http,
// IntendedCookieName.
func DefaultCookieNames() CookieNames { return CookieNamesFor(support.Brand{}) }

// CookieOption adjusts the cookies a store, an issuer or a flash writes. It is
// variadic on every constructor it reaches, so the call that passes none is the
// call every application already writes.
type CookieOption func(*cookieSettings)

// cookieSettings is what the options add up to. Unexported, so the set of things
// a caller can ask for is the set of exported options.
type cookieSettings struct {
	names CookieNames
}

// WithCookieNames names the cookies after the application. An empty name in names
// keeps the default for that cookie, so a caller that renames one does not have
// to spell out the other three.
func WithCookieNames(names CookieNames) CookieOption {
	return func(s *cookieSettings) {
		defaults := DefaultCookieNames()
		if names.Session != "" {
			defaults.Session = names.Session
		}
		if names.CSRFGuest != "" {
			defaults.CSRFGuest = names.CSRFGuest
		}
		if names.Flash != "" {
			defaults.Flash = names.Flash
		}
		if names.Intended != "" {
			defaults.Intended = names.Intended
		}
		s.names = defaults
	}
}

// ResolveCookieNames returns the names the options ask for, and
// [DefaultCookieNames] when there are none. It is exported for the constructors
// of other packages that take the same options, such as hesape/http, and is not
// something an application calls.
func ResolveCookieNames(opts ...CookieOption) CookieNames {
	s := cookieSettings{names: DefaultCookieNames()}
	for _, opt := range opts {
		if opt == nil {
			panic("session: a nil CookieOption was passed. Pass session.WithCookieNames(...), or pass none")
		}
		opt(&s)
	}
	return s.names
}
