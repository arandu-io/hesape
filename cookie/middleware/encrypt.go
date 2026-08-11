package middleware

import (
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/cookie"
	"github.com/arandu-io/hesape/encryption"
)

// EncryptCookies answers to Illuminate\Cookie\Middleware\EncryptCookies. It
// decrypts the cookies on the way in and encrypts them on the way out, so a
// handler reads and writes plain values and the browser only ever holds
// ciphertext.
//
// It is a struct with a Handle method, as the PHP is a class with a handle
// method, and Handle has the shape pipeline.Middleware[http.Handler] wants:
//
//	encrypt := middleware.NewEncryptCookies(encrypter)
//	encrypt.DisableFor("XSRF-TOKEN")
//	r.Get("/", h, encrypt.Handle)
//
// It goes outside AddQueuedCookiesToResponse, as it does in Laravel's global
// stack, so the cookies the jar queued are encrypted too.
type EncryptCookies struct {
	mu sync.RWMutex

	// encrypter is the $encrypter property.
	encrypter *encryption.Encrypter
	// except is the $except property: the names this instance was told to
	// leave alone.
	except []string
}

// neverEncrypt is the static $neverEncrypt property of EncryptCookies, and
// serialize is the static $serialize. Go has no statics; a package-level var is
// what a static property is, and [Except], [Serialized] and [FlushState] are
// the static methods that read and write them.
var (
	stateMu      sync.RWMutex
	neverEncrypt []string
	serialize    bool
)

// NewEncryptCookies answers to EncryptCookies::__construct().
func NewEncryptCookies(encrypter *encryption.Encrypter) *EncryptCookies {
	return &EncryptCookies{encrypter: encrypter}
}

// DisableFor answers to EncryptCookies::disableFor(). The named cookies are
// read and written in the clear by this instance.
//
// PHP takes a string or an array of them; Go takes a variadic, which is the
// same two calls. Names add up across calls, as array_merge does, and a name
// listed twice is harmless.
//
// The cookie that needs this is the CSRF token: JavaScript on the page has to
// read it to put it in a header, and it cannot decrypt.
func (m *EncryptCookies) DisableFor(name ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.except = append(m.except, name...)
}

// IsDisabled answers to EncryptCookies::isDisabled(). It reports whether this
// cookie is left in the clear, by this instance's list or by the global one.
func (m *EncryptCookies) IsDisabled(name string) bool {
	m.mu.RLock()
	mine := slices.Contains(m.except, name)
	m.mu.RUnlock()
	if mine {
		return true
	}

	stateMu.RLock()
	defer stateMu.RUnlock()
	return slices.Contains(neverEncrypt, name)
}

// Except answers to EncryptCookies::except(), a static method: the named
// cookies are never encrypted, by any instance.
//
// PHP takes a string or an array and wraps it with Arr::wrap; Go takes a
// variadic. Duplicates are dropped, as array_unique drops them.
//
// It is global state, and it is global in the PHP too, because the point is to
// declare the exception once at boot rather than on every instance that gets
// built. A test that touches it calls [FlushState] afterwards.
func Except(cookies ...string) {
	stateMu.Lock()
	defer stateMu.Unlock()

	for _, name := range cookies {
		if !slices.Contains(neverEncrypt, name) {
			neverEncrypt = append(neverEncrypt, name)
		}
	}
}

// Serialized answers to EncryptCookies::serialized(), a static method: whether
// this cookie's contents are serialized before encryption.
//
// It reads the static $serialize, which is false and which no released method
// sets. Laravel dropped the setter and left the property; the branch stays here
// for the same reason it stays there, so that the flag has one reader.
//
// The name is taken and ignored, as it is in the PHP -- the flag is per
// application, not per cookie.
func Serialized(name string) bool {
	_ = name

	stateMu.RLock()
	defer stateMu.RUnlock()
	return serialize
}

// FlushState answers to EncryptCookies::flushState(), a static method: it
// empties the global never-encrypt list and clears the serialize flag. A test
// that called [Except] calls this to put the package back.
func FlushState() {
	stateMu.Lock()
	defer stateMu.Unlock()

	neverEncrypt = nil
	serialize = false
}

// Handle answers to EncryptCookies::handle(), which is
// $this->encrypt($next($this->decrypt($request))).
//
// The decrypt half runs the same way here: the request handed downstream
// carries a rewritten Cookie header, so r.Cookie(name) gives a handler the
// plain value. PHP sets the value on a ParameterBag instead, because there the
// request is a bag; the effect is the one thing that matters, which is that
// nothing downstream sees ciphertext.
//
// The encrypt half cannot wait for the handler to return, because by then the
// header is gone. It hangs off the first WriteHeader or Write, or off the
// handler returning without either, and rewrites every Set-Cookie in place.
func (m *EncryptCookies) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ew := &encryptWriter{ResponseWriter: w, middleware: m}
		next.ServeHTTP(ew, m.decrypt(r))
		ew.encryptCookies()
	})
}

// decrypt answers to EncryptCookies::decrypt(): every cookie on the request,
// decrypted and stripped of its value prefix.
//
// A cookie that does not decrypt, or whose prefix does not match its name, is
// dropped from the request. PHP sets it to null, which reads back as a cookie
// that is there and empty; dropping it makes r.Cookie return ErrNoCookie, which
// is the closer of the two answers to "this cookie did not survive" and the one
// that will not be mistaken for a cookie whose value really is "".
func (m *EncryptCookies) decrypt(r *http.Request) *http.Request {
	in := r.Cookies()
	if len(in) == 0 {
		return r
	}

	out := make([]*http.Cookie, 0, len(in))
	changed := false
	for _, c := range in {
		if m.IsDisabled(c.Name) {
			out = append(out, c)
			continue
		}
		changed = true

		plain, err := m.decryptCookie(c.Name, c.Value)
		if err != nil {
			continue
		}
		value, ok := cookie.ValidateCookieValuePrefix(c.Name, plain, m.encrypter.GetAllKeys())
		if !ok || !validCookieValue(value) {
			// Not valid means the value was written for another cookie name,
			// or under a key this application no longer holds. Not a valid
			// cookie value means the plaintext holds a byte a Cookie header
			// cannot carry, so it cannot be handed on; both are the request
			// arriving without that cookie.
			continue
		}
		out = append(out, &http.Cookie{Name: c.Name, Value: value})
	}
	if !changed {
		return r
	}

	clone := new(http.Request)
	*clone = *r
	clone.Header = r.Header.Clone()
	if len(out) == 0 {
		clone.Header.Del("Cookie")
		return clone
	}

	var header strings.Builder
	for i, c := range out {
		if i > 0 {
			header.WriteString("; ")
		}
		header.WriteString(c.Name)
		header.WriteByte('=')
		header.WriteString(c.Value)
	}
	clone.Header.Set("Cookie", header.String())
	return clone
}

// decryptCookie answers to EncryptCookies::decryptCookie(). The array branch of
// the PHP, and decryptArray with it, is not mirrored: an array cookie is PHP's
// SAPI reading a[b]=c into a nested array, and net/http has no such thing --
// a cookie is one name and one string.
func (m *EncryptCookies) decryptCookie(name, value string) (string, error) {
	if Serialized(name) {
		return encryption.Decrypt[string](m.encrypter, value)
	}
	return m.encrypter.DecryptString(value)
}

// encryptCookie is the encrypt() half of the PHP loop for one cookie: the value
// prefix for this name, then the whole thing encrypted.
func (m *EncryptCookies) encryptCookie(name, value string) (string, error) {
	prefixed := cookie.CreateCookieValuePrefix(name, m.encrypter.GetKey()) + value
	if Serialized(name) {
		return encryption.Encrypt(m.encrypter, prefixed)
	}
	return m.encrypter.EncryptString(prefixed)
}

// encryptWriter is the response writer EncryptCookies wraps around the real one
// so the Set-Cookie headers can be rewritten before they go out.
type encryptWriter struct {
	http.ResponseWriter
	middleware *EncryptCookies
	done       bool
}

// encryptCookies answers to EncryptCookies::encrypt(): every cookie on the
// response, re-set with its value encrypted.
//
// A cookie whose value will not encrypt is dropped rather than sent in the
// clear. PHP throws EncryptException and the request becomes a 500; here the
// response is already going out, and the choice is between a cookie that will
// not decrypt on the way back and one the browser never stores. The second is
// the safe half of the same failure.
func (w *encryptWriter) encryptCookies() {
	if w.done {
		return
	}
	w.done = true

	header := w.Header()
	lines := header.Values("Set-Cookie")
	if len(lines) == 0 {
		return
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		c, err := http.ParseSetCookie(line)
		if err != nil || w.middleware.IsDisabled(c.Name) {
			out = append(out, line)
			continue
		}
		value, err := w.middleware.encryptCookie(c.Name, c.Value)
		if err != nil {
			continue
		}
		encrypted := *c
		encrypted.Value = value
		// The ciphertext is base64 and needs no quoting, whatever the value it
		// replaced needed.
		encrypted.Quoted = false
		if rendered := encrypted.String(); rendered != "" {
			out = append(out, rendered)
		}
	}
	header["Set-Cookie"] = out
}

func (w *encryptWriter) WriteHeader(status int) {
	w.encryptCookies()
	w.ResponseWriter.WriteHeader(status)
}

func (w *encryptWriter) Write(b []byte) (int, error) {
	w.encryptCookies()
	return w.ResponseWriter.Write(b)
}

// Unwrap gives http.ResponseController the writer underneath, so flushing and
// hijacking still reach it.
func (w *encryptWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// validCookieValue reports whether a value can travel back on a Cookie header.
// It is net/http's own rule for a cookie octet, which is what the parser on the
// far side of this rewrite will apply.
func validCookieValue(value string) bool {
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b < 0x20 || b >= 0x7f || b == '"' || b == ';' || b == '\\' {
			return false
		}
	}
	return true
}
