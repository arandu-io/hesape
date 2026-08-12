package listeners_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/events"
	"github.com/arandu-io/hesape/auth/listeners"
)

// account is an auth.Authenticatable with nothing to verify.
type account struct{ id string }

func (a *account) GetAuthIdentifierName() string { return "id" }
func (a *account) GetAuthIdentifier() any        { return a.id }
func (a *account) GetAuthPasswordName() string   { return "password" }
func (a *account) GetAuthPassword() string       { return "" }
func (a *account) GetRememberToken() string      { return "" }
func (a *account) SetRememberToken(string)       {}
func (a *account) GetRememberTokenName() string  { return "remember_token" }

// verifying is an account that must confirm its address, and records whether it
// was asked to.
type verifying struct {
	*account
	verified bool
	sends    int
	sendErr  error
}

func (v *verifying) HasVerifiedEmail() bool                    { return v.verified }
func (v *verifying) MarkEmailAsVerified(context.Context) error { v.verified = true; return nil }
func (v *verifying) GetEmailForVerification() string           { return "ada@example.com" }
func (v *verifying) SendEmailVerificationNotification(context.Context) error {
	v.sends++
	return v.sendErr
}

func TestANewAccountIsAskedToConfirmItsAddress(t *testing.T) {
	user := &verifying{account: &account{id: "7"}}

	if err := (listeners.SendEmailVerificationNotification{}).Handle(context.Background(), events.Registered{User: user}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if user.sends != 1 {
		t.Fatalf("the link was sent %d times, want once", user.sends)
	}
}

func TestAnAlreadyConfirmedAccountIsNotAskedAgain(t *testing.T) {
	user := &verifying{account: &account{id: "7"}, verified: true}

	if err := (listeners.SendEmailVerificationNotification{}).Handle(context.Background(), events.Registered{User: user}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if user.sends != 0 {
		t.Fatal("a confirmed account was sent a verification link")
	}
}

// TestAnAccountWithNothingToVerifyIsLeftAlone is PHP's `instanceof
// MustVerifyEmail`, and is what lets an application register this listener
// unconditionally.
func TestAnAccountWithNothingToVerifyIsLeftAlone(t *testing.T) {
	var plain auth.Authenticatable = &account{id: "7"}

	if err := (listeners.SendEmailVerificationNotification{}).Handle(context.Background(), events.Registered{User: plain}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

// TestAFailedSendIsReturned is the one deliberate difference from PHP, which
// returns void: the dispatcher is what decides whether a link that did not go
// out fails the registration or is retried, and it cannot decide what it is not
// told.
func TestAFailedSendIsReturned(t *testing.T) {
	wanted := errors.New("the mail transport is down")
	user := &verifying{account: &account{id: "7"}, sendErr: wanted}

	err := (listeners.SendEmailVerificationNotification{}).Handle(context.Background(), events.Registered{User: user})
	if !errors.Is(err, wanted) {
		t.Fatalf("err = %v, want %v", err, wanted)
	}
}
