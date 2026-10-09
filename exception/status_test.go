package exception_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/exception"
	"github.com/arandu-io/hesape/http/exceptions"
	"github.com/arandu-io/hesape/session"
	"github.com/arandu-io/hesape/validation"
)

// invoiceClosed is an application's own domain failure, stating its status the
// way the first row of the table reads it: by method set, with no import of
// the exception package.
type invoiceClosed struct{ cause error }

func (e invoiceClosed) Error() string   { return "the invoice is closed" }
func (e invoiceClosed) HTTPStatus() int { return http.StatusConflict }
func (e invoiceClosed) Unwrap() error   { return e.cause }

func rejected() validation.Errors {
	errs := validation.Errors{}
	errs.Add("email", "The email field must be a valid email address.")
	return errs
}

// row is one entry of the status table: an error and the status it asks for.
type row struct {
	name string
	err  error
	want int
}

// everyRow is every row StatusOf answers, each written the way it arrives at
// an adapter: wrapped, because a service adds context on the way out.
func everyRow() []row {
	return []row{
		{"an error stating its own status", fmt.Errorf("closing: %w", invoiceClosed{}), http.StatusConflict},
		{"an abort", fmt.Errorf("loading: %w", exception.Abort(http.StatusGone, "")), http.StatusGone},
		{"a failed validate", fmt.Errorf("registering: %w", validation.WithMessages(map[string][]string{"email": {"invalid"}})), http.StatusUnprocessableEntity},
		{"a body over the limit", fmt.Errorf("storing: %w", exceptions.NewPostTooLargeException("", &http.MaxBytesError{Limit: 1}, nil, 0)), http.StatusRequestEntityTooLarge},
		{"a spent rate limit", exceptions.NewThrottleRequestsException("", nil, nil, 0), http.StatusTooManyRequests},
		{"a path that cannot be decoded", exceptions.NewMalformedUrlException(), http.StatusBadRequest},
		{"validation errors", fmt.Errorf("registering: %w", rejected()), http.StatusUnprocessableEntity},
		{"a record that does not exist", fmt.Errorf("loading invoice 7: %w", database.ErrRecordNotFound), http.StatusNotFound},
		{"a repository miss", fmt.Errorf("loading invoice 7: %w", database.ErrNotFound), http.StatusNotFound},
		{"a model that does not exist", fmt.Errorf("%w: invoices [7]", model.ErrModelNotFound), http.StatusNotFound},
		{"a policy refused", fmt.Errorf("%w: invoices.delete", auth.ErrForbidden), http.StatusForbidden},
		{"a token that does not match", fmt.Errorf("submitting: %w", session.ErrTokenMismatch), exception.StatusPageExpired},
		{"a duplicate key", fmt.Errorf("inserting user: %w", database.ErrUniqueViolation), http.StatusConflict},
	}
}

// TestStatusOfAnswersEveryRow pins the table one row at a time, so a row that
// goes missing names itself in the failure.
func TestStatusOfAnswersEveryRow(t *testing.T) {
	for _, r := range everyRow() {
		t.Run(r.name, func(t *testing.T) {
			status, ok := exception.StatusOf(r.err)
			if !ok {
				t.Fatalf("%v was not claimed, so an adapter answers it with 500", r.err)
			}
			if status != r.want {
				t.Fatalf("status = %d, want %d", status, r.want)
			}
		})
	}
}

// TestStatusOfAnswersEveryRowOfTheRouterTable: the router adapter carried a
// table of its own before it called this one, and while both exist they must
// not disagree. These are its rows, in its words: anything it answers with a
// status, StatusOf answers with the same status. A row deleted here, or added
// there and not here, is the second answer for the same error that the one
// table exists to prevent.
func TestStatusOfAnswersEveryRowOfTheRouterTable(t *testing.T) {
	routerTable := []row{
		{"model.ErrModelNotFound", model.ErrModelNotFound, http.StatusNotFound},
		{"database.ErrRecordNotFound", database.ErrRecordNotFound, http.StatusNotFound},
		{"security.ErrForbidden", auth.ErrForbidden, http.StatusForbidden},
		{"security.ErrCSRF", session.ErrTokenMismatch, exception.StatusPageExpired},
		{"database.ErrUniqueViolation", database.ErrUniqueViolation, http.StatusConflict},
		{"HTTPStatus() int", invoiceClosed{}, http.StatusConflict},
	}
	for _, r := range routerTable {
		status, ok := exception.StatusOf(r.err)
		if !ok || status != r.want {
			t.Errorf("%s: StatusOf = (%d, %v), and the router answers %d", r.name, status, ok, r.want)
		}
	}
}

// TestAnErrorStatingItsStatusWinsOverTheSentinelItWrapped: the explicit
// statement is the answer and the sentinel is its cause. A domain failure that
// says 409 about a row that went missing must not be turned into a 404 by the
// row it wrapped.
func TestAnErrorStatingItsStatusWinsOverTheSentinelItWrapped(t *testing.T) {
	err := fmt.Errorf("closing invoice 7: %w", invoiceClosed{cause: database.ErrRecordNotFound})

	if status, _ := exception.StatusOf(err); status != http.StatusConflict {
		t.Fatalf("status = %d, want the 409 the error stated", status)
	}
}

// TestAValidationExceptionAnswersTheStatusItWasGiven: the exception's own
// status is an explicit statement like any other, so an API that chose 400 for
// its validation failures gets 400 rather than the row's 422.
func TestAValidationExceptionAnswersTheStatusItWasGiven(t *testing.T) {
	err := validation.WithMessages(map[string][]string{"name": {"required"}}).Status(http.StatusBadRequest)

	if status, ok := exception.StatusOf(err); !ok || status != http.StatusBadRequest {
		t.Fatalf("StatusOf = (%d, %v), want (400, true)", status, ok)
	}
}

// TestEmptyValidationErrorsAreNotClaimed: a handler that returned an Errors
// with nothing in it never asked whether anything failed. Claiming it as 422
// would answer a form with "correct this" and nothing to correct.
func TestEmptyValidationErrorsAreNotClaimed(t *testing.T) {
	for _, err := range []error{validation.Errors{}, validation.Errors(nil), fmt.Errorf("x: %w", validation.Errors{})} {
		if status, ok := exception.StatusOf(err); ok {
			t.Errorf("%#v was answered with %d, and it holds no message", err, status)
		}
	}
}

// TestAStatedStatusIsReturnedAsWritten: what to do with a status outside the
// error range is the adapter's decision, and it can only make it if the table
// does not quietly replace the value.
func TestAStatedStatusIsReturnedAsWritten(t *testing.T) {
	err := &exception.HTTPError{Status: http.StatusOK}

	if status, ok := exception.StatusOf(err); !ok || status != http.StatusOK {
		t.Fatalf("StatusOf = (%d, %v), want (200, true)", status, ok)
	}
}

// TestTheHandlerAnswersEveryRowWithTheTablesStatus: the Handler is an adapter
// too, and it asks the same table. A row the Handler answered differently from
// the router would be the same error with two answers depending on which one
// it reached.
func TestTheHandlerAnswersEveryRowWithTheTablesStatus(t *testing.T) {
	h := exception.NewHandler(exception.Config{})
	for _, r := range everyRow() {
		t.Run(r.name, func(t *testing.T) {
			for _, accept := range []string{"text/html", "application/json"} {
				req := httptest.NewRequest(http.MethodPost, "/invoices", nil)
				req.Header.Set("Accept", accept)
				rec := httptest.NewRecorder()

				h.Render(rec, req, r.err)

				if rec.Code != r.want {
					t.Errorf("Accept %s: answered %d, want %d", accept, rec.Code, r.want)
				}
			}
		})
	}
}

// TestAnUnclaimedErrorStaysUnclaimed: the table is closed, and an error that
// matches no row is a defect the adapter answers as one.
func TestAnUnclaimedErrorStaysUnclaimed(t *testing.T) {
	err := fmt.Errorf("writing the export: %w", errors.New("disk full"))

	if status, ok := exception.StatusOf(err); ok {
		t.Fatalf("answered %d, and no row claims a full disk", status)
	}
}
