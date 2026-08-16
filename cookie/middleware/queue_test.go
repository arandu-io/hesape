package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/cookie"
	"github.com/arandu-io/hesape/cookie/middleware"
)

// serve runs one request through the handler and gives back the response.
func serve(h http.Handler, r *http.Request) *http.Response {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec.Result()
}

// get is a GET / with no cookies on it.
func get() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/", nil)
}

// find returns the cookie of that name off a response, or nil.
func find(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestAHandlerQueuesAndTheMiddlewareWritesIt(t *testing.T) {
	// The point of the whole component: the handler never sees the
	// ResponseWriter, and the cookie still goes out.
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
	}))

	c := find(serve(h, get()), "theme")
	if c == nil {
		t.Fatal("the queued cookie never reached the response")
	}
	if c.Value != "dark" {
		t.Errorf("Value = %q, want %q", c.Value, "dark")
	}
	if c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("the jar defaults were lost: %+v", c)
	}
}

func TestTheQueueIsPerRequestAndNotShared(t *testing.T) {
	// The process outlives a single request, so a cookie queued by one
	// visitor must not appear in the next one's response.
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	first := true
	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if first {
			j := cookie.CookieJarFrom(r.Context())
			j.Queue(j.Make("secret", "first-visitor", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
		}
	}))

	if find(serve(h, get()), "secret") == nil {
		t.Fatal("the first request did not get its own cookie")
	}
	first = false
	if c := find(serve(h, get()), "secret"); c != nil {
		t.Fatalf("the second request carried the first one's cookie: %q", c.Value)
	}
	if got := jar.GetQueuedCookies(); len(got) != 0 {
		t.Errorf("the shared jar was written to: %d cookies queued on it", len(got))
	}
}

func TestACookieQueuedOnTheSharedJarGoesOutOnEveryRequest(t *testing.T) {
	// The other half of the clone: what was queued at boot is carried into
	// every request's copy, so it is written every time and not just once.
	jar := cookie.NewCookieJar()
	jar.Queue(jar.Make("banner", "shown", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for i := range 2 {
		if find(serve(h, get()), "banner") == nil {
			t.Fatalf("request %d did not get the cookie queued on the shared jar", i+1)
		}
	}
}

func TestAQueuedCookieReplacesOneTheHandlerAlreadySet(t *testing.T) {
	// setCookie keys by domain, path and name, so the queued one wins rather
	// than joining. Two Set-Cookie lines for the same cookie leave the
	// browser to pick, and which it picks is not something to build on.
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "theme", Value: "set-by-hand", Path: "/"})
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "queued", 60, "/", "", nil, true, false, http.SameSiteDefaultMode))
	}))

	res := serve(h, get())
	var seen []string
	for _, c := range res.Cookies() {
		if c.Name == "theme" {
			seen = append(seen, c.Value)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("got %d theme cookies (%v), want the queued one to have replaced the other", len(seen), seen)
	}
	if seen[0] != "queued" {
		t.Errorf("Value = %q, want %q", seen[0], "queued")
	}
}

func TestACookieOnAnotherPathIsNotReplacedButJoined(t *testing.T) {
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "root", 60, "/", "", nil, true, false, http.SameSiteDefaultMode))
		j.Queue(j.Make("theme", "admin", 60, "/admin", "", nil, true, false, http.SameSiteDefaultMode))
	}))

	byPath := map[string]string{}
	for _, c := range serve(h, get()).Cookies() {
		if c.Name == "theme" {
			byPath[c.Path] = c.Value
		}
	}
	if byPath["/"] != "root" || byPath["/admin"] != "admin" {
		t.Fatalf("got %v, want both paths kept", byPath)
	}
}

func TestTheQueueLandsBeforeAnExplicitWriteHeader(t *testing.T) {
	// A header written after WriteHeader is a header that never goes out, so
	// the queue has to be drained at the point of no return and not when the
	// handler returns.
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("body"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get())

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if find(rec.Result(), "theme") == nil {
		t.Fatal("the cookie was queued before WriteHeader and still did not go out")
	}
}

func TestAQueueThatIsDrainedOnceIsNotDrainedAgain(t *testing.T) {
	// WriteHeader, Write and the handler returning are three ways to finish,
	// and the cookie must appear once however many of them happen.
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("a"))
		_, _ = w.Write([]byte("b"))
	}))

	res := serve(h, get())
	if n := len(res.Header.Values("Set-Cookie")); n != 1 {
		t.Fatalf("got %d Set-Cookie lines, want 1", n)
	}
}

func TestExpireQueuesTheCookieThatDeletesItInTheBrowser(t *testing.T) {
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie.CookieJarFrom(r.Context()).Expire("theme", "", "")
	}))

	c := find(serve(h, get()), "theme")
	if c == nil {
		t.Fatal("Expire did not put anything on the response")
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want it emptied", c.Value)
	}
	if c.MaxAge > 0 {
		t.Errorf("MaxAge = %d, want it in the past so the browser drops the cookie", c.MaxAge)
	}
}

func TestUnqueueTakesACookieBackBeforeTheResponseIsWritten(t *testing.T) {
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
		j.Unqueue("theme", "")
	}))

	if c := find(serve(h, get()), "theme"); c != nil {
		t.Fatalf("the cookie was unqueued and still went out with %q", c.Value)
	}
}

func TestWithoutTheMiddlewareThereIsNoJarInTheContext(t *testing.T) {
	// Nothing would write the queue, so the nil is the honest answer rather
	// than an empty jar that swallows cookies.
	var got *cookie.CookieJar
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = cookie.CookieJarFrom(r.Context())
	})

	serve(h, get())

	if got != nil {
		t.Fatal("a jar appeared in a context nothing put one in")
	}
}

func TestTheWriterUnderneathIsStillReachable(t *testing.T) {
	// http.ResponseController has to find the real writer through the wrapper,
	// or streaming breaks on every route this middleware is on.
	jar := cookie.NewCookieJar()
	queue := middleware.NewAddQueuedCookiesToResponse(jar)

	h := queue.Handle(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		j := cookie.CookieJarFrom(r.Context())
		j.Queue(j.Make("theme", "dark", 60, "", "", nil, true, false, http.SameSiteDefaultMode))
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush through the wrapper: %v", err)
		}
	}))

	if find(serve(h, get()), "theme") == nil {
		t.Fatal("flushing lost the queued cookie")
	}
}
