package exception_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/validation"
)

// decodeProblem reads a recorded response as a problem document, and fails the
// test when it is not one.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) (exception.Problem, map[string]json.RawMessage) {
	t.Helper()

	if got := rec.Header().Get("Content-Type"); got != exception.ProblemContentType {
		t.Fatalf("Content-Type = %q, want %q", got, exception.ProblemContentType)
	}
	var p exception.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("the body is not a problem document: %v\n%s", err, rec.Body)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &members); err != nil {
		t.Fatalf("the body is not a JSON object: %v", err)
	}
	return p, members
}

// TestAValidationProblemCarriesTheMessagesByField: a JSON client cannot read a
// flash, so the errors member is the only way it learns which field to
// correct. Keyed by field, the same shape validation.Errors has.
func TestAValidationProblemCarriesTheMessagesByField(t *testing.T) {
	errs := validation.Errors{}
	errs.Add("email", "The email field must be a valid email address.")
	errs.Add("name", "The name field is required.")
	errs.Add("name", "The name field must be at least 2 characters.")

	rec := httptest.NewRecorder()
	exception.WriteValidationProblem(rec, httptest.NewRequest(http.MethodPost, "/accounts", nil), errs)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("answered %d, want 422", rec.Code)
	}
	p, _ := decodeProblem(t, rec)
	if p.Status != http.StatusUnprocessableEntity || p.Title != "Unprocessable Entity" {
		t.Errorf("status/title = %d %q", p.Status, p.Title)
	}
	if len(p.Errors) != 2 || len(p.Errors["name"]) != 2 || p.Errors["email"][0] != errs.First("email") {
		t.Fatalf("errors = %v, want %v", p.Errors, errs)
	}
	if p.Detail == "" {
		t.Error("the detail is empty, and a client that shows one sentence has nothing to show")
	}
}

// TestAValidationProblemIsWrittenLikeEveryOtherProblem: the two writers share
// the headers, so a validation failure is no more cacheable, no less traceable
// and no more revealing of the query than a refusal.
func TestAValidationProblemIsWrittenLikeEveryOtherProblem(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/accounts/new?token=s3cret&ref=mail", nil)

	validationRec := httptest.NewRecorder()
	validationRec.Header().Set("X-Request-ID", "req-42")
	exception.WriteValidationProblem(validationRec, req, map[string][]string{"email": {"invalid"}})

	refusalRec := httptest.NewRecorder()
	refusalRec.Header().Set("X-Request-ID", "req-42")
	exception.WriteProblem(refusalRec, req, http.StatusForbidden, "This action is unauthorized.")

	for _, header := range []string{"Content-Type", "Cache-Control"} {
		if got, want := validationRec.Header().Get(header), refusalRec.Header().Get(header); got != want {
			t.Errorf("%s = %q, and a refusal is written with %q", header, got, want)
		}
	}
	if got := validationRec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q: one person's typed input in a shared cache", got)
	}

	p, _ := decodeProblem(t, validationRec)
	if p.RequestID != "req-42" {
		t.Errorf("request_id = %q, want the id of the log line", p.RequestID)
	}
	if p.Instance != "/accounts/new" {
		t.Errorf("instance = %q, want the path without its query", p.Instance)
	}
	if strings.Contains(validationRec.Body.String(), "s3cret") {
		t.Error("the query string reached the body")
	}
}

// TestAProblemWithoutValidationHasNoErrorsMember: the member is the
// validation failure's, and a client that tests for its presence must not find
// an empty one on a 403.
func TestAProblemWithoutValidationHasNoErrorsMember(t *testing.T) {
	rec := httptest.NewRecorder()
	exception.WriteProblem(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusForbidden, "")

	_, members := decodeProblem(t, rec)
	if _, present := members["errors"]; present {
		t.Fatalf("a refusal carries an errors member: %s", rec.Body)
	}
}

// TestTheHandlerAnswersAJSONValidationFailureWithItsMessages: the Handler is
// the other adapter that writes a problem, and a validation failure it answers
// has to carry the same member the router's answer does.
func TestTheHandlerAnswersAJSONValidationFailureWithItsMessages(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	for _, err := range []error{
		fmt.Errorf("registering: %w", validation.WithMessages(map[string][]string{"email": {"invalid"}})),
		fmt.Errorf("registering: %w", validation.Errors{"email": {"invalid"}}),
	} {
		req := httptest.NewRequest(http.MethodPost, "/accounts", nil)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()

		h.Render(rec, req, err)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%T: answered %d, want 422", err, rec.Code)
		}
		p, _ := decodeProblem(t, rec)
		if got := p.Errors["email"]; len(got) != 1 || got[0] != "invalid" {
			t.Errorf("errors = %v, want the email message", p.Errors)
		}
	}
}
