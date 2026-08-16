package mail

import (
	"context"
	"reflect"
	"time"
)

// Factory is the part of a [MailManager] that a queued job needs, which is one
// call.
type Factory interface {
	Mailer(name string) (*Mailer, error)
}

// SendQueuedMailable is the job that sends a mailable on a worker.
type SendQueuedMailable struct {
	// Mailable is the message to send.
	Mailable Mailable

	// Pending is the addressing the call site had collected, so that
	// mailer.To(...).Queue(...) sends to the same people the synchronous call
	// would have. Without it a queued send goes to whoever the mailable itself
	// names, and to nobody when it names no one.
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

// Backoff is how long a released job waits before the next attempt, read off
// the mailable when it declares Backoff() []time.Duration and nil when it does
// not.
func (j *SendQueuedMailable) Backoff() []time.Duration {
	if from, ok := j.Mailable.(interface{ Backoff() []time.Duration }); ok {
		return from.Backoff()
	}
	return nil
}

// RetryUntil is the moment after which the job stops being retried, and the
// zero time when the mailable does not say.
func (j *SendQueuedMailable) RetryUntil() time.Time {
	if from, ok := j.Mailable.(interface{ RetryUntil() time.Time }); ok {
		return from.RetryUntil()
	}
	return time.Time{}
}

// Failed hands the failure to the mailable, if it declares Failed(error).
func (j *SendQueuedMailable) Failed(err error) {
	if to, ok := j.Mailable.(interface{ Failed(error) }); ok {
		to.Failed(err)
	}
}

// DisplayName is what the job is called on a queue dashboard: the mailable's
// type, qualified by its package.
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
