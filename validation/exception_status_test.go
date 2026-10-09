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

// TestTheMessagesErrorsReturnsAreACopy: Errors handed out the validator's own
// map while As handed out a copy, so a controller that added a message to what
// Errors returned, or trimmed a field's list before flashing it, rewrote the
// failure the exception reported to the log and to every later reader.
func TestTheMessagesErrorsReturnsAreACopy(t *testing.T) {
	v := validation.Make(validation.Data{"name": "", "email": "nope"},
		validation.MustCompile(validation.Rules{"name": "required", "email": "email"}))
	_, failed := v.Validate()
	var exc *validation.ValidationException
	if !errors.As(failed, &exc) {
		t.Fatal("a failed Validate must carry a *ValidationException")
	}
	before := exc.Errors()
	first := before["name"][0]

	got := exc.Errors()
	got["name"][0] = "rewritten by the caller"
	got["name"] = append(got["name"], "appended by the caller")
	got["other"] = []string{"added by the caller"}
	delete(got, "email")

	after := exc.Errors()
	if len(after) != len(before) {
		t.Fatalf("the exception now reports %d fields, want %d: %v", len(after), len(before), after)
	}
	if _, added := after["other"]; added {
		t.Fatal("a field added to the returned map reached the exception")
	}
	if len(after["email"]) != 1 {
		t.Fatal("a field deleted from the returned map was deleted from the exception")
	}
	if len(after["name"]) != 1 || after["name"][0] != first {
		t.Fatalf("name now reads %q, want only %q: the returned slice aliased the exception's", after["name"], first)
	}
	if v.Errors().First("name") != first {
		t.Fatalf("the validator's own bag reads %q, want %q", v.Errors().First("name"), first)
	}
}

// TestANilValidationExceptionHasNoMessages: As already answers false for a nil
// exception, and Errors answers the empty map rather than a panic, so a caller
// that reads it from a typed nil does not take the request down.
func TestANilValidationExceptionHasNoMessages(t *testing.T) {
	var exc *validation.ValidationException
	if got := exc.Errors(); got == nil || len(got) != 0 {
		t.Fatalf("Errors() = %#v, want an empty map", got)
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
