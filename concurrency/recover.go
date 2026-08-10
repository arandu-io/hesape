package concurrency

import (
	"context"
	"fmt"
	"runtime/debug"
)

// PanicError is what a recovered panic becomes.
//
// It exists so that a panic can travel the ordinary error path: the caller
// matches it with errors.As, logs it, retries the job, or lets it reach the
// exception handler like any other failure. Nothing about it is special except
// where it came from.
type PanicError struct {
	// Name is the call site that was guarded: the name given to [Recover], or
	// the position of the task inside [Run]. The stack says where the panic was
	// raised; only this says which piece of work was being done.
	Name string

	// Value is what was passed to panic.
	Value any

	// Stack is the goroutine stack captured where the panic was recovered. It
	// covers the panic site plus the frames of the deferred function that
	// caught it.
	Stack []byte
}

// Error returns the name of the guarded call site and the recovered value.
func (e *PanicError) Error() string {
	if e.Name == "" {
		return fmt.Sprintf("panic: %v", e.Value)
	}
	return fmt.Sprintf("%s: panic: %v", e.Name, e.Value)
}

// Unwrap returns the recovered value when the code panicked with an error, so
// that errors.Is and errors.As reach through the panic to it. It returns nil
// for any other value.
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}

// Recover runs fn and turns a panic into an error.
//
// It is the guard for a call site that runs code it does not own and must not
// die with it: a queue handler, a scheduled task, a pass of the event relay. A
// panic comes back as a [PanicError] carrying name, which should say where the
// work came from -- "queue: send-invoice", "schedule: prune-sessions". The
// stack alone does not say which job was being handled.
//
// If ctx is already done, fn does not run and Recover returns the context
// error. Work cancelled before it started should not start.
func Recover(ctx context.Context, name string, fn func() error) (err error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	defer func() {
		if v := recover(); v != nil {
			err = newPanicError(name, v)
		}
	}()
	return fn()
}

// call runs a task under the same guard as [Recover], with the result type the
// task produces.
func call[T any](ctx context.Context, name string, task Task[T]) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			v, err = zero, newPanicError(name, r)
		}
	}()
	return task(ctx)
}

func newPanicError(name string, v any) *PanicError {
	return &PanicError{Name: name, Value: v, Stack: debug.Stack()}
}

// taskName is how a task without a name of its own appears in a [PanicError].
func taskName(i int) string {
	return fmt.Sprintf("task %d", i)
}
