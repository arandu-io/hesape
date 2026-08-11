package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// SendQueuedNotifications is the job that sends a notification from a worker.
//
// It is Illuminate's SendQueuedNotifications, and it is what "send this later"
// means here. Illuminate decides between now and later by an interface the
// notification class implements somewhere else, which makes a call that
// sometimes blocks for two seconds and sometimes does not, with nothing at the
// call site to say which. Here the choice is which of the two you write:
//
//	notifier.Send(ctx, g, user, InvoicePaid{...})            // now
//	queue.Push(ctx, g, "", job.Name, job.Payload)            // later
//
// The value carries the recipients, the notification and the channels, exactly
// as the PHP job's three public properties do. What it does not carry is a
// serialized model: the recipients are their identities, and a worker that
// needs more than that loads it.
type SendQueuedNotifications struct {
	// Notifiables is who to reach. Illuminate wraps one or many in a
	// collection; a slice of one is the same thing without the wrapper.
	Notifiables []Notifiable
	// Notification is what to send.
	Notification Notification
	// Channels overrides the channels the notification names, or is empty to
	// use them.
	Channels []ChannelName
	// Tries is how many attempts the worker gives it, Timeout is how long one
	// attempt may run, and MaxExceptions is how many unhandled failures are
	// allowed before the job is given up on. Zero means "whatever the worker is
	// configured to do", which is Illuminate's null.
	Tries         int
	Timeout       time.Duration
	MaxExceptions int
	// BackoffFor is how long a released notification waits before it is
	// available again, and RetryUntilAt is when the worker stops retrying.
	//
	// Illuminate calls them backoff() and retryUntil() and reads a property of
	// the same name; Go has one namespace for the fields and the methods of a
	// type, so the methods keep the names and the fields say what they hold.
	BackoffFor   time.Duration
	RetryUntilAt time.Time
	// OnFailure is called when the worker gives up, after Failed has been told.
	// It is Illuminate's `$notification->failed($e)`, which is a method on the
	// notification found by name; here it is a function, because a notification
	// is an interface and not a base class with optional methods.
	OnFailure func(cause error)
}

// Handle sends the notification. It is what the worker calls.
func (j SendQueuedNotifications) Handle(ctx context.Context, g auth.Grant, n *Notifier) error {
	if n == nil {
		return errors.New("notifications: sending a queued notification needs a notifier")
	}
	var errs []error
	for _, to := range j.Notifiables {
		errs = append(errs, n.SendNow(ctx, g, to, j.Notification, j.Channels...))
	}
	return errors.Join(errs...)
}

// DisplayName is what the job is called on a dashboard and in a log line.
//
// Illuminate answers with the notification's class name. Here it is the Key,
// which is the name that was chosen to be stable (see Key) rather than the one
// that changes when somebody moves a type between packages.
func (j SendQueuedNotifications) DisplayName() string {
	if j.Notification == nil {
		return "notification"
	}
	return string(j.Notification.Key())
}

// Failed is called when the worker gives up on the job.
func (j SendQueuedNotifications) Failed(cause error) {
	if j.OnFailure != nil {
		j.OnFailure(cause)
	}
}

// Backoff is how long a released notification waits before a worker may take it
// again. Zero means the worker's own default.
func (j SendQueuedNotifications) Backoff() time.Duration { return j.BackoffFor }

// RetryUntil is when the worker stops retrying. The zero time means the
// worker's own limit stands.
func (j SendQueuedNotifications) RetryUntil() time.Time { return j.RetryUntilAt }
