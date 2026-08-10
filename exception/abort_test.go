package exception_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/session"
)

func TestAbortNamesItsStatus(t *testing.T) {
	err := exception.Abort(http.StatusNotFound, "no invoice with that number")

	status, ok := exception.StatusOf(err)
	if !ok {
		t.Fatal("an aborted request did not claim a status")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}

	var he *exception.HTTPError
	if !errors.As(err, &he) {
		t.Fatal("Abort must return an *HTTPError, or nothing downstream can read the message")
	}
	if he.Message != "no invoice with that number" {
		t.Fatalf("message = %q", he.Message)
	}
}

// The status survives wrapping, which is what lets a service abort and a
// controller add context on the way out without losing the answer.
func TestTheStatusSurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("loading invoice inv-1: %w", exception.Abort(http.StatusGone, "this invoice was voided"))

	status, ok := exception.StatusOf(err)
	if !ok || status != http.StatusGone {
		t.Fatalf("StatusOf = (%d, %v), want (410, true)", status, ok)
	}
}

// The whole reason this package exists: before it, every error leaving a
// handler became 500, including the ones that had already said what they were.
func TestTheCollectionsSentinelsAreClassified(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"a policy refused", fmt.Errorf("%w: posts.delete", auth.ErrForbidden), http.StatusForbidden},
		{"the token is stale", session.ErrTokenMismatch, exception.StatusPageExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, ok := exception.StatusOf(c.err)
			if !ok {
				t.Fatalf("%v was not classified", c.err)
			}
			if status != c.want {
				t.Fatalf("status = %d, want %d", status, c.want)
			}
		})
	}
}

// An explicit Abort wins over a sentinel it wrapped: the developer said what
// this means, and the sentinel is the cause, not the answer.
func TestAnExplicitAbortWinsOverTheSentinel(t *testing.T) {
	err := &exception.HTTPError{
		Status:  http.StatusNotFound,
		Message: "no such page",
		Err:     fmt.Errorf("%w: pages.view", auth.ErrForbidden),
	}

	if status, _ := exception.StatusOf(err); status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestAnUnclaimedErrorIsNotClassified(t *testing.T) {
	for _, err := range []error{nil, errors.New("the disk is full")} {
		if status, ok := exception.StatusOf(err); ok {
			t.Fatalf("%v was answered with %d, and nothing claimed it", err, status)
		}
	}
}

// The Error string carries the status, because it ends up in a log line where
// the number is the first thing anybody looks for.
func TestTheErrorStringNamesTheStatusAndTheCause(t *testing.T) {
	err := &exception.HTTPError{Status: http.StatusNotFound, Err: errors.New("sql: no rows")}

	got := err.Error()
	for _, want := range []string{"Not Found", "sql: no rows"} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
	if !errors.Is(err, err.Err) {
		t.Error("the cause must stay reachable through errors.Is")
	}
}
