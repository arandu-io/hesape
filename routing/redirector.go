package routing

import (
	"net/http"
	"strings"
	"time"

	"github.com/arandu-io/hesape/collections/arr"
	hhttp "github.com/arandu-io/hesape/http"
)

// Redirector builds RedirectResponses to every place a handler can send the
// browser: back to the previous page, to a named route, to a path, to the
// address an unauthenticated visitor was trying to reach.
//
// It holds a UrlGenerator and optionally a session store, and every method
// that answers a redirect does so by building a URL through the generator and
// wrapping it.
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

// Back sends the browser where it came from, and to the fallback when it
// came from nowhere -- a bookmarked form, a link opened in a new tab.
func (r *Redirector) Back(status int, headers http.Header, fallback string) *Redirect {
	return r.createRedirect(r.generator.Previous(fallback), status, headers)
}

// Refresh sends the browser to the current URL.
func (r *Redirector) Refresh(status int, headers http.Header) *Redirect {
	path := "/"
	if req := r.generator.GetRequest(); req != nil {
		path = req.URL.Path
	}
	return r.To(path, status, headers, nil)
}

// To sends the browser to path.
//
//	redirector.To("/invoices/42", 302, nil, nil)
func (r *Redirector) To(path string, status int, headers http.Header, secure *bool) *Redirect {
	u := r.generator.To(path, nil, secure)
	return r.createRedirect(u, status, headers)
}

// Away sends the browser to an external URL, without validating the
// destination -- for a third-party integration, not for a path within the
// application.
func (r *Redirector) Away(path string, status int, headers http.Header) *Redirect {
	return r.createRedirect(path, status, headers)
}

// Secure sends the browser to path over https.
func (r *Redirector) Secure(path string, status int, headers http.Header) *Redirect {
	t := true
	return r.To(path, status, headers, &t)
}

// Route sends the browser to a named route.
func (r *Redirector) Route(name string, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.Route(name, parameters, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// SignedRoute sends the browser to a signed URL for a named route.
func (r *Redirector) SignedRoute(name string, parameters map[string]any, expiration time.Time, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.SignedRoute(name, parameters, expiration, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// TemporarySignedRoute sends the browser to a signed URL for a named route,
// expiring at the given time.
func (r *Redirector) TemporarySignedRoute(name string, expiration time.Time, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.TemporarySignedRoute(name, expiration, parameters, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// Action sends the browser to a controller action.
func (r *Redirector) Action(action string, parameters map[string]any, status int, headers http.Header) (*Redirect, error) {
	u, err := r.generator.Action(action, parameters, true)
	if err != nil {
		return nil, err
	}
	return r.createRedirect(u, status, headers), nil
}

// Home sends the browser to the route named "home", falling back to "/" when
// there is none.
func (r *Redirector) Home(status int, headers http.Header) *Redirect {
	if u, err := r.generator.Route("home", nil, true); err == nil {
		return r.createRedirect(u, status, headers)
	}
	return r.To("/", status, headers, nil)
}

// Guest sends the browser to a path, storing where it was going so Intended
// can bring it back after a sign-in.
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

// SetIntendedUrl stores a URL for Intended to read back.
func (r *Redirector) SetIntendedUrl(u string) *Redirector {
	if r.session != nil {
		r.session.Put("url.intended", u)
	}
	return r
}

// GetIntendedUrl reads the stored URL without removing it.
func (r *Redirector) GetIntendedUrl() string {
	if r.session == nil {
		return ""
	}
	if s, ok := r.session.Get("url.intended").(string); ok {
		return s
	}
	return ""
}

// GetUrlGenerator returns the generator backing this redirector.
func (r *Redirector) GetUrlGenerator() *UrlGenerator { return r.generator }

// createRedirect builds every Redirect this redirector answers, carrying its
// session and the request the generator was built for, so that what a handler
// flashes on the redirect -- With, WithInput, OnlyInput -- reaches the session
// instead of a nil store that drops it.
func (r *Redirector) createRedirect(path string, status int, headers http.Header) *Redirect {
	if status == 0 {
		status = http.StatusFound
	}
	if headers == nil {
		headers = http.Header{}
	}
	var request *http.Request
	if r.generator != nil {
		request = r.generator.GetRequest()
	}
	return &Redirect{Path: path, Status: status, Headers: headers, Session: r.session, request: request}
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
	// A Redirector fills it with its own session; a Redirect written as a
	// literal flashes nothing until it is set.
	Session SessionStore

	// request is the request OnlyInput and ExceptInput read the input off. A
	// Redirector fills it; a literal has none.
	request *http.Request
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

// OnlyInput flashes the named keys of the request's input, and nothing else
// of it.
//
//	return redirector.Back(0, nil, "/login").OnlyInput("email")
//
// A key may be a "dot" path into nested input, and a key the request did not
// send is skipped. The input is the request's fields as hesape/http reads them,
// without the uploaded files: a file is not something to put back in a text
// box.
//
// It reads the request the Redirector was built for, so a Redirect written as
// a literal, which has none, flashes nothing; so does one with no Session.
func (r *Redirect) OnlyInput(keys ...string) *Redirect {
	input, ok := r.input()
	if !ok {
		return r
	}
	only := map[string]any{}
	for _, key := range keys {
		if value, found := arr.Get(input, key); found {
			arr.Set(only, key, value)
		}
	}
	return r.WithInput(only)
}

// ExceptInput flashes the request's input without the named keys.
//
//	return redirector.Back(0, nil, "/register").ExceptInput("password", "password_confirmation")
//
// A key may be a "dot" path into nested input. The input is read as OnlyInput
// reads it, from the same request and without the uploaded files, and flashes
// nothing under the same two conditions.
func (r *Redirect) ExceptInput(keys ...string) *Redirect {
	input, ok := r.input()
	if !ok {
		return r
	}
	return r.WithInput(arr.Except(input, keys...))
}

// input is the request's input without its files, and false when there is no
// request to read it from or no session to flash it into.
func (r *Redirect) input() (map[string]any, bool) {
	if r.request == nil || r.Session == nil {
		return nil, false
	}
	input, _ := hhttp.NewRequest(r.request).Input("").(map[string]any)
	if input == nil {
		input = map[string]any{}
	}
	return input, true
}

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
