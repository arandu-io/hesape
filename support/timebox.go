package support

import "time"

// Timebox answers to Illuminate\Support\Timebox: run a callback and do not
// come back before the given number of microseconds has passed, so that the
// time a check took says nothing about its result.
type Timebox struct {
	// EarlyReturn answers to the $earlyReturn property.
	EarlyReturn bool
}

// NewTimebox is the `new Timebox` the PHP writes, which has no constructor of
// its own.
func NewTimebox() *Timebox {
	return &Timebox{}
}

// Call answers to Timebox::call: invoke the callback inside the timebox and
// return what it returned. An error from the callback is held until the box
// has been waited out, then returned, the way the PHP rethrows.
func (t *Timebox) Call(callback func(*Timebox) (any, error), microseconds int) (any, error) {
	start := time.Now()

	result, err := callback(t)

	remainder := time.Duration(microseconds)*time.Microsecond - time.Since(start)

	if !t.EarlyReturn && remainder > 0 {
		time.Sleep(remainder)
	}

	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReturnEarly answers to Timebox::returnEarly.
func (t *Timebox) ReturnEarly() *Timebox {
	t.EarlyReturn = true
	return t
}

// DontReturnEarly answers to Timebox::dontReturnEarly.
func (t *Timebox) DontReturnEarly() *Timebox {
	t.EarlyReturn = false
	return t
}
