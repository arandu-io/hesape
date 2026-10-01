package providers_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestAFormPostCallbackIsReadFromTheBody: a provider answering with
// response_mode=form_post posts the code and the state as a url-encoded body.
func TestAFormPostCallbackIsReadFromTheBody(t *testing.T) {
	store := &fakeStore{state: "bar"}
	client := answering(http.StatusOK, "access_token=token")
	p := provider(store).SetHTTPClient(client).RedirectURL("https://current.test/callback")

	r := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader("state=bar&code=blah"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := p.GetAccessToken(r); err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	sent, _ := url.ParseQuery(client.bodies[0])
	if got := sent.Get("code"); got != "blah" {
		t.Fatalf("code = %q, want the one the body carried", got)
	}
}

type copiedKey struct{}

// TestAMultipartCallbackLeavesNoTemporaryFile: the callback was read with
// FormValue, which parses a multipart body outside the path that removes its
// temporary files, so a large part posted at the callback stayed on disk.
func TestAMultipartCallbackLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	store := &fakeStore{state: "bar"}
	client := answering(http.StatusOK, "access_token=token")
	p := provider(store).SetHTTPClient(client).RedirectURL("https://current.test/callback")

	var exchangeErr error
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, exchangeErr = p.GetAccessToken(r.WithContext(context.WithValue(r.Context(), copiedKey{}, true)))
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Start()
	defer srv.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("state", "bar")
	_ = writer.WriteField("code", "blah")
	part, err := writer.CreateFormFile("padding", "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(bytes.Repeat([]byte{0}, 40<<20))
	_ = writer.Close()

	resp, err := http.Post(srv.URL, writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if exchangeErr != nil {
		t.Fatalf("GetAccessToken: %v", exchangeErr)
	}

	var left []string
	for deadline := time.Now().Add(3 * time.Second); ; {
		left = left[:0]
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "multipart-") {
				left = append(left, entry.Name())
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(left) > 0 {
		t.Fatalf("%d multipart temporary file(s) outlived the request: %v", len(left), left)
	}
}
