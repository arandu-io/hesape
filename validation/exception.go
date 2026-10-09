package validation

import (
	"fmt"
	"strings"
)

// ValidationException is the error a failed Validate carries, and what a
// controller reads to answer 422.
//
// Errors is the bag, GetStatus the code, GetErrorBag the named bag a redirect
// flashes it into, and GetRedirectTo where that redirect goes.
type ValidationException struct {
	// validator is the run that failed.
	validator *Validator

	// response is the answer the HTTP layer already prepared, when it prepared
	// one. It is any because the response belongs to that layer and this
	// package does not import it.
	response any

	status     int
	errorBag   string
	redirectTo string

	message string
}

// The status a failed validation answers with, and the number every client is
// written against.
const defaultValidationStatus = 422

// NewValidationException returns the error a failed run is turned into.
//
// The variadic response is the answer the HTTP layer already prepared, when it
// prepared one. The bag starts at "default" and is changed with ErrorBag.
func NewValidationException(validator *Validator, response ...any) *ValidationException {
	e := &ValidationException{
		validator: validator,
		status:    defaultValidationStatus,
		errorBag:  "default",
	}
	if len(response) > 0 {
		e.response = response[0]
	}
	e.message = summarizeValidation(validator)

	return e
}

// WithMessages returns an exception built from a plain map of messages, for a
// failure that no rule produced.
func WithMessages(messages map[string][]string) *ValidationException {
	validator := Make(Data{}, &Set{byName: map[string]*field{}})
	validator.messages = Errors{}

	for _, key := range sortedKeys(messages) {
		for _, message := range messages[key] {
			validator.messages.Add(key, message)
		}
	}

	return NewValidationException(validator)
}

// summarizeValidation renders the first message, and how many more there
// were.
func summarizeValidation(validator *Validator) string {
	if validator == nil {
		return "The given data was invalid."
	}

	messages := validator.Errors().All()

	if len(messages) == 0 {
		return "The given data was invalid."
	}

	message := messages[0]

	if count := len(messages) - 1; count > 0 {
		pluralized := "errors"
		if count == 1 {
			pluralized = "error"
		}
		message += fmt.Sprintf(" (and %d more %s)", count, pluralized)
	}

	return message
}

// Error is the summary: the first message, and how many more there were.
func (e *ValidationException) Error() string { return e.message }

// Errors returns every validation message, keyed by the field it belongs to. It
// is what a controller turns into a 422 body.
func (e *ValidationException) Errors() map[string][]string {
	if e.validator == nil {
		return map[string][]string{}
	}
	return e.validator.Errors().Messages()
}

// Validator returns the run that failed.
func (e *ValidationException) Validator() *Validator { return e.validator }

// Status sets the HTTP status code to answer with. It returns the exception, so
// calls chain.
func (e *ValidationException) Status(status int) *ValidationException {
	e.status = status

	return e
}

// GetStatus reads the code Status set. A field cannot carry the same name as the
// method that sets it, so the pair is spelled the way GetResponse already is.
func (e *ValidationException) GetStatus() int { return e.status }

// ErrorBag names the bag a redirect flashes the messages into, so that two forms
// on one page do not draw each other's errors.
func (e *ValidationException) ErrorBag(errorBag string) *ValidationException {
	e.errorBag = errorBag

	return e
}

// GetErrorBag reads the bag ErrorBag named, for the reason GetStatus exists.
func (e *ValidationException) GetErrorBag() string { return e.errorBag }

// RedirectTo names where the client is sent back to, which is the form it came
// from.
func (e *ValidationException) RedirectTo(url string) *ValidationException {
	e.redirectTo = url

	return e
}

// GetRedirectTo reads the URL RedirectTo named, for the reason GetStatus exists.
func (e *ValidationException) GetRedirectTo() string { return e.redirectTo }

// GetResponse returns the answer the HTTP layer prepared, when it prepared one.
func (e *ValidationException) GetResponse() any { return e.response }

// HTTPStatus reports the status the failure answers with: 422, unless Status
// set another.
//
// It is the method a routing layer looks for on any error, through errors.As
// over an interface rather than over this type, so a ValidationException is
// answered with its own status by code that never imports this package. Without
// it, an adapter that knew Errors and nothing else let the exception through as
// a failure nobody claimed, and a rejected form became a 500.
func (e *ValidationException) HTTPStatus() int {
	if e.status == 0 {
		return defaultValidationStatus
	}
	return e.status
}

// As lets errors.As read the exception as Errors: the same messages, keyed by
// the field they belong to.
//
//	var errs validation.Errors
//	if errors.As(err, &errs) {
//		// err was an Errors or a *ValidationException, wrapped or not
//	}
//
// A failed Validate and a hand-built Errors are the same failure, and an
// adapter that answers a rejected form reads both with the one errors.As above
// instead of a branch for each. The map written into the target is a copy, so
// what the adapter does with it does not reach back into the exception.
//
// Any other target answers false, which is what lets errors.As keep walking the
// chain.
func (e *ValidationException) As(target any) bool {
	errs, ok := target.(*Errors)
	if !ok || e == nil {
		return false
	}

	messages := e.Errors()
	copied := make(Errors, len(messages))
	for field, list := range messages {
		copied[field] = append([]string(nil), list...)
	}
	*errs = copied
	return true
}

// sortedKeys is the stable order a Go map has none of.
func sortedKeys(messages map[string][]string) []string {
	keys := make([]string, 0, len(messages))
	for key := range messages {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && strings.Compare(keys[j-1], keys[j]) > 0; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
