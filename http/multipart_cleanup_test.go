package http_test

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
)

type copiedKey struct{}

// uploadThroughACopy serves handler behind a middleware that hands it a copy
// of the request, the way every middleware that adds a context value does,
// posts a file larger than the in-memory limit of a multipart parse, and
// returns the temporary directory the parse wrote into.
func uploadThroughACopy(t *testing.T, handler func(w stdhttp.ResponseWriter, r *stdhttp.Request)) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	srv := httptest.NewUnstartedServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		handler(w, r.WithContext(context.WithValue(r.Context(), copiedKey{}, true)))
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.Start()
	defer srv.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("role", "user")
	part, err := writer.CreateFormFile("avatar", "a.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(bytes.Repeat([]byte{0}, 40<<20))
	_ = writer.Close()

	resp, err := stdhttp.Post(srv.URL, writer.FormDataContentType(), &body)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return dir
}

// assertNoMultipartFileRemains waits briefly for the removal the end of the
// request triggers, then fails naming every multipart temporary file still on
// disk.
func assertNoMultipartFileRemains(t *testing.T, dir string) {
	t.Helper()
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

var avatarRules = validation.MustCompile(validation.Rules{"role": "required"})

// TestAnUploadParsedOnACopyIsRemoved: net/http removes the temporary files of
// the form parsed on the request it handed the handler, and only that one. A
// form parsed on a copy -- which is what every handler behind a middleware
// receives -- left a forty-megabyte file in the temporary directory per
// request, for good.
func TestAnUploadParsedOnACopyIsRemoved(t *testing.T) {
	dir := uploadThroughACopy(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if _, err := hhttp.NewRequest(r).Validate(avatarRules); err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusUnprocessableEntity)
		}
	})
	assertNoMultipartFileRemains(t, dir)
}

// TestAnUploadReadByFileIsRemoved: File parsed the form on the request the
// Context carries, which is the same copy.
func TestAnUploadReadByFileIsRemoved(t *testing.T) {
	dir := uploadThroughACopy(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if _, err := hhttp.File(hhttp.NewContext(w, r, nil, nil), "avatar"); err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadRequest)
		}
	})
	assertNoMultipartFileRemains(t, dir)
}

// TestAnUploadIsRemovedWhenTheHandlerFails: the error answer is the path a
// rejected upload takes, and it must clean up like a successful one.
func TestAnUploadIsRemovedWhenTheHandlerFails(t *testing.T) {
	dir := uploadThroughACopy(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		var form struct {
			Role string `form:"role"`
		}
		_ = hhttp.NewContext(w, r, nil, nil).Bind(&form)
		stdhttp.Error(w, "refused", stdhttp.StatusForbidden)
	})
	assertNoMultipartFileRemains(t, dir)
}

// TestAnUploadIsRemovedWhenTheHandlerPanics: a panic skips every deferred
// cleanup the handler did not write, and the upload must not depend on one.
func TestAnUploadIsRemovedWhenTheHandlerPanics(t *testing.T) {
	dir := uploadThroughACopy(t, func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		_ = hhttp.NewContext(w, r, nil, nil).Input("role")
		panic("the handler failed after reading the form")
	})
	assertNoMultipartFileRemains(t, dir)
}
