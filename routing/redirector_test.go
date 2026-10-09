package routing_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/routing"
)

// memorySession is the SessionStore a redirect flashes into, kept in a map so
// a test can read back what was put there.
type memorySession struct{ values map[string]any }

func newMemorySession() *memorySession { return &memorySession{values: map[string]any{}} }

func (s *memorySession) Get(key string, def ...any) any {
	if v, ok := s.values[key]; ok {
		return v
	}
	if len(def) > 0 {
		return def[0]
	}
	return nil
}

func (s *memorySession) Put(key string, value any) { s.values[key] = value }

func (s *memorySession) Pull(key string, def ...any) any {
	v := s.Get(key, def...)
	delete(s.values, key)
	return v
}

func (s *memorySession) PreviousURL() string { return "" }

// flashedInput is what a redirect left in the session for the form to come
// back filled with.
func flashedInput(t *testing.T, s *memorySession) map[string]any {
	t.Helper()
	input, ok := s.values["_old_input"].(map[string]any)
	if !ok {
		t.Fatalf("no input was flashed: %#v", s.values["_old_input"])
	}
	return input
}

// postedForm is a sign-in form as the browser sends it.
func postedForm() *http.Request {
	body := "email=ada%40example.test&password=hunter2&remember=on"
	r := httptest.NewRequest(http.MethodPost, "https://example.test/login", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// redirectorFor is a Redirector built the way a request builds one: over the
// request, with the session attached.
func redirectorFor(r *http.Request, s routing.SessionStore) *routing.Redirector {
	redirector := routing.NewRedirector(routing.NewUrlGenerator(routing.NewRoutes(), r))
	redirector.SetSession(s)
	return redirector
}

// TestOnlyInputFlashesTheNamedKeysAndNothingElse: it used to return the
// receiver and do nothing, so a sign-in form that asked to keep the address
// came back empty -- and one that relied on it to leave the password behind
// had nothing to leave behind only because nothing was kept at all.
func TestOnlyInputFlashesTheNamedKeysAndNothingElse(t *testing.T) {
	session := newMemorySession()

	redirectorFor(postedForm(), session).Back(0, nil, "/login").OnlyInput("email", "remember", "missing")

	input := flashedInput(t, session)
	if input["email"] != "ada@example.test" || input["remember"] != "on" {
		t.Errorf("flashed %v, want the email and the checkbox", input)
	}
	if _, kept := input["password"]; kept {
		t.Error("a key that was not named was flashed")
	}
	if _, kept := input["missing"]; kept {
		t.Error("a key the request did not send was flashed")
	}
	if len(input) != 2 {
		t.Errorf("flashed %d keys, want 2: %v", len(input), input)
	}
}

// TestExceptInputFlashesEverythingButTheNamedKeys: the form comes back with
// what was typed, minus what the handler said should not travel.
func TestExceptInputFlashesEverythingButTheNamedKeys(t *testing.T) {
	session := newMemorySession()

	redirectorFor(postedForm(), session).Back(0, nil, "/login").ExceptInput("password")

	input := flashedInput(t, session)
	if _, kept := input["password"]; kept {
		t.Fatal("the named key was flashed anyway")
	}
	for _, key := range []string{"email", "remember"} {
		if _, kept := input[key]; !kept {
			t.Errorf("%s was dropped, and only the password was named", key)
		}
	}
}

// TestTheKeysReachIntoNestedInputByDotPath: a JSON body nests, and a handler
// names the field it keeps the way the form names it.
func TestTheKeysReachIntoNestedInputByDotPath(t *testing.T) {
	newRequest := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "https://example.test/profile",
			strings.NewReader(`{"profile":{"name":"Ada","city":"London"},"token":"t-1"}`))
		r.Header.Set("Content-Type", "application/json")
		return r
	}

	only := newMemorySession()
	redirectorFor(newRequest(), only).Back(0, nil, "/profile").OnlyInput("profile.name")
	profile, _ := flashedInput(t, only)["profile"].(map[string]any)
	if profile["name"] != "Ada" || len(profile) != 1 || len(flashedInput(t, only)) != 1 {
		t.Errorf("OnlyInput flashed %v, want profile.name alone", flashedInput(t, only))
	}

	except := newMemorySession()
	redirectorFor(newRequest(), except).Back(0, nil, "/profile").ExceptInput("profile.city", "token")
	profile, _ = flashedInput(t, except)["profile"].(map[string]any)
	if profile["name"] != "Ada" || len(profile) != 1 || len(flashedInput(t, except)) != 1 {
		t.Errorf("ExceptInput flashed %v, want profile.name alone", flashedInput(t, except))
	}
}

// TestOnlyInputLeavesTheUploadedFilesBehind: a file is not something to put
// back in a text box, and not something to keep in a session either, even
// when the handler names its field.
func TestOnlyInputLeavesTheUploadedFilesBehind(t *testing.T) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("title", "Quarterly report")
	part, _ := form.CreateFormFile("attachment", "report.pdf")
	_, _ = part.Write([]byte("%PDF-1.7"))
	_ = form.Close()

	r := httptest.NewRequest(http.MethodPost, "https://example.test/reports", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	session := newMemorySession()

	redirectorFor(r, session).Back(0, nil, "/reports/new").OnlyInput("title", "attachment")

	input := flashedInput(t, session)
	if input["title"] != "Quarterly report" {
		t.Errorf("title = %v, want the text field", input["title"])
	}
	if _, kept := input["attachment"]; kept {
		t.Error("the uploaded file was flashed into the session")
	}
}

// TestARedirectFromTheRedirectorFlashesIntoItsSession: With and WithInput
// flashed into Redirect.Session, which the Redirector never filled, so
// everything a handler attached to a redirect it built was dropped.
func TestARedirectFromTheRedirectorFlashesIntoItsSession(t *testing.T) {
	session := newMemorySession()

	redirectorFor(postedForm(), session).To("/dashboard", 0, nil, nil).With("status", "Signed in.")

	if session.values["status"] != "Signed in." {
		t.Fatalf("session = %v, want the flashed status", session.values)
	}
}

// TestARedirectWithoutARequestOrASessionFlashesNothing: a Redirect written as a
// literal has no request to read input from, and one with no session has
// nowhere to put it. Both are a no-op rather than a panic.
func TestARedirectWithoutARequestOrASessionFlashesNothing(t *testing.T) {
	session := newMemorySession()
	literal := &routing.Redirect{Path: "/login", Status: http.StatusFound, Session: session}
	literal.OnlyInput("email").ExceptInput("password")
	if _, flashed := session.values["_old_input"]; flashed {
		t.Error("a literal with no request flashed input it could not have read")
	}

	unattached := routing.NewRedirector(routing.NewUrlGenerator(routing.NewRoutes(), postedForm()))
	redirect := unattached.Back(0, nil, "/login")
	if got := redirect.OnlyInput("email").ExceptInput("password"); got != redirect {
		t.Error("the methods must return the receiver, so calls chain")
	}
}
