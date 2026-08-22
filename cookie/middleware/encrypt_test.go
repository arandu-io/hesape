package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/cookie"
	"github.com/arandu-io/hesape/cookie/middleware"
	"github.com/arandu-io/hesape/encryption"
)

// newEncrypter builds the encrypter the middleware is constructed with. The
// key is fixed so that a failure is reproducible.
func newEncrypter(t *testing.T, key string) *encryption.Encrypter {
	t.Helper()

	e, err := encryption.NewEncrypter([]byte(key), encryption.AES256GCM)
	if err != nil {
		t.Fatalf("NewEncrypter: %v", err)
	}
	return e
}

const (
	key      = "0123456789abcdef0123456789abcdef"
	otherKey = "fedcba9876543210fedcba9876543210"
)

// readBack sends the cookies a response set back on a new request, which is
// what the browser does and the only way to test the two halves together.
func readBack(res *http.Response) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range res.Cookies() {
		r.AddCookie(c)
	}
	return r
}

func TestACookieGoesOutEncryptedAndComesBackPlain(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	var seen string
	h := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("theme"); err == nil {
			seen = c.Value
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
	}))

	res := serve(h, get())
	c := find(res, "theme")
	if c == nil {
		t.Fatal("no cookie on the response")
	}
	if c.Value == "dark" {
		t.Fatal("the value went to the browser in the clear")
	}
	if !encryption.AppearsEncrypted(c.Value) {
		t.Fatalf("the value is not one of this framework's payloads: %q", c.Value)
	}

	serve(h, readBack(res))
	if seen != "dark" {
		t.Fatalf("the handler read %q on the way back, want %q", seen, "dark")
	}
}

func TestACookieRenamedIsNotReadAsTheOtherOne(t *testing.T) {
	// The attack the value prefix exists to stop, end to end. The attacker
	// holds a ciphertext this application issued -- their own -- and sends it
	// back under another cookie's name. It decrypts, so decryption cannot
	// catch it; the name sealed inside it is what catches it.
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	issue := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "attacker_pref", Value: "1", Path: "/"})
	}))
	stolen := find(serve(issue, get()), "attacker_pref")
	if stolen == nil {
		t.Fatal("nothing was issued to steal")
	}

	var got string
	var err error
	read := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c *http.Cookie
		c, err = r.Cookie("app_session")
		if err == nil {
			got = c.Value
		}
	}))

	replay := httptest.NewRequest(http.MethodGet, "/", nil)
	replay.AddCookie(&http.Cookie{Name: "app_session", Value: stolen.Value})
	serve(read, replay)

	if !errors.Is(err, http.ErrNoCookie) {
		t.Fatalf("a value issued for attacker_pref was accepted as app_session and read as %q", got)
	}
}

func TestAValueThatDoesNotDecryptLeavesTheRequestWithoutThatCookie(t *testing.T) {
	// Dropping the cookie is the answer that cannot be mistaken for a cookie
	// whose value really is "".
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	var err error
	h := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err = r.Cookie("theme")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "theme", Value: "not-a-payload"})
	serve(h, r)

	if !errors.Is(err, http.ErrNoCookie) {
		t.Fatalf("r.Cookie returned %v, want ErrNoCookie for a value that did not decrypt", err)
	}
}

func TestAValueEncryptedUnderAnotherApplicationsKeyIsDropped(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	theirs := middleware.NewEncryptCookies(newEncrypter(t, otherKey))
	issued := find(serve(theirs.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
	})), get()), "theme")

	ours := middleware.NewEncryptCookies(newEncrypter(t, key))
	var err error
	serve(ours.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err = r.Cookie("theme")
	})), readBack(&http.Response{Header: http.Header{"Set-Cookie": {issued.String()}}}))

	if !errors.Is(err, http.ErrNoCookie) {
		t.Fatalf("r.Cookie returned %v, want ErrNoCookie for a foreign key", err)
	}
}

func TestAValueWrittenUnderAPreviousKeyIsStillRead(t *testing.T) {
	// The key was rotated and the browser still holds what the old key wrote.
	// GetAllKeys returns every key Validate walks, so the prefix has to match
	// under the old key too.
	t.Cleanup(middleware.FlushState)

	old := middleware.NewEncryptCookies(newEncrypter(t, otherKey))
	issued := find(serve(old.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
	})), get()), "theme")

	rotated, err := newEncrypter(t, key).PreviousKeys([][]byte{[]byte(otherKey)})
	if err != nil {
		t.Fatalf("PreviousKeys: %v", err)
	}

	var seen string
	serve(middleware.NewEncryptCookies(rotated).Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("theme"); err == nil {
			seen = c.Value
		}
	})), readBack(&http.Response{Header: http.Header{"Set-Cookie": {issued.String()}}}))

	if seen != "dark" {
		t.Fatalf("read %q after the rotation, want %q", seen, "dark")
	}
}

func TestDisableForLeavesACookieInTheClearBothWays(t *testing.T) {
	// The cookie that needs this is the CSRF token: script on the page has to
	// read it to put it in a header, and it cannot decrypt.
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	encrypt.DisableFor("XSRF-TOKEN")

	var seen string
	h := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("XSRF-TOKEN"); err == nil {
			seen = c.Value
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "XSRF-TOKEN", Value: "token-value", Path: "/"})
	}))

	res := serve(h, get())
	if c := find(res, "XSRF-TOKEN"); c == nil || c.Value != "token-value" {
		t.Fatalf("the excepted cookie was encrypted on the way out: %+v", c)
	}
	serve(h, readBack(res))
	if seen != "token-value" {
		t.Fatalf("the excepted cookie read back as %q, want %q", seen, "token-value")
	}
}

func TestDisableForTakesSeveralNamesAndAddsUpAcrossCalls(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	encrypt.DisableFor("one", "two")
	encrypt.DisableFor("three")

	for _, name := range []string{"one", "two", "three"} {
		if !encrypt.IsDisabled(name) {
			t.Errorf("IsDisabled(%q) = false, want true", name)
		}
	}
	if encrypt.IsDisabled("four") {
		t.Error("IsDisabled(\"four\") = true, and it was never listed")
	}
}

func TestDisableForIsPerInstanceAndExceptIsGlobal(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	mine := middleware.NewEncryptCookies(newEncrypter(t, key))
	yours := middleware.NewEncryptCookies(newEncrypter(t, key))
	mine.DisableFor("mine")
	middleware.Except("everyones")

	if yours.IsDisabled("mine") {
		t.Error("disableFor on one instance reached another")
	}
	if !mine.IsDisabled("everyones") || !yours.IsDisabled("everyones") {
		t.Error("except() did not reach every instance, and it is static in the PHP")
	}
}

func TestExceptDropsDuplicatesAndFlushStatePutsItBack(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	middleware.Except("one")
	middleware.Except("one", "two")

	if !encrypt.IsDisabled("one") || !encrypt.IsDisabled("two") {
		t.Fatal("except() did not register the names")
	}
	middleware.FlushState()
	if encrypt.IsDisabled("one") || encrypt.IsDisabled("two") {
		t.Error("flushState() left the global list behind")
	}
	if middleware.Serialized("anything") {
		t.Error("serialized() is true after flushState(), and $serialize is false")
	}
}

func TestSerializedIsFalseAndIgnoresTheName(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	// The flag is per application, not per cookie: Serialized takes a name it
	// never reads.
	if middleware.Serialized("one") || middleware.Serialized("two") {
		t.Error("serialized() = true, and no released method sets $serialize")
	}
}

func TestTheCookieAttributesSurviveEncryption(t *testing.T) {
	// Only the value is replaced. A cookie that came back without HttpOnly or
	// without its path is a session cookie the browser sends to the wrong
	// place, and nothing in the value would say so.
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	h := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{
			Name:     "theme",
			Value:    "dark",
			Path:     "/admin",
			Domain:   "example.test",
			MaxAge:   3600,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}))

	c := find(serve(h, get()), "theme")
	if c == nil {
		t.Fatal("no cookie on the response")
	}
	if c.Path != "/admin" || c.Domain != "example.test" || c.MaxAge != 3600 {
		t.Errorf("path, domain or max-age changed: %+v", c)
	}
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("a security attribute was dropped: %+v", c)
	}
}

func TestAQueuedCookieIsEncryptedToo(t *testing.T) {
	// EncryptCookies goes outside AddQueuedCookiesToResponse, so what the jar
	// queues is encrypted on the way past. If the order were the other way
	// round the queue would go out in the clear, which is the mistake this
	// test is here to catch.
	t.Cleanup(middleware.FlushState)

	jar := cookie.NewCookieJar()
	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	var seen string
	h := encrypt.Handle(queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("theme"); err == nil {
			seen = c.Value
			return
		}
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
	})))

	res := serve(h, get())
	c := find(res, "theme")
	if c == nil {
		t.Fatal("the queued cookie never reached the response")
	}
	if c.Value == "dark" {
		t.Fatal("a queued cookie went out in the clear")
	}
	serve(h, readBack(res))
	if seen != "dark" {
		t.Fatalf("the queued cookie read back as %q, want %q", seen, "dark")
	}
}

func TestAQueuedCookieIsEncryptedAcrossAFlushToo(t *testing.T) {
	// A flush commits the header. If it walked past the two wrappers the
	// cookie would go to the browser in the clear, which is worse than losing
	// it.
	t.Cleanup(middleware.FlushState)

	jar := cookie.NewCookieJar()
	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := encrypt.Handle(queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush: %v", err)
		}
	})))

	c := find(serve(h, get()), "theme")
	if c == nil {
		t.Fatal("the flush lost the queued cookie")
	}
	if c.Value == "dark" {
		t.Fatal("the flush sent the cookie past the encrypter in the clear")
	}
}

func TestARequestWithNoCookiesIsHandedOnUntouched(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	var count int
	serve(encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count = len(r.Cookies())
	})), get())

	if count != 0 {
		t.Fatalf("got %d cookies on a request that carried none", count)
	}
}

func TestAResponseWithNoCookiesGetsNoSetCookieHeader(t *testing.T) {
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	res := serve(encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("body"))
	})), get())

	if n := len(res.Header.Values("Set-Cookie")); n != 0 {
		t.Fatalf("got %d Set-Cookie lines on a response that set none", n)
	}
}

func TestAnEmptyValueSurvivesTheRoundTrip(t *testing.T) {
	// "" is a legal cookie value, and it is what the prefix alone decrypts to.
	// It must not be confused with the cookie having failed to validate.
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	var seen string
	var err error
	h := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var c *http.Cookie
		if c, err = r.Cookie("empty"); err == nil {
			seen = c.Value
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "empty", Value: "", Path: "/"})
	}))

	serve(h, readBack(serve(h, get())))

	if err != nil {
		t.Fatalf("a cookie whose value is \"\" was dropped: %v", err)
	}
	if seen != "" {
		t.Fatalf("read %q, want the empty string", seen)
	}
}

func TestOnlyTheNamedCookieIsDecryptedAndTheRestStillArrive(t *testing.T) {
	// A request carries an encrypted cookie and one that was never encrypted
	// because it is excepted. Rewriting the header must not lose either.
	t.Cleanup(middleware.FlushState)

	encrypt := middleware.NewEncryptCookies(newEncrypter(t, key))
	encrypt.DisableFor("plain")

	issue := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "secret", Value: "shh", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "plain", Value: "visible", Path: "/"})
	}))

	got := map[string]string{}
	read := encrypt.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range r.Cookies() {
			got[c.Name] = c.Value
		}
	}))
	serve(read, readBack(serve(issue, get())))

	if got["secret"] != "shh" || got["plain"] != "visible" {
		t.Fatalf("got %v, want secret=shh and plain=visible", got)
	}
}
