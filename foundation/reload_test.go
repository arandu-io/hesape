package foundation

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	// What Boot fills in from the module that brought it. Fixed here so these
	// tests are about the injection and not about the asset pipeline.
	reloadTag = []byte(`<script src="/_arandu/assets/abc/arandu-reload.js" defer></script>`)
	t.Cleanup(func() { reloadTag = nil })
	rec := httptest.NewRecorder()
	liveReload(handler).ServeHTTP(rec, req)
	return rec
}

// A whole document gets the script, once, inside the body.
func TestADocumentIsGivenTheReloadScript(t *testing.T) {
	rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body><h1>Hello</h1></body></html>"))
	}, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	if n := strings.Count(body, "arandu-reload.js"); n != 1 {
		t.Fatalf("the page carries %d listeners, want 1:\n%s", n, body)
	}
	if !strings.Contains(body, "<h1>Hello</h1>") {
		t.Error("injecting the script lost the page")
	}
	if strings.Index(body, "arandu-reload.js") > strings.Index(body, "</body>") {
		t.Error("the script went outside the body")
	}
}

// The tag is a script and a request to this origin, so a response whose policy
// blocks either one is left as it is: the browser would refuse the script and
// report a violation on a page that did nothing wrong.
func TestAPageWhosePolicyBlocksTheScriptIsLeftAlone(t *testing.T) {
	page := "<html><body>no</body></html>"
	for name, policy := range map[string]string{
		"nothing is allowed":                "default-src 'none'; style-src 'sha256-x'; img-src data:",
		"script-src without this origin":    "default-src 'self'; script-src https://cdn.example.test",
		"script-src none":                   "default-src 'self'; script-src 'none'",
		"script-src-elem without it":        "script-src 'self'; script-src-elem 'none'",
		"connect-src without it":            "default-src 'self'; connect-src https://api.example.test",
		"a second policy that blocks":       "default-src 'self', default-src 'none'",
		"default-src without this origin":   "default-src https://cdn.example.test",
		"a directive name in capitals":      "DEFAULT-SRC 'none'",
		"only the first script-src is read": "script-src 'none'; script-src 'self'",
	} {
		rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Security-Policy", policy)
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(page))
		}, httptest.NewRequest(http.MethodGet, "/notes/1", nil))

		if rec.Code != http.StatusForbidden || rec.Body.String() != page {
			t.Errorf("%s: the page was changed (%d) %q", name, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Content-Security-Policy"); got != policy {
			t.Errorf("%s: the policy was rewritten to %q", name, got)
		}
	}
}

// A policy that admits this origin gets the script, and so does a page with no
// policy: the tag is for development, and a project that never wrote a policy
// is no reason to lose it.
func TestAPageWhosePolicyAllowsTheScriptIsGivenIt(t *testing.T) {
	for name, policy := range map[string]string{
		"none":                         "",
		"the default policy":           "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'",
		"default-src alone":            "default-src 'self'",
		"script-src with this origin":  "default-src 'none'; script-src 'self'; connect-src 'self'",
		"script-src-elem over script":  "script-src 'none'; script-src-elem 'self'; connect-src 'self'",
		"a policy that restricts only": "img-src 'self'",
		"a wildcard":                   "default-src *",
	} {
		rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if policy != "" {
				w.Header().Set("Content-Security-Policy", policy)
			}
			_, _ = w.Write([]byte("<html><body>ok</body></html>"))
		}, httptest.NewRequest(http.MethodGet, "/", nil))

		if strings.Count(rec.Body.String(), "arandu-reload.js") != 1 {
			t.Errorf("%s: the page was not given the script: %q", name, rec.Body.String())
		}
	}
}

// What the browser enforces is what the response carries when it leaves, so a
// handler that replaces the policy after the middleware set another decides.
func TestThePolicyThatIsReadIsTheOneTheResponseLeavesWith(t *testing.T) {
	rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; connect-src 'self'")
		// The error page replaces the application's policy with its own.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'sha256-x'")
		_, _ = w.Write([]byte("<html><body>403</body></html>"))
	}, httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Contains(rec.Body.String(), "arandu-reload.js") {
		t.Fatalf("the script was injected under the policy that replaced the application's:\n%s", rec.Body.String())
	}
}

// The one that matters most. A YPY swap replaces a fragment, and a fragment
// carrying the script would open one connection per interaction -- until the
// browser's six-per-origin limit is reached and the page stops working, which
// looks like anything except a dev-server feature.
func TestAYPYFragmentIsLeftAlone(t *testing.T) {
	rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<li id="comment-4">Said something</li>`))
	}, httptest.NewRequest(http.MethodGet, "/posts/1/comments", nil))

	if strings.Contains(rec.Body.String(), "arandu-reload.js") {
		t.Fatalf("a fragment was given a listener; every swap would add another:\n%s", rec.Body.String())
	}
	if rec.Body.String() != `<li id="comment-4">Said something</li>` {
		t.Error("the fragment was altered")
	}
}

// Anything that is not HTML is written straight through, never buffered.
func TestANonDocumentResponseIsUntouched(t *testing.T) {
	for _, tc := range []struct{ contentType, body string }{
		{"application/json", `{"ok":true}`},
		{"text/css", "body{color:red}"},
		{"image/svg+xml", "<svg><body></body></svg>"},
	} {
		rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.contentType)
			_, _ = w.Write([]byte(tc.body))
		}, httptest.NewRequest(http.MethodGet, "/x", nil))

		if rec.Body.String() != tc.body {
			t.Errorf("%s was rewritten: %q", tc.contentType, rec.Body.String())
		}
	}
}

// A rewritten body needs a rewritten length, or the page is truncated at
// exactly the point the script was added.
func TestTheLengthFollowsTheBody(t *testing.T) {
	const page = "<html><body>x</body></html>"
	rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", strconv.Itoa(len(page)))
		_, _ = w.Write([]byte(page))
	}, httptest.NewRequest(http.MethodGet, "/", nil))

	declared, err := strconv.Atoi(rec.Header().Get("Content-Length"))
	if err != nil {
		t.Fatalf("no length on the response: %v", err)
	}
	if declared != rec.Body.Len() {
		t.Fatalf("declared %d bytes and wrote %d: the page is truncated where the script begins",
			declared, rec.Body.Len())
	}
}

// The status a handler chose survives the buffering.
func TestTheStatusIsPreserved(t *testing.T) {
	rec := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("<html><body>no</body></html>"))
	}, httptest.NewRequest(http.MethodGet, "/auth/register", nil))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: a rejected form would look accepted", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "arandu-reload.js") {
		t.Error("a 422 page does not follow the restart, and it is the one people iterate on")
	}
}

// A handler that flushes is streaming, and a streamed response must not be held
// until it ends -- which for a stream is never.
func TestAStreamIsNotHeld(t *testing.T) {
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		defer close(done)
		liveReload(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body>first"))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("</body></html>"))
		})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	<-done

	if !strings.Contains(rec.Body.String(), "first") {
		t.Error("what was flushed never reached the client")
	}
	if strings.Contains(rec.Body.String(), "arandu-reload.js") {
		t.Error("a streaming response was rewritten, which means it was buffered first")
	}
}

// Two processes must not claim the same identity, or a page loaded by one never
// reloads when the other answers -- the single failure this feature cannot
// tolerate, because it is silent.
func TestEveryProcessHasItsOwnIdentity(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newBootID()
		if id == "arandu" {
			t.Fatal("the entropy source failed, so every process would claim one identity")
		}
		if seen[id] {
			t.Fatalf("two of a hundred boot ids collided (%s): a restart would go unnoticed", id)
		}
		seen[id] = true
	}
}

// Nothing is mounted or injected outside development.
func TestProductionHasNoneOfThis(t *testing.T) {
	if devReload(false) != nil {
		t.Fatal("the injection middleware is in the production pipeline")
	}
	if len(devReload(true)) != 1 {
		t.Fatal("development did not get the middleware")
	}
}

// TestAskingWhichProcessAnsweredDoesNotHoldTheConnection.
//
// This is the whole reason the endpoint stopped being an EventSource. A browser
// allows six connections per origin over HTTP/1.1 and a held stream spends one
// -- and on navigation the browser abandons the stream of the page it is leaving
// without closing it, so the server keeps the slot until its next write fails.
//
// Six quick navigations therefore left six dead streams holding every slot, and
// the seventh click did nothing at all. Reported as "I navigate, I go back, I
// navigate, and it freezes", and that is exactly what it was. Shutdown waiting
// its full timeout for the same request was the second symptom of the one
// defect.
func TestAskingWhichProcessAnsweredDoesNotHoldTheConnection(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleReload(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, reloadPath, nil))
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the handler is holding the connection, which is what froze the browser after a handful of clicks")
	}
}

// The answer names this process and nothing else, and no cache may keep it: a
// cached answer is the previous process answering forever, so the page would
// never learn that the code changed.
func TestTheAnswerIsThisProcessAndIsNeverCached(t *testing.T) {
	rec := httptest.NewRecorder()
	handleReload(rec, httptest.NewRequest(http.MethodGet, reloadPath, nil))

	if got := strings.TrimSpace(rec.Body.String()); got != bootID {
		t.Fatalf("the answer is %q, want the boot id %q", got, bootID)
	}
	if store := rec.Header().Get("Cache-Control"); !strings.Contains(store, "no-store") {
		t.Errorf("Cache-Control is %q: a cached answer is the old process answering forever", store)
	}
}
