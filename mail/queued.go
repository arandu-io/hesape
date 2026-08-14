package mail

import (
	"context"
	"reflect"
	"time"
)

// Factory is Illuminate\Contracts\Mail\Factory: the slice of [MailManager] that
// a queued job needs, which is one call.
type Factory interface {
	Mailer(name string) (*Mailer, error)
}

// SendQueuedMailable is Illuminate\Mail\SendQueuedMailable: the job that sends
// a mailable on a worker.
//
// The queue itself is [Queue], declared in this package rather than imported,
// because hesape/queue is being written by another workflow. When it lands, its
// job contract is what this satisfies and the interface here goes away.
type SendQueuedMailable struct {
	// Mailable is the message to send.
	Mailable Mailable

	// Pending is the addressing the call site had collected, so that
	// mailer.To(...).Queue(...) sends to the same people the synchronous call
	// would have. Illuminate does not need this because its addresses are on
	// the mailable itself.
	Pending *PendingMail

	// Tries is how many attempts the job gets.
	Tries int
	// Timeout is how long one attempt may run.
	Timeout time.Duration
	// MaxExceptions is how many unhandled failures are tolerated before the job
	// is failed outright.
	MaxExceptions int
	// ShouldBeEncrypted asks the queue to encrypt the payload.
	ShouldBeEncrypted bool
	// Delay is how long the job waits before its first attempt.
	Delay time.Duration
	// Connection and Queue name where the job goes.
	Connection string
	Queue      string
}

// Handle sends the mailable, and is what the worker calls.
//
// Handle is SendQueuedMailable::handle.
func (j *SendQueuedMailable) Handle(ctx context.Context, factory Factory) error {
	mailer, err := factory.Mailer(j.Connection)
	if err != nil {
		return err
	}
	pending := j.Pending
	if pending == nil {
		pending = &PendingMail{}
	}
	pending.mailer = mailer
	_, err = pending.Send(ctx, j.Mailable)
	return err
}

// Backoff is how long a released job waits before the next attempt.
//
// Illuminate reads a backoff() on the mailable when there is one; a Go mailable
// that declares Backoff() []time.Duration is read the same way.
//
// Backoff is SendQueuedMailable::backoff.
func (j *SendQueuedMailable) Backoff() []time.Duration {
	if from, ok := j.Mailable.(interface{ Backoff() []time.Duration }); ok {
		return from.Backoff()
	}
	return nil
}

// RetryUntil is the moment after which the job stops being retried, and the
// zero time when the mailable does not say.
//
// RetryUntil is SendQueuedMailable::retryUntil.
func (j *SendQueuedMailable) RetryUntil() time.Time {
	if from, ok := j.Mailable.(interface{ RetryUntil() time.Time }); ok {
		return from.RetryUntil()
	}
	return time.Time{}
}

// Failed hands the failure to the mailable, if it wants to know.
//
// Failed is SendQueuedMailable::failed.
func (j *SendQueuedMailable) Failed(err error) {
	if to, ok := j.Mailable.(interface{ Failed(error) }); ok {
		to.Failed(err)
	}
}

// DisplayName is what the job is called on a queue dashboard: the mailable's
// type, which is Illuminate's get_class($this->mailable).
//
// DisplayName is SendQueuedMailable::displayName.
func (j *SendQueuedMailable) DisplayName() string {
	t := reflect.TypeOf(j.Mailable)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return "mail.SendQueuedMailable"
	}
	if t.PkgPath() == "" {
		return t.Name()
	}
	return t.PkgPath() + "." + t.Name()
}
