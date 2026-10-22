package filesystem_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

func TestServeWritesTheFileAndTheHeaders(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "the body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Serve(rec, req, g, d, "notes.txt", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if body := rec.Body.String(); body != "the body" {
		t.Fatalf("body = %q", body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Fatalf("Content-Disposition = %q, want inline by default", got)
	}
}

// TestServeAlwaysSendsNosniff: the stored type came from the key, and nosniff is
// what stops a browser deciding a .txt was HTML all along. It is not an option,
// because the day it is one is the day somebody turns it off.
func TestServeAlwaysSendsNosniff(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "<script>alert(1)</script>")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Serve(rec, req, g, d, "notes.txt", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

// TestServeNeverCachesInAShared cache: a shared cache holding one tenant's file
// is the same leak as a missing prefix, arriving later.
func TestServeNeverCachesInASharedCache(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "x")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Serve(rec, req, g, d, "notes.txt", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "private") {
		t.Fatalf("Cache-Control = %q, want a private default", got)
	}
}

func TestServeOffersADownloadWithAName(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "2026-08/q1.pdf", "%PDF")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	opt := filesystem.ServeOptions{Download: true, Filename: `relatório "final".pdf`}
	if err := filesystem.Serve(rec, req, g, d, "2026-08/q1.pdf", opt); err != nil {
		t.Fatal(err)
	}

	got := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(got, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
	// A filename with a quote and a semicolon in it is a header injection with
	// a friendly name, so the value is escaped rather than concatenated.
	if strings.Contains(got, "\n") || strings.Contains(got, `"final".pdf"`) {
		t.Fatalf("Content-Disposition = %q, want the name escaped", got)
	}
}

// TestServeDefaultsTheNameToTheLastSegmentOfTheKey, so a download of
// "2026-08/q1.pdf" does not land as "2026-08".
func TestServeDefaultsTheNameToTheLastSegmentOfTheKey(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "2026-08/q1.pdf", "%PDF")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	if err := filesystem.Serve(rec, req, g, d, "2026-08/q1.pdf", filesystem.ServeOptions{Download: true}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "q1.pdf") {
		t.Fatalf("Content-Disposition = %q, want q1.pdf", got)
	}
}

// TestServeWritesNothingWhenItFails: the caller decides the status, because
// ErrNotFound is a 404 and a Grant refusal is a 403, and only the caller has an
// exception handler.
func TestServeWritesNothingWhenItFails(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/missing", nil)
	err := filesystem.Serve(rec, req, grant(tenant), d, "missing.pdf", filesystem.ServeOptions{})
	if !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want nothing written", rec.Body.String())
	}
	if len(rec.Header()) != 0 {
		t.Fatalf("headers = %v, want none", rec.Header())
	}
}

// TestServeCannotServeAnotherTenantsFile: the route ran a Policy, and the Grant
// that Policy produced is the only thing that reaches the disk.
func TestServeCannotServeAnotherTenantsFile(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := filesystem.NewDisk("local", newMem())
	put(t, d, grant(other), "secret.pdf", "theirs")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/secret.pdf", nil)
	if err := filesystem.Serve(rec, req, grant(tenant), d, "secret.pdf", filesystem.ServeOptions{}); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := filesystem.Serve(rec, req, auth.Grant{}, d, "secret.pdf", filesystem.ServeOptions{}); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want nothing written", rec.Body.String())
	}
}

// TestServeServesARangeWhenTheDriverCanSeek: a video does not play without it,
// and the driver that can is the common one.
func TestServeServesARangeWhenTheDriverCanSeek(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "clip.bin", "0123456789")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/clip.bin", nil)
	req.Header.Set("Range", "bytes=2-5")
	if err := filesystem.Serve(rec, req, g, d, "clip.bin", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Fatalf("body = %q, want the requested range", got)
	}
}

// TestServeStillWorksWhenTheDriverCannotSeek: a streaming driver gets a plain
// copy -- correct, just without resumable downloads.
func TestServeStillWorksWhenTheDriverCannotSeek(t *testing.T) {
	d := filesystem.NewDisk("stream", streamAdapter{newMem()})
	g := grant(tenant)
	put(t, d, g, "notes.txt", "the body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Serve(rec, req, g, d, "notes.txt", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Body.String(); got != "the body" {
		t.Fatalf("body = %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "8" {
		t.Fatalf("Content-Length = %q, want 8", got)
	}
}

// TestAHeadRequestCarriesNoBody, on the driver that cannot seek -- ServeContent
// handles the other one.
func TestAHeadRequestCarriesNoBody(t *testing.T) {
	d := filesystem.NewDisk("stream", streamAdapter{newMem()})
	g := grant(tenant)
	put(t, d, g, "notes.txt", "the body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/files/notes.txt", nil)
	if err := filesystem.Serve(rec, req, g, d, "notes.txt", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want none on a HEAD", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Length"); got != "8" {
		t.Fatalf("Content-Length = %q, want the real size", got)
	}
}

// TestServeSandboxesWhatItRenders: a stored file is a string somebody
// uploaded, and the response carried nothing that stopped a browser running
// what was in it on the application's origin. The policy is sent on every
// response, and a caller's header cannot replace it.
func TestServeSandboxesWhatItRenders(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "the body")

	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Security-Policy", "default-src *")
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Serve(rec, req, g, d, "notes.txt", filesystem.ServeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Result().Header.Values("Content-Security-Policy"); len(got) != 1 || got[0] != "default-src 'none'; sandbox" {
		t.Fatalf("Content-Security-Policy = %q, want exactly \"default-src 'none'; sandbox\"", got)
	}
}

// TestServeSendsActiveContentAsAnAttachment: an SVG with a script element in it
// passes the upload check, and Serve rendered it inline by default -- stored
// XSS on the application's origin. SVG, HTML and XML go out as attachments
// unless the caller opts in.
func TestServeSendsActiveContentAsAnAttachment(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	for _, key := range []string{"logo.svg", "page.html", "page.htm", "feed.xml", "page.xhtml"} {
		put(t, d, g, key, `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/files/"+key, nil)
		if err := filesystem.Serve(rec, req, g, d, key, filesystem.ServeOptions{}); err != nil {
			t.Fatal(err)
		}
		if got := rec.Result().Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") {
			t.Errorf("%s was served with Content-Disposition %q, want an attachment", key, got)
		}

		rec = httptest.NewRecorder()
		if err := filesystem.Serve(rec, req, g, d, key, filesystem.ServeOptions{InlineActiveContent: true}); err != nil {
			t.Fatal(err)
		}
		res := rec.Result()
		if got := res.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
			t.Errorf("%s with InlineActiveContent was served with Content-Disposition %q, want inline", key, got)
		}
		if got := res.Header.Get("Content-Security-Policy"); got != "default-src 'none'; sandbox" {
			t.Errorf("%s rendered inline without the sandbox: %q", key, got)
		}
	}
}
