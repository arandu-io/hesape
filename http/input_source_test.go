package http_test

import (
	"bytes"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/validation"
)

var (
	requiredRole = validation.MustCompile(validation.Rules{"role": "required|in:user,member"})
	optionalRole = validation.MustCompile(validation.Rules{"role": "nullable|in:user,member"})
)

// multipartPost builds a multipart POST of target carrying the fields and one
// small file, the shape of any form that uploads something.
func multipartPost(t *testing.T, target string, fields map[string]string) *stdhttp.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("avatar", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("png"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(stdhttp.MethodPost, target, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

// TestAMultipartBodyIsWhatIsValidated: a multipart form used to reach
// Validate through ParseForm, which does not read a multipart body, so the
// rules ran against nothing -- a nullable rule passed -- while Context.Input
// read the body and handed the handler role=admin, a value no rule had seen.
func TestAMultipartBodyIsWhatIsValidated(t *testing.T) {
	r := multipartPost(t, "/users", map[string]string{"role": "admin"})
	if _, err := hhttp.NewRequest(r).Validate(optionalRole); err == nil {
		t.Fatal("role=admin in a multipart body passed in:user,member: the rules did not see the body")
	}
}

// TestTheQueryCannotStandInForAMultipartField: with ?role=user on the action
// URL and role=admin in the multipart body, the rules used to validate the
// query's "user" while Input, read after the files were parsed, returned the
// body's "admin".
func TestTheQueryCannotStandInForAMultipartField(t *testing.T) {
	r := multipartPost(t, "/users?role=user", map[string]string{"role": "admin"})
	req := hhttp.NewRequest(r)
	if _, err := req.Validate(requiredRole); err == nil {
		t.Fatal("the query's role=user was validated in place of the body's role=admin")
	}
	if got := req.Input("role"); got != "admin" {
		t.Fatalf("Input(role) = %v, want the body's value", got)
	}
	if got := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Input("role"); got != "admin" {
		t.Fatalf("Context.Input(role) = %q, want the body's value", got)
	}
}

// TestAJSONBodyIsWhatContextInputReads: Validate read a JSON body while
// Context.Input read FormValue, which never decodes JSON and fell through to
// the query string -- so ?role=admin reached the handler beside a body whose
// role=user had been validated.
func TestAJSONBodyIsWhatContextInputReads(t *testing.T) {
	r := httptest.NewRequest(stdhttp.MethodPost, "/users?role=admin", strings.NewReader(`{"role":"user"}`))
	r.Header.Set("Content-Type", "application/json")
	ctx := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil)

	in, err := hhttp.NewRequest(r).Validate(requiredRole)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := ctx.Input("role"); got != in.String("role") {
		t.Fatalf("Context.Input(role) = %q, validated %q: the handler read a value no rule saw", got, in.String("role"))
	}
}

type approvalRequest struct {
	Approved bool `form:"approved"`
}

// TestAnUncheckedBoxIsNotFilledFromTheActionURL: an unchecked checkbox sends
// nothing, and the field's absence is the answer "no". A query string merged
// into the input turned ?approved=1 on the form's action into a "yes" the
// person never ticked, for Input, Boolean, Context.Input and Bind alike.
func TestAnUncheckedBoxIsNotFilledFromTheActionURL(t *testing.T) {
	newPost := func() *stdhttp.Request {
		r := httptest.NewRequest(stdhttp.MethodPost, "/approvals?approved=1", strings.NewReader(url.Values{"note": {"x"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}

	req := hhttp.NewRequest(newPost())
	if req.Boolean("approved") || req.Input("approved") != nil || req.Has("approved") {
		t.Fatal("the action URL's approved=1 was read as the checkbox")
	}
	ctx := hhttp.NewContext(httptest.NewRecorder(), newPost(), nil, nil)
	if got := ctx.Input("approved"); got != "" {
		t.Fatalf("Context.Input(approved) = %q, want empty: the box was not ticked", got)
	}
	var bound approvalRequest
	if err := hhttp.NewContext(httptest.NewRecorder(), newPost(), nil, nil).Bind(&bound); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if bound.Approved {
		t.Fatal("Bind read approved=1 from the action URL")
	}
	if got := ctx.Query("approved"); got != "1" {
		t.Fatalf("Query(approved) = %q, want the query string still readable on its own", got)
	}
}

// TestADeleteBodyIsInput: net/http reads a url-encoded body for POST, PUT and
// PATCH only, so a DELETE's form would otherwise be empty -- and with the query
// no longer merged in, empty is what every reader would see.
func TestADeleteBodyIsInput(t *testing.T) {
	r := httptest.NewRequest(stdhttp.MethodDelete, "/posts/1?reason=query", strings.NewReader("reason=body"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if got := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Input("reason"); got != "body" {
		t.Fatalf("Context.Input(reason) = %q, want the DELETE body's value", got)
	}
}

// TestAGetReadsItsQueryEverywhere: for GET the query string is the input, so
// every reader agrees on it.
func TestAGetReadsItsQueryEverywhere(t *testing.T) {
	r := httptest.NewRequest(stdhttp.MethodGet, "/users?role=member", nil)
	in, err := hhttp.NewRequest(r).Validate(requiredRole)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Input("role"); got != in.String("role") || got != "member" {
		t.Fatalf("Context.Input(role) = %q, validated %q", got, in.String("role"))
	}
}

// TestJSONBindsItsTopLevelFields: Bind reads the same map as Validate, so a
// JSON body binds, and the query string beside it does not.
func TestJSONBindsItsTopLevelFields(t *testing.T) {
	type jsonRequest struct {
		Role   string   `form:"role"`
		Count  int      `form:"count"`
		Active *bool    `form:"active"`
		Tags   []string `form:"tags"`
	}
	r := httptest.NewRequest(stdhttp.MethodPost, "/users?role=admin", strings.NewReader(`{"role":"user","count":3,"active":false,"tags":["a","b"]}`))
	r.Header.Set("Content-Type", "application/json")
	var got jsonRequest
	if err := hhttp.NewContext(httptest.NewRecorder(), r, nil, nil).Bind(&got); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if got.Role != "user" || got.Count != 3 || got.Active == nil || *got.Active || len(got.Tags) != 2 {
		t.Fatalf("bound %+v, want the JSON body's fields", got)
	}
}
