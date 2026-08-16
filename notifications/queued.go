package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// SendQueuedNotifications is the job that sends a notification from a worker.
//
// It is what "send this later" means here, and nothing decides it for you: the
// choice is which of the two calls you write, so a call that blocks for two
// seconds looks different from one that does not.
//
//	notifier.Send(ctx, g, user, InvoicePaid{...})            // now
//	queue.Push(ctx, g, "", job.Name, job.Payload)            // later
//
// The value carries the recipients, the notification and the channels. What it
// does not carry is a serialized model: the recipients are their identities,
// and a worker that needs more than that loads it.
type SendQueuedNotifications struct {
	// Notifiables is who to reach. One recipient is a slice of one.
	Notifiables []Notifiable
	// Notification is what to send.
	Notification Notification
	// Channels overrides the channels the notification names, or is empty to
	// use them.
	Channels []ChannelName
	// Tries is how many attempts the worker gives it, Timeout is how long one
	// attempt may run, and MaxExceptions is how many unhandled failures are
	// allowed before the job is given up on. Zero means "whatever the worker is
	// configured to do".
	Tries         int
	Timeout       time.Duration
	MaxExceptions int
	// BackoffFor is how long a released notification waits before it is
	// available again, and RetryUntilAt is when the worker stops retrying.
	//
	// The fields and the methods of a Go type share one namespace, so the
	// methods keep the short names -- Backoff, RetryUntil -- and the fields say
	// what they hold.
	BackoffFor   time.Duration
	RetryUntilAt time.Time
	// OnFailure is called when the worker gives up, after Failed has been told.
	// It is a function rather than an optional method on the notification,
	// because a notification is an interface and every method on an interface
	// is one every notification has to write.
	OnFailure func(cause error)
}

// Handle sends the notification, and is what the worker calls. It keeps going
// after a recipient fails and joins the errors.
//
// The Notifier is an argument, along with the ctx the I/O needs and the Grant
// every send takes.
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
// It is the [Key], which was chosen to be stable, rather than a type name that
// changes when somebody moves a type between packages.
func (j SendQueuedNotifications) DisplayName() string {
	if j.Notification == nil {
		return "notification"
	}
	return string(j.Notification.Key())
}

// Failed is SendQueuedNotifications::failed, called when the worker gives up on
// the job.
func (j SendQueuedNotifications) Failed(cause error) {
	if j.OnFailure != nil {
		j.OnFailure(cause)
	}
}

// Backoff is SendQueuedNotifications::backoff: how long a released notification
// waits before a worker may take it again. Zero means the worker's own default.
func (j SendQueuedNotifications) Backoff() time.Duration { return j.BackoffFor }

// RetryUntil is SendQueuedNotifications::retryUntil: when the worker stops
// retrying. The zero time means the worker's own limit stands.
func (j SendQueuedNotifications) RetryUntil() time.Time { return j.RetryUntilAt }
