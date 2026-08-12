package listeners

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/events"
)

// SendEmailVerificationNotification is
// Illuminate\Auth\Listeners\SendEmailVerificationNotification.
//
// It sends the verification link when an account is registered. It holds
// nothing: the sending is the account's own
// auth.MustVerifyEmail.SendEmailVerificationNotification, which is where the
// application decides what the message says and which mailer carries it.
//
// It is a struct rather than a function so that it reads at the wiring site the
// way Illuminate's listener list does, and so that a project which needs to
// carry a collaborator can add a field without every registration changing
// shape.
type SendEmailVerificationNotification struct{}

// Handle handles the event.
//
// It is SendEmailVerificationNotification::handle. PHP:
//
//	if ($event->user instanceof MustVerifyEmail && ! $event->user->hasVerifiedEmail()) {
//	    $event->user->sendEmailVerificationNotification();
//	}
//
// The context is the fifth mechanical change (see hesape/auth's contracts.go):
// sending goes over the network, and a registration abandoned mid-request must
// not leave a send running with nothing waiting for it.
//
// PHP returns void and lets a failure become an exception the queue worker
// catches. Here the error is returned, because the caller is the dispatcher and
// it is the one that decides whether a verification link that did not go out
// fails the registration or is retried.
//
// An account with nothing to verify, and one that is already verified, are both
// silently nothing -- which is PHP's, and is what lets an application register
// this listener unconditionally.
func (SendEmailVerificationNotification) Handle(ctx context.Context, event events.Registered) error {
	user, ok := event.User.(auth.MustVerifyEmail)
	if !ok || user.HasVerifiedEmail() {
		return nil
	}
	return user.SendEmailVerificationNotification(ctx)
}
