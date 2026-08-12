package http

import (
	"mime/multipart"
	stdhttp "net/http"
	"net/url"
	"strings"

	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/support"
	"github.com/arandu-io/hesape/validation"
)

// RedirectResponse mirrors Illuminate\Http\RedirectResponse: a Response whose
// whole content is a Location header, plus the four things a redirect carries
// across the request boundary -- flashed data, old input, errors and cookies.
//
// It embeds Response, which is how the PHP `extends BaseRedirectResponse` plus
// `use ResponseTrait` arrives in Go.
//
// The tenant is never flashed and never read back (RULE 14): WithInput carries
// what the browser typed, and what a repository is allowed to see comes from
// the auth.Grant a policy minted, not from a value that survived a redirect.
type RedirectResponse struct {
	Response

	// targetURL is Symfony's $targetUrl: where the browser is being sent.
	targetURL string
	// request is RedirectResponse::$request, which WithInput and OnlyInput
	// read the input off.
	request *Request
	// session is RedirectResponse::$session, which With, WithInput and
	// WithErrors flash into.
	session *session.Store
}

// NewRedirectResponse answers to Symfony's RedirectResponse::__construct, which
// Illuminate's subclass inherits.
//
// The variadic arguments are the PHP's defaults: status (302) and headers
// (none).
func NewRedirectResponse(to string, args ...any) *RedirectResponse {
	status := stdhttp.StatusFound
	if len(args) > 0 {
		if value, ok := args[0].(int); ok {
			status = value
		}
	}
	r := &RedirectResponse{
		Response: Response{
			status:          status,
			headers:         stdhttp.Header{},
			protocolVersion: "1.0",
		},
	}
	if len(args) > 1 {
		switch headers := args[1].(type) {
		case stdhttp.Header:
			r.WithHeaders(headers)
		case map[string]string:
			for key, value := range headers {
				r.Response.headers.Set(key, value)
			}
		}
	}
	return r.SetTargetUrl(to)
}

// GetTargetUrl answers to Symfony's RedirectResponse::getTargetUrl: where the
// browser is being sent.
func (r *RedirectResponse) GetTargetUrl() string { return r.targetURL }

// SetTargetUrl answers to Symfony's RedirectResponse::setTargetUrl, which
// WithFragment and EnforceSameOrigin both call.
func (r *RedirectResponse) SetTargetUrl(to string) *RedirectResponse {
	r.targetURL = to
	r.Response.headers.Set("Location", to)
	return r
}

// With answers to RedirectResponse::with: flash a piece of data to the session,
// so the page that follows the redirect can read it once.
//
// The PHP takes string|array for the key; a map flashes every pair in it, and a
// string flashes the single pair. The value is variadic because the PHP's
// second argument defaults to null.
func (r *RedirectResponse) With(key any, value ...any) *RedirectResponse {
	if r.session == nil {
		return r
	}
	var single any
	if len(value) > 0 {
		single = value[0]
	}
	switch typed := key.(type) {
	case map[string]any:
		for k, v := range typed {
			r.session.Flash(k, v)
		}
	case map[string]string:
		for k, v := range typed {
			r.session.Flash(k, v)
		}
	default:
		r.session.Flash(stringify(key), single)
	}
	return r
}

// WithCookies answers to RedirectResponse::withCookies: add several cookies at
// once.
func (r *RedirectResponse) WithCookies(cookies []*stdhttp.Cookie) *RedirectResponse {
	for _, cookie := range cookies {
		r.Response.WithCookie(cookie)
	}
	return r
}

// WithInput answers to RedirectResponse::withInput: flash the input to the
// session so the form that was rejected comes back with the boxes filled.
//
// It is one half of a pair with [Request.Old], which reads what this writes. If
// the two diverge, the form comes back blank, which is the bug the pair exists
// to not have.
//
// The variadic argument is the PHP's optional $input: with none, the request's
// own input is flashed.
func (r *RedirectResponse) WithInput(input ...map[string]any) *RedirectResponse {
	if r.session == nil {
		return r
	}
	payload := map[string]any{}
	if len(input) > 0 && input[0] != nil {
		payload = input[0]
	} else if r.request != nil {
		payload, _ = r.request.Input("").(map[string]any)
	}
	r.session.FlashInput(removeFilesFromInput(payload))
	return r
}

// removeFilesFromInput answers to RedirectResponse::removeFilesFromInput: an
// uploaded file is not something to put back in a text box, and it is not
// something to keep in a session either.
func removeFilesFromInput(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		if isUploadedValue(value) {
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			out[key] = removeFilesFromInput(typed)
		case []any:
			list := make([]any, 0, len(typed))
			for _, item := range typed {
				if isUploadedValue(item) {
					continue
				}
				list = append(list, item)
			}
			out[key] = list
		default:
			out[key] = value
		}
	}
	return out
}

// isUploadedValue reports whether a value is a file that arrived on the
// request, in either of the two shapes [Request.AllFiles] produces.
func isUploadedValue(value any) bool {
	switch value.(type) {
	case *multipart.FileHeader, []*multipart.FileHeader, *UploadedFile, []*UploadedFile:
		return true
	}
	return false
}

// OnlyInput answers to RedirectResponse::onlyInput: flash only the named keys.
func (r *RedirectResponse) OnlyInput(keys ...string) *RedirectResponse {
	if r.request == nil {
		return r
	}
	return r.WithInput(r.request.Only(keys...))
}

// ExceptInput answers to RedirectResponse::exceptInput: flash everything except
// the named keys. It is what keeps a password out of the session.
func (r *RedirectResponse) ExceptInput(keys ...string) *RedirectResponse {
	if r.request == nil {
		return r
	}
	return r.WithInput(r.request.Except(keys...))
}

// WithErrors answers to RedirectResponse::withErrors: flash a container of
// messages into the session's error bag, so the page that follows draws them
// beside the fields they belong to.
//
// The PHP takes MessageProvider|array|string; this takes any and reads the same
// shapes, plus validation.Errors, which is what this module's validator
// produces. The variadic key is the PHP's $key, defaulting to the bag named
// "default" -- which is what lets two forms on one page keep their messages
// apart.
func (r *RedirectResponse) WithErrors(provider any, key ...string) *RedirectResponse {
	if r.session == nil {
		return r
	}
	bagName := support.DefaultErrorBag
	if len(key) > 0 && key[0] != "" {
		bagName = key[0]
	}

	errors, _ := r.session.Get("errors", support.NewViewErrorBag()).(*support.ViewErrorBag)
	if errors == nil {
		errors = support.NewViewErrorBag()
	}
	r.session.Flash("errors", errors.Put(bagName, parseErrors(provider)))
	return r
}

// parseErrors answers to RedirectResponse::parseErrors.
func parseErrors(provider any) *support.MessageBag {
	switch typed := provider.(type) {
	case nil:
		return support.NewMessageBag(nil)
	case support.MessageProvider:
		return typed.GetMessageBag()
	case *support.MessageBag:
		return typed
	case validation.Errors:
		return support.NewMessageBag(map[string][]string(typed))
	case map[string][]string:
		return support.NewMessageBag(typed)
	case map[string]string:
		messages := make(map[string][]string, len(typed))
		for field, message := range typed {
			messages[field] = []string{message}
		}
		return support.NewMessageBag(messages)
	case []string:
		return support.NewMessageBag(map[string][]string{support.DefaultErrorBag: typed})
	case string:
		return support.NewMessageBag(map[string][]string{support.DefaultErrorBag: {typed}})
	}
	return support.NewMessageBag(map[string][]string{support.DefaultErrorBag: {stringify(provider)}})
}

// WithFragment answers to RedirectResponse::withFragment: put a "#anchor" on
// the target, replacing one that is already there.
func (r *RedirectResponse) WithFragment(fragment string) *RedirectResponse {
	return r.WithoutFragment().
		SetTargetUrl(r.GetTargetUrl() + "#" + strAfter(fragment, "#"))
}

// WithoutFragment answers to RedirectResponse::withoutFragment: drop any
// "#anchor" from the target.
func (r *RedirectResponse) WithoutFragment() *RedirectResponse {
	return r.SetTargetUrl(strBefore(r.GetTargetUrl(), "#"))
}

// EnforceSameOrigin answers to RedirectResponse::enforceSameOrigin: replace the
// target with the fallback unless it is on the same origin as the request.
//
// It is the open-redirect defence on the Response side, and it answers the same
// question [LocalPath] answers on the Request side -- by host rather than by
// refusing a host at all, because the PHP does. A framework redirect built from
// a path should still go through LocalPath; this is for the target that
// genuinely carries a host.
//
// The variadic arguments are the PHP's $validateScheme and $validatePort, both
// defaulting to true.
func (r *RedirectResponse) EnforceSameOrigin(fallback string, args ...bool) *RedirectResponse {
	validateScheme, validatePort := true, true
	if len(args) > 0 {
		validateScheme = args[0]
	}
	if len(args) > 1 {
		validatePort = args[1]
	}

	target, err := url.Parse(r.targetURL)
	if err != nil {
		return r.SetTargetUrl(fallback)
	}
	// A target with no host is already a path on this origin, which is the one
	// shape that cannot leave it.
	if target.Host == "" && target.Scheme == "" {
		return r
	}
	if r.request == nil {
		return r.SetTargetUrl(fallback)
	}
	current, err := url.Parse(r.request.SchemeAndHttpHost())
	if err != nil {
		return r.SetTargetUrl(fallback)
	}

	if !strings.EqualFold(target.Hostname(), current.Hostname()) ||
		(validateScheme && !strings.EqualFold(target.Scheme, current.Scheme)) ||
		(validatePort && target.Port() != current.Port()) {
		return r.SetTargetUrl(fallback)
	}
	return r
}

// GetOriginalContent answers to RedirectResponse::getOriginalContent, which
// returns nothing: a redirect has no body to have had an original.
func (r *RedirectResponse) GetOriginalContent() any { return nil }

// GetRequest answers to RedirectResponse::getRequest.
func (r *RedirectResponse) GetRequest() *Request { return r.request }

// SetRequest answers to RedirectResponse::setRequest.
func (r *RedirectResponse) SetRequest(request *Request) *RedirectResponse {
	r.request = request
	return r
}

// GetSession answers to RedirectResponse::getSession.
func (r *RedirectResponse) GetSession() *session.Store { return r.session }

// SetSession answers to RedirectResponse::setSession.
func (r *RedirectResponse) SetSession(store *session.Store) *RedirectResponse {
	r.session = store
	return r
}

// The next block redeclares the ResponseTrait methods that return $this. Go has
// no covariant return type, so the promoted method from the embedded Response
// would hand back a *Response and end the chain; PHP's $this keeps its class,
// and these keep the type so that
//
//	redirect("/home").With("status", "saved").WithCookie(c).WithFragment("top")
//
// compiles the way the Laravel line it mirrors reads.

// Header is ResponseTrait::header, typed for the chain.
func (r *RedirectResponse) Header(key string, values ...any) *RedirectResponse {
	r.Response.Header(key, values...)
	return r
}

// WithHeaders is ResponseTrait::withHeaders, typed for the chain.
func (r *RedirectResponse) WithHeaders(headers stdhttp.Header) *RedirectResponse {
	r.Response.WithHeaders(headers)
	return r
}

// WithoutHeader is ResponseTrait::withoutHeader, typed for the chain.
func (r *RedirectResponse) WithoutHeader(keys ...string) *RedirectResponse {
	r.Response.WithoutHeader(keys...)
	return r
}

// Cookie is ResponseTrait::cookie, typed for the chain.
func (r *RedirectResponse) Cookie(cookie *stdhttp.Cookie) *RedirectResponse {
	return r.WithCookie(cookie)
}

// WithCookie is ResponseTrait::withCookie, typed for the chain.
func (r *RedirectResponse) WithCookie(cookie *stdhttp.Cookie) *RedirectResponse {
	r.Response.WithCookie(cookie)
	return r
}

// WithoutCookie is ResponseTrait::withoutCookie, typed for the chain.
func (r *RedirectResponse) WithoutCookie(name string, args ...string) *RedirectResponse {
	r.Response.WithoutCookie(name, args...)
	return r
}

// WithException is ResponseTrait::withException, typed for the chain.
func (r *RedirectResponse) WithException(err error) *RedirectResponse {
	r.Response.WithException(err)
	return r
}
