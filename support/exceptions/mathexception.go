package exceptions

// MathException answers to Illuminate\Support\Exceptions\MathException: the
// RuntimeException Number::spell and the currency and percentage writers raise
// when they are handed a number they cannot write.
//
// The PHP class carries nothing but its name; in Go an error carries its
// message, so this does.
type MathException struct {
	// Message is what the PHP passes to the RuntimeException constructor.
	Message string
}

// NewMathException answers to `new MathException($message)`.
func NewMathException(message string) *MathException {
	return &MathException{Message: message}
}

// Error is the getMessage of the PHP exception, under the name Go looks for.
func (e *MathException) Error() string { return e.Message }
