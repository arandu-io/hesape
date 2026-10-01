package http_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/exceptions"
	"github.com/arandu-io/hesape/validation"
)

// limited returns a request whose body is cut off at limit bytes, the way the
// body-size middleware hands it on.
func limited(method, contentType, body string, limit int64) *stdhttp.Request {
	r := httptest.NewRequest(method, "/documents", strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	r.ContentLength = -1
	r.Body = stdhttp.MaxBytesReader(httptest.NewRecorder(), r.Body, limit)
	return r
}

// assert413 fails unless err is the collection's 413 for a body over the limit,
// and answers HTTPStatus with it.
func assert413(t *testing.T, err error) {
	t.Helper()
	var tooLarge *exceptions.PostTooLargeException
	if !errors.As(err, &tooLarge) {
		t.Fatalf("error = %v, want a *exceptions.PostTooLargeException", err)
	}
	if got := tooLarge.HTTPStatus(); got != stdhttp.StatusRequestEntityTooLarge {
		t.Fatalf("HTTPStatus = %d, want 413", got)
	}
	var cause *stdhttp.MaxBytesError
	if !errors.As(err, &cause) {
		t.Fatal("the 413 does not wrap the reader's error, so a log cannot say what the limit was")
	}
}

var titleRules = validation.MustCompile(validation.Rules{"title": "required"})

// TestValidateAnswersAFormCutOffByTheLimitWith413: the rules used to judge the
// part of the body that arrived, and an oversized form came back as a field
// that was "required".
func TestValidateAnswersAFormCutOffByTheLimitWith413(t *testing.T) {
	r := limited(stdhttp.MethodPost, "application/x-www-form-urlencoded", "title="+strings.Repeat("a", 4096), 64)

	_, err := hhttp.NewRequest(r).Validate(titleRules)
	assert413(t, err)
}

// TestValidateWithBagAnswersAFormCutOffByTheLimitWith413: the named variant is
// the same read.
func TestValidateWithBagAnswersAFormCutOffByTheLimitWith413(t *testing.T) {
	r := limited(stdhttp.MethodPost, "application/x-www-form-urlencoded", "title="+strings.Repeat("a", 4096), 64)

	_, err := hhttp.NewRequest(r).ValidateWithBag("documents", titleRules)
	assert413(t, err)
}

// TestValidateAnswersAMultipartBodyCutOffByTheLimitWith413: the upload is the
// body most likely to meet the limit.
func TestValidateAnswersAMultipartBodyCutOffByTheLimitWith413(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("title", "a draft")
	part, _ := writer.CreateFormFile("scan", "scan.bin")
	_, _ = part.Write(bytes.Repeat([]byte{0}, 4096))
	_ = writer.Close()

	r := limited(stdhttp.MethodPost, writer.FormDataContentType(), body.String(), 256)
	_, err := hhttp.NewRequest(r).Validate(titleRules)
	assert413(t, err)
}

// TestBindAnswersAFormCutOffByTheLimitWith413: it used to be a wrapped parse
// failure, which the router answered 500.
func TestBindAnswersAFormCutOffByTheLimitWith413(t *testing.T) {
	r := limited(stdhttp.MethodPost, "application/x-www-form-urlencoded", "title="+strings.Repeat("a", 4096), 64)

	var form struct {
		Title string `form:"title"`
	}
	assert413(t, hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&form))
}

// TestBindAnswersAJSONBodyCutOffByTheLimitWith413: a JSON body that could not
// be read whole used to bind as an empty object.
func TestBindAnswersAJSONBodyCutOffByTheLimitWith413(t *testing.T) {
	r := limited(stdhttp.MethodPost, "application/json", `{"title":"`+strings.Repeat("a", 4096)+`"}`, 64)

	var form struct {
		Title string `form:"title"`
	}
	assert413(t, hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&form))
}

// TestABodyUnderTheLimitIsReadAsBefore: the limit changes nothing for a body
// that fits.
func TestABodyUnderTheLimitIsReadAsBefore(t *testing.T) {
	r := limited(stdhttp.MethodPost, "application/x-www-form-urlencoded", "title=a+draft", 1<<20)

	input, err := hhttp.NewRequest(r).Validate(titleRules)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := input.String("title"); got != "a draft" {
		t.Fatalf("title = %q", got)
	}
}
