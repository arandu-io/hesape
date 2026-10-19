package session_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/arandu-io/hesape/session"
)

func TestAMessageSurvivesExactlyOneRequest(t *testing.T) {
	f := testFlash()

	rec := httptest.NewRecorder()
	f.WriteMessages(rec, map[string]string{"status": "Perfil salvo."})
	cookie := written(rec)
	if cookie == nil {
		t.Fatal("WriteMessages set no cookie")
	}

	first := httptest.NewRecorder()
	got, ok := f.Read(first, pageRead(cookie))
	if !ok {
		t.Fatal("the first read got nothing")
	}
	if got.Messages["status"] != "Perfil salvo." {
		t.Errorf("Messages = %v", got.Messages)
	}
	if len(got.Errors) != 0 || len(got.Old) != 0 {
		t.Errorf("a message came back with errors %v or input %v", got.Errors, got.Old)
	}
	if !cleared(first) {
		t.Error("the read did not tell the browser to drop the cookie, so a reload shows the message again")
	}
	if _, ok := f.Read(httptest.NewRecorder(), pageRead()); ok {
		t.Error("a request carrying no flash cookie produced one anyway")
	}
}

func TestAMessageIsNotSpentByAFragment(t *testing.T) {
	f := testFlash()
	rec := httptest.NewRecorder()
	f.WriteMessages(rec, map[string]string{"status": "Saved."})
	cookie := written(rec)

	fragment := httptest.NewRequest(http.MethodGet, "/profile", nil)
	fragment.Header.Set("Accept", "*/*")
	fragment.AddCookie(cookie)
	swap := httptest.NewRecorder()
	if _, ok := f.Read(swap, fragment); ok {
		t.Fatal("a request that is not a page read spent the message")
	}
	if cleared(swap) {
		t.Fatal("a request that is not a page read cleared the cookie")
	}
	if got, ok := f.Read(httptest.NewRecorder(), pageRead(cookie)); !ok || got.Messages["status"] != "Saved." {
		t.Errorf("the page after the fragment read %v, %v", got.Messages, ok)
	}
}

func TestTakeSpendsACookieThatCarriedOnlyMessages(t *testing.T) {
	f := testFlash()
	rec := httptest.NewRecorder()
	f.WriteMessages(rec, map[string]string{"status": "Saved."})

	read := httptest.NewRecorder()
	if _, _, ok := f.Take(read, pageRead(written(rec))); ok {
		t.Error("Take answered ok for a cookie with no errors and no input")
	}
	if !cleared(read) {
		t.Error("Take left a spent cookie in the browser")
	}
}

func TestASecondWriteReplacesTheFirstOnTheSameResponse(t *testing.T) {
	f := testFlash()
	rec := httptest.NewRecorder()
	f.WriteMessages(rec, map[string]string{"status": "first"})
	f.WriteMessages(rec, map[string]string{"status": "second", "notice": "also"})

	var flashes int
	for _, line := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(line, session.FlashCookieName+"=") {
			flashes++
		}
	}
	if flashes != 1 {
		t.Fatalf("the response sets the flash %d times, want once", flashes)
	}
	got, _ := f.Read(httptest.NewRecorder(), pageRead(written(rec)))
	if got.Messages["status"] != "second" || got.Messages["notice"] != "also" {
		t.Errorf("Messages = %v", got.Messages)
	}

	// A rejection on the same response is the answer, and replaces the report.
	f.Write(rec, map[string][]string{"email": {"is required"}}, url.Values{"email": {""}})
	got, _ = f.Read(httptest.NewRecorder(), pageRead(written(rec)))
	if len(got.Messages) != 0 || got.Errors["email"][0] != "is required" {
		t.Errorf("after Write: messages %v, errors %v", got.Messages, got.Errors)
	}
}

func TestNothingIsWrittenForEmptyMessages(t *testing.T) {
	f := testFlash()
	rec := httptest.NewRecorder()
	f.WriteMessages(rec, map[string]string{"status": "", "": "keyless"})
	if written(rec) != nil {
		t.Error("an empty message set a cookie")
	}
}

func TestOversizeMessagesKeepTheFirstKeysAndWholeCharacters(t *testing.T) {
	f := testFlash()
	long := strings.Repeat("é", 600)
	messages := map[string]string{}
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		messages[key] = long
	}
	rec := httptest.NewRecorder()
	f.WriteMessages(rec, messages)
	cookie := written(rec)
	if cookie == nil {
		t.Fatal("no cookie")
	}
	if len(cookie.Value) > session.MaxFlashBytes {
		t.Fatalf("the cookie is %d bytes, over the %d the browser keeps", len(cookie.Value), session.MaxFlashBytes)
	}
	got, ok := f.Read(httptest.NewRecorder(), pageRead(cookie))
	if !ok {
		t.Fatal("nothing came back")
	}
	if _, kept := got.Messages["a"]; !kept {
		t.Errorf("the first key in sorted order was dropped: %v", got.Messages)
	}
	if _, kept := got.Messages["j"]; kept {
		t.Error("the last key in sorted order survived a trim")
	}
	if !utf8.ValidString(got.Messages["a"]) {
		t.Error("a cut message ends in half a character")
	}
}
