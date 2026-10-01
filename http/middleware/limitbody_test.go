package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/middleware"
	"github.com/arandu-io/hesape/validation"
)

// TestADeclaredLengthOverTheLimitIsAnswered413: the middleware said it answers
// 413 by itself, and it never did -- MaxBytesReader only fails the read, and a
// handler that ignored the error answered whatever it pleased. A body whose
// declared length is over the limit is now refused before the handler runs.
func TestADeclaredLengthOverTheLimitIsAnswered413(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader(strings.Repeat("a", 4096)))

	ran := false
	rec := httptest.NewRecorder()
	middleware.LimitBodySize(64)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		ran = true
	})).ServeHTTP(rec, r)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if ran {
		t.Fatal("the handler ran for a body the middleware had already refused")
	}
}

// TestABodyThatDeclaresNoLengthIsAnswered413ByTheReaders: a chunked body
// reaches the handler, and the request readers turn the cut-off read into an
// error that answers 413 rather than validating what part of it arrived.
func TestABodyThatDeclaresNoLengthIsAnswered413ByTheReaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader("title="+strings.Repeat("a", 4096)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ContentLength = -1

	rules := validation.MustCompile(validation.Rules{"title": "required"})
	var err error
	middleware.LimitBodySize(64)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, err = hhttp.NewRequest(r).Validate(rules)
	})).ServeHTTP(httptest.NewRecorder(), r)

	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != http.StatusRequestEntityTooLarge {
		t.Fatalf("Validate = %v, want an error answering 413", err)
	}
}
