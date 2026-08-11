package validation

import (
	"fmt"
	"strings"
)

// ValidationException answers to Illuminate\Validation\ValidationException: the
// error a failed Validate carries, and what a controller catches to answer 422.
//
// The PHP is thrown and this is returned, which is the one change Go forces.
// Everything a caller reads off it is the PHP's: Errors for the bag, GetStatus
// for the code, GetErrorBag for the named bag a redirect flashes it into, and
// GetRedirectTo for where that redirect goes.
type ValidationException struct {
	// validator answers to the public $validator.
	validator *Validator

	// response answers to the public $response: the answer the HTTP layer
	// already prepared, when it prepared one. It is any because the response
	// belongs to that layer and this package does not import it.
	response any

	status     int
	errorBag   string
	redirectTo string

	message string
}

// The status a failed validation answers with, which is Laravel's default and
// the number every client is written against.
const defaultValidationStatus = 422

// NewValidationException answers to the ValidationException constructor.
//
// The PHP's $response and $errorBag default to null and "default"; the variadic
// response is how Go spells the first, and the bag starts at "default" and is
// changed with ErrorBag.
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

// WithMessages answers to ValidationException::withMessages: an exception built
// from a plain map of messages, for a failure that no rule produced.
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

// summarizeValidation answers to ValidationException::summarize: the first
// message, and how many more there were.
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

// Error is the summary the PHP passes to the Exception constructor.
func (e *ValidationException) Error() string { return e.message }

// Errors answers to ValidationException::errors: every validation message, keyed
// by the field it belongs to. It is what a controller turns into a 422 body.
func (e *ValidationException) Errors() map[string][]string {
	if e.validator == nil {
		return map[string][]string{}
	}
	return e.validator.Errors().Messages()
}

// Validator answers to the public $validator.
func (e *ValidationException) Validator() *Validator { return e.validator }

// Status answers to ValidationException::status: the HTTP status code to answer
// with. It returns the exception, as the PHP returns $this.
func (e *ValidationException) Status(status int) *ValidationException {
	e.status = status

	return e
}

// GetStatus reads the code Status set. The PHP reads the public $status
// directly; a Go field cannot carry the same name as the method that sets it, so
// the pair is spelled the way GetResponse already is.
func (e *ValidationException) GetStatus() int { return e.status }

// ErrorBag answers to ValidationException::errorBag: the name of the bag a
// redirect flashes the messages into, so that two forms on one page do not draw
// each other's errors.
func (e *ValidationException) ErrorBag(errorBag string) *ValidationException {
	e.errorBag = errorBag

	return e
}

// GetErrorBag reads the bag ErrorBag named, for the reason GetStatus exists.
func (e *ValidationException) GetErrorBag() string { return e.errorBag }

// RedirectTo answers to ValidationException::redirectTo: where the client is
// sent back to, which is the form it came from.
func (e *ValidationException) RedirectTo(url string) *ValidationException {
	e.redirectTo = url

	return e
}

// GetRedirectTo reads the URL RedirectTo named, for the reason GetStatus exists.
func (e *ValidationException) GetRedirectTo() string { return e.redirectTo }

// GetResponse answers to ValidationException::getResponse.
func (e *ValidationException) GetResponse() any { return e.response }

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
