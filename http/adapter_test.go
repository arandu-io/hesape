package http_test

import (
	"errors"
	"fmt"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	hhttp "github.com/arandu-io/hesape/http"
	"github.com/arandu-io/hesape/http/exceptions"
	"github.com/arandu-io/hesape/routing"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
)

// The adapter is the parameter hesape/routing takes, as it stands.
var _ routing.Adapter[hhttp.Context] = hhttp.NewAdapter(nil, nil, nil)

// adapted serves one request through the adapter wrapping action.
func adapted(t *testing.T, flash *session.Flash, action func(*hhttp.Context) error, header stdhttp.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodPost, "/invoices", nil)
	req.Host = "example.test"
	for key, values := range header {
		req.Header[key] = values
	}
	rec := httptest.NewRecorder()
	hhttp.NewAdapter(nil, nil, flash)(action).ServeHTTP(rec, req)
	return rec
}

func TestAnActionThatSucceedsKeepsItsOwnAnswer(t *testing.T) {
	rec := adapted(t, nil, func(c *hhttp.Context) error {
		c.Response.WriteHeader(stdhttp.StatusAccepted)
		return nil
	}, nil)
	if rec.Code != stdhttp.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
}

func TestARejectionOnAPageIsAnsweredWithTheRedirectBack(t *testing.T) {
	flash := session.NewFlash(make([]byte, 32), false)
	rec := adapted(t, flash, func(*hhttp.Context) error {
		return validation.Errors{"title": {"this field is required"}}
	}, stdhttp.Header{"Referer": {"http://example.test/invoices/create"}})

	if rec.Code != stdhttp.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if to := rec.Header().Get("Location"); to != "/invoices/create" {
		t.Errorf("Location = %q, want /invoices/create", to)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Error("the rejection carried no flash cookie")
	}
}

func TestARejectionForAJSONClientIsA422Problem(t *testing.T) {
	rec := adapted(t, nil, func(*hhttp.Context) error {
		return validation.Errors{"title": {"this field is required"}}
	}, stdhttp.Header{"Accept": {"application/json"}})

	if rec.Code != stdhttp.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("Content-Type = %q, want a problem document", ct)
	}
	if !strings.Contains(rec.Body.String(), "this field is required") {
		t.Errorf("body = %s, want the message keyed by field", rec.Body)
	}
}

func TestAnErrorNamingAStatusIsAnsweredWithItAndItsHeaders(t *testing.T) {
	throttled := exceptions.NewThrottleRequestsException("slow down", nil, stdhttp.Header{"Retry-After": {"30"}}, 0)
	rec := adapted(t, nil, func(*hhttp.Context) error {
		return fmt.Errorf("listing invoices: %w", throttled)
	}, nil)

	if rec.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
	if strings.Contains(rec.Body.String(), "slow down") {
		t.Errorf("body = %q, carries the error's own text", rec.Body)
	}
}

func TestARefusalIsAnsweredWithItsStatus(t *testing.T) {
	rec := adapted(t, nil, func(*hhttp.Context) error { return auth.ErrForbidden }, nil)
	if rec.Code != stdhttp.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAnUnclaimedErrorPanics(t *testing.T) {
	cause := errors.New("connection refused")
	defer func() {
		if v := recover(); v != cause {
			t.Fatalf("panicked with %v, want the action's error", v)
		}
	}()
	adapted(t, nil, func(*hhttp.Context) error { return cause }, nil)
	t.Fatal("an unclaimed error was answered")
}

func TestARejectionOnAPageWithNoFlashPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a rejection nobody would see did not panic")
		}
	}()
	adapted(t, nil, func(*hhttp.Context) error {
		return validation.Errors{"title": {"required"}}
	}, nil)
}

// The flash is on the request before the action runs, so a sentence written
// with With reaches the same cookie a rejection would.
func TestTheFlashIsOnTheRequestTheActionReceives(t *testing.T) {
	flash := session.NewFlash(make([]byte, 32), false)
	rec := adapted(t, flash, func(c *hhttp.Context) error {
		c.With("status", "saved")
		return c.Redirect("/invoices")
	}, nil)
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("the sentence written with With reached no cookie")
	}
}
