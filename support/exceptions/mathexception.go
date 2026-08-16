package exceptions

// MathException is the error handed back when a number cannot be written out:
// the spelling, currency and percentage writers return it for a value they
// cannot render.
type MathException struct {
	// Message says why the number could not be written.
	Message string
}

// NewMathException builds a MathException carrying the given message.
func NewMathException(message string) *MathException {
	return &MathException{Message: message}
}

// Error returns the message, so MathException satisfies error.
func (e *MathException) Error() string { return e.Message }
