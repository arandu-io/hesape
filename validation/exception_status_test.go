package validation_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/arandu-io/hesape/validation"
)

// TestAValidationExceptionAnswersTheStatusInterface: a routing layer that does
// not import this package finds the status through errors.As over an
// interface. Without the method a failed Validate returned by a handler was
// claimed by nothing, and the person who mistyped an address got a 500.
func TestAValidationExceptionAnswersTheStatusInterface(t *testing.T) {
	err := fmt.Errorf("registering: %w", validation.WithMessages(map[string][]string{
		"email": {"The email field must be a valid email address."},
	}))

	var statused interface{ HTTPStatus() int }
	if !errors.As(err, &statused) {
		t.Fatal("a wrapped *ValidationException must satisfy interface{ HTTPStatus() int }")
	}
	if got := statused.HTTPStatus(); got != http.StatusUnprocessableEntity {
		t.Fatalf("HTTPStatus() = %d, want 422", got)
	}
}

// TestTheStatusAValidationExceptionWasGivenIsTheOneItAnswers: Status is the
// setter, and HTTPStatus has to read what it set, or an application that chose
// 400 for its API gets 422 from the adapter anyway.
func TestTheStatusAValidationExceptionWasGivenIsTheOneItAnswers(t *testing.T) {
	err := validation.WithMessages(map[string][]string{"name": {"required"}}).Status(http.StatusBadRequest)

	if got := err.HTTPStatus(); got != http.StatusBadRequest {
		t.Fatalf("HTTPStatus() = %d, want the 400 Status set", got)
	}
}

// TestAZeroValidationExceptionStillAnswers422: a status of zero is not an
// answer, and a value built without the constructor must not hand one to the
// adapter.
func TestAZeroValidationExceptionStillAnswers422(t *testing.T) {
	var e validation.ValidationException
	if got := e.HTTPStatus(); got != http.StatusUnprocessableEntity {
		t.Fatalf("HTTPStatus() = %d, want 422", got)
	}
}

// TestAValidationExceptionReadsAsErrors: the adapter that answers a rejected
// form looks for Errors with errors.As. A failed Validate has to be found by
// the same call, with the same messages, or it is a second kind of rejected
// form that only one of the two paths knows.
func TestAValidationExceptionReadsAsErrors(t *testing.T) {
	v := validation.Make(validation.Data{"name": "", "email": "nope"},
		validation.MustCompile(validation.Rules{"name": "required", "email": "email"}))
	_, failed := v.Validate()
	if failed == nil {
		t.Fatal("the run must fail, or there is nothing to read")
	}
	err := fmt.Errorf("storing the account: %w", failed)

	var errs validation.Errors
	if !errors.As(err, &errs) {
		t.Fatal("a wrapped *ValidationException must be found by errors.As(err, *validation.Errors)")
	}

	var invalid *validation.ValidationException
	if !errors.As(err, &invalid) {
		t.Fatal("the exception itself must stay reachable")
	}
	want := invalid.Errors()
	if len(errs) != len(want) || len(errs) != 2 {
		t.Fatalf("Errors read %v, want the %d fields of %v", errs, len(want), want)
	}
	for field, messages := range want {
		got := errs.Get(field)
		if len(got) != len(messages) || got[0] != messages[0] {
			t.Errorf("%s: read %v, want %v", field, got, messages)
		}
	}
}

// TestTheErrorsReadFromAnExceptionAreACopy: the adapter writes the messages to
// a flash and may add to them; that must not change what the exception reports
// to the log afterwards.
func TestTheErrorsReadFromAnExceptionAreACopy(t *testing.T) {
	exc := validation.WithMessages(map[string][]string{"name": {"required"}})

	var errs validation.Errors
	if !errors.As(error(exc), &errs) {
		t.Fatal("the exception must read as Errors")
	}
	errs.Add("name", "added by the adapter")
	errs.Add("other", "also added")

	if got := exc.Errors(); len(got) != 1 || len(got["name"]) != 1 {
		t.Fatalf("the exception now reports %v: the target aliased its messages", got)
	}
}

// TestAValidationExceptionIsNotEveryTarget: As answers only for Errors, so
// errors.As keeps walking the chain for anything else.
func TestAValidationExceptionIsNotEveryTarget(t *testing.T) {
	exc := validation.WithMessages(map[string][]string{"name": {"required"}})

	var other map[string]string
	if exc.As(&other) {
		t.Fatal("As claimed a target that is not *validation.Errors")
	}
}
