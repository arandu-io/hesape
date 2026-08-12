package routing

import (
	"net/http"
	"strings"
	"time"
)

// Redirector builds RedirectResponses to every place a handler can send the
// browser: back to the previous page, to a named route, to a path, to the
// address an unauthenticated visitor was trying to reach.
//
// It mirrors Illuminate\Routing\Redirector. It holds a UrlGenerator and
// optionally a session store, and every method that answers a redirect does so
// by building a URL through the generator and wrapping it.
type Redirector struct {
	generator *UrlGenerator
	session   SessionStore
}

// NewRedirector returns a redirector backed by g.
func NewRedirector(g *UrlGenerator) *Redirector {
	return &Redirector{generator: g}
}

// SetSession attaches a session store so Intended and Guest can read and write
// the intended URL.
func (r *Redirector) SetSession(s SessionStore) { r.session = s }

// Back is Redirector::back. It sends the browser where it came from, and to
// the fallback when it came from nowhere -- a bookmarked form, a link opened
// in a new tab.
func (r *Redirector) Back(status int, headers http.Header, fallback string) *Redirect {
	return r.createRedirect(r.generator.Previous(fallback), status, headers)
}

// Refresh is Redirector::refresh. It sends the browser to the current URL.
func (r *Redirector) Refresh(status int, headers http.Header) *Redirect {
	path := "/"
	if req := r.generator.GetRequest(); req != nil {
		path = req.URL.Path
	}
	return r.To(path, status, headers, nil)
}

// To is Redirector::to.
//
//	redirector.To("/invoices/42", 302, nil, nil)
func (r *Redirector) To(path string, status int, headers http.Header, secure *bool) *Redirect {
	u := r.generator.To(path, nil, secure)
	return r.createRedirect(u, status, headers)
}

// Away is Redirector::away. It sends the browser to an external URL, without
// validating the destination -- for a third-party integration, not for a path
// within the application.
func (r *Redirector) Away(path string, status int, headers http.Header) *Redirect {
	return r.createRedirect(path, status, headers)
}

// Secure is Redirector::secure.
func (r *Redirector) Secure(path string, status int, headers http.Header) *Redirect {
	t := true
	return r.To(path, status, headers, &t)
}

// Route is Redirector::route.
func (r *Redirector) Route(name string, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.Route(name, parameters, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// SignedRoute is Redirector::signedRoute.
func (r *Redirector) SignedRoute(name string, parameters map[string]any, expiration time.Time, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.SignedRoute(name, parameters, expiration, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// TemporarySignedRoute is Redirector::temporarySignedRoute.
func (r *Redirector) TemporarySignedRoute(name string, expiration time.Time, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.TemporarySignedRoute(name, expiration, parameters, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// Action is Redirector::action.
func (r *Redirector) Action(action string, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.Action(action, parameters, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// Home is Redirector::home. It sends the browser to the route named "home",
// falling back to "/" when there is none.
func (r *Redirector) Home(status int, headers http.Header) *Redirect {
	if u, err := r.generator.Route("home", nil, true); err == nil {
		return r.createRedirect(u, status, headers)
	}
	return r.To("/", status, headers, nil)
}

// Guest is Redirector::guest. It sends the browser to a path, storing where it
// was going so Intended can bring it back after a sign-in.
func (r *Redirector) Guest(path string, status int, headers http.Header, secure *bool) *Redirect {
	req := r.generator.GetRequest()
	if req != nil && req.Method == http.MethodGet && !strings.Contains(req.Header.Get("Accept"), "application/json") {
		intended := r.generator.Full()
		if intended != "" {
			r.SetIntendedUrl(intended)
		}
	} else {
		intended := r.generator.Previous("")
		if intended != "" && intended != "/" {
			r.SetIntendedUrl(intended)
		}
	}
	return r.To(path, status, headers, secure)
}

// Intended sends the browser to the URL stored by Guest, or to the default.
func (r *Redirector) Intended(def string, status int, headers http.Header, secure *bool) *Redirect {
	path := def
	if r.session != nil {
		if v := r.session.Pull("url.intended"); v != nil {
			if s, ok := v.(string); ok && s != "" {
				path = s
			}
		}
	}
	return r.To(path, status, headers, secure)
}

// SetIntendedUrl is Redirector::setIntendedUrl. It stores a URL for Intended
// to read back.
func (r *Redirector) SetIntendedUrl(u string) *Redirector {
	if r.session != nil {
		r.session.Put("url.intended", u)
	}
	return r
}

// GetIntendedUrl is Redirector::getIntendedUrl. It reads the stored URL
// without removing it.
func (r *Redirector) GetIntendedUrl() string {
	if r.session == nil {
		return ""
	}
	if s, ok := r.session.Get("url.intended").(string); ok {
		return s
	}
	return ""
}

// GetUrlGenerator is Redirector::getUrlGenerator.
func (r *Redirector) GetUrlGenerator() *UrlGenerator { return r.generator }

func (r *Redirector) createRedirect(path string, status int, headers http.Header) *Redirect {
	if status == 0 {
		status = http.StatusFound
	}
	if headers == nil {
		headers = http.Header{}
	}
	return &Redirect{Path: path, Status: status, Headers: headers}
}

// Redirect is the data a handler returns to tell the framework to send a
// redirect response.
//
// The fields are public so the caller can add cookies, flash messages and
// headers before returning it.
type Redirect struct {
	Path    string
	Status  int
	Headers http.Header
	// Session carries flash data attached to the redirect: WithInput, WithErrors.
	Session SessionStore
}

// With flashes the value under key so the next request reads it.
func (r *Redirect) With(key string, value any) *Redirect {
	if r.Session != nil {
		r.Session.Put(key, value)
	}
	return r
}

// WithInput flashes the input so the form comes back filled.
func (r *Redirect) WithInput(input map[string]any) *Redirect {
	return r.With("_old_input", input)
}

// WithErrors flashes errors so the form shows them.
func (r *Redirect) WithErrors(errors any) *Redirect {
	return r.With("errors", errors)
}

// WithoutInput removes the flashed input.
func (r *Redirect) WithoutInput() *Redirect {
	if r.Session != nil {
		r.Session.Put("_old_input", nil)
	}
	return r
}

// OnlyInput flashes only the named keys from the current input.
func (r *Redirect) OnlyInput(keys ...string) *Redirect { return r }

// ExceptInput flashes everything except the named keys.
func (r *Redirect) ExceptInput(keys ...string) *Redirect { return r }

// WithFragment appends a fragment to the URL.
func (r *Redirect) WithFragment(fragment string) *Redirect {
	if fragment != "" {
		r.Path += "#" + fragment
	}
	return r
}

// WithoutFragment removes the fragment.
func (r *Redirect) WithoutFragment() *Redirect {
	if i := strings.IndexByte(r.Path, '#'); i >= 0 {
		r.Path = r.Path[:i]
	}
	return r
}

// GetTargetURL returns the path being redirected to.
func (r *Redirect) GetTargetURL() string { return r.Path }

// SetTargetURL replaces the path.
func (r *Redirect) SetTargetURL(u string) { r.Path = u }
