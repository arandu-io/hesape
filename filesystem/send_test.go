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

func TestSendWritesTheFileAndTheHeaders(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "the body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Send(rec, req, g, d, "notes.txt", filesystem.SendOptions{}); err != nil {
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

// TestSendAlwaysSendsNosniff: the stored type came from the key, and nosniff is
// what stops a browser deciding a .txt was HTML all along. It is not an option,
// because the day it is one is the day somebody turns it off.
func TestSendAlwaysSendsNosniff(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "<script>alert(1)</script>")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Send(rec, req, g, d, "notes.txt", filesystem.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

// TestSendNeverCachesInAShared cache: a shared cache holding one tenant's file
// is the same leak as a missing prefix, arriving later.
func TestSendNeverCachesInASharedCache(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "notes.txt", "x")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Send(rec, req, g, d, "notes.txt", filesystem.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "private") {
		t.Fatalf("Cache-Control = %q, want a private default", got)
	}
}

func TestSendOffersADownloadWithAName(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "2026-08/q1.pdf", "%PDF")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	opt := filesystem.SendOptions{Download: true, Filename: `relatório "final".pdf`}
	if err := filesystem.Send(rec, req, g, d, "2026-08/q1.pdf", opt); err != nil {
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

// TestSendDefaultsTheNameToTheLastSegmentOfTheKey, so a download of
// "2026-08/q1.pdf" does not land as "2026-08".
func TestSendDefaultsTheNameToTheLastSegmentOfTheKey(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "2026-08/q1.pdf", "%PDF")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	if err := filesystem.Send(rec, req, g, d, "2026-08/q1.pdf", filesystem.SendOptions{Download: true}); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "q1.pdf") {
		t.Fatalf("Content-Disposition = %q, want q1.pdf", got)
	}
}

// TestSendWritesNothingWhenItFails: the caller decides the status, because
// ErrNotFound is a 404 and a Grant refusal is a 403, and only the caller has an
// exception handler.
func TestSendWritesNothingWhenItFails(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/missing", nil)
	err := filesystem.Send(rec, req, grant(tenant), d, "missing.pdf", filesystem.SendOptions{})
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

// TestSendCannotServeAnotherTenantsFile: the route ran a Policy, and the Grant
// that Policy produced is the only thing that reaches the disk.
func TestSendCannotServeAnotherTenantsFile(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := filesystem.NewDisk("local", newMem())
	put(t, d, grant(other), "secret.pdf", "theirs")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/secret.pdf", nil)
	if err := filesystem.Send(rec, req, grant(tenant), d, "secret.pdf", filesystem.SendOptions{}); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := filesystem.Send(rec, req, auth.Grant{}, d, "secret.pdf", filesystem.SendOptions{}); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want nothing written", rec.Body.String())
	}
}

// TestSendServesARangeWhenTheDriverCanSeek: a video does not play without it,
// and the driver that can is the common one.
func TestSendServesARangeWhenTheDriverCanSeek(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "clip.bin", "0123456789")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/clip.bin", nil)
	req.Header.Set("Range", "bytes=2-5")
	if err := filesystem.Send(rec, req, g, d, "clip.bin", filesystem.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", rec.Code)
	}
	if got := rec.Body.String(); got != "2345" {
		t.Fatalf("body = %q, want the requested range", got)
	}
}

// TestSendStillWorksWhenTheDriverCannotSeek: a streaming driver gets a plain
// copy -- correct, just without resumable downloads.
func TestSendStillWorksWhenTheDriverCannotSeek(t *testing.T) {
	d := filesystem.NewDisk("stream", streamAdapter{newMem()})
	g := grant(tenant)
	put(t, d, g, "notes.txt", "the body")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/notes.txt", nil)
	if err := filesystem.Send(rec, req, g, d, "notes.txt", filesystem.SendOptions{}); err != nil {
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
	if err := filesystem.Send(rec, req, g, d, "notes.txt", filesystem.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want none on a HEAD", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Length"); got != "8" {
		t.Fatalf("Content-Length = %q, want the real size", got)
	}
}
