package auth_test

import (
	"context"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// member is a user model that embeds the trait, the way a Laravel model writes
// `use MustVerifyEmail`.
type member struct {
	auth.MustVerifyEmailTrait

	saves int
	sent  []any
}

func newMember() *member {
	m := &member{}
	m.Email = "person@example.com"
	m.VerifyEmail = "verify-email-notification"
	m.Save = func(context.Context) error { m.saves++; return nil }
	m.Notify = func(_ context.Context, notification any) error {
		m.sent = append(m.sent, notification)
		return nil
	}
	return m
}

func TestAnAddressStartsUnverifiedAndIsStampedWhenItIsVerified(t *testing.T) {
	m := newMember()

	if m.HasVerifiedEmail() {
		t.Fatal("a fresh account claims a verified address")
	}
	if m.GetEmailForVerification() != "person@example.com" {
		t.Fatalf("the address is %q", m.GetEmailForVerification())
	}

	if err := m.MarkEmailAsVerified(context.Background()); err != nil {
		t.Fatalf("MarkEmailAsVerified answered %v", err)
	}
	if !m.HasVerifiedEmail() || m.EmailVerifiedAt.IsZero() {
		t.Fatal("the address was not stamped")
	}
	if m.saves != 1 {
		t.Fatalf("the model was saved %d times, want 1", m.saves)
	}

	if err := m.MarkEmailAsUnverified(context.Background()); err != nil {
		t.Fatalf("MarkEmailAsUnverified answered %v", err)
	}
	if m.HasVerifiedEmail() || !m.EmailVerifiedAt.IsZero() {
		t.Fatal("the stamp survived being taken off")
	}
	if m.saves != 2 {
		t.Fatalf("the model was saved %d times, want 2", m.saves)
	}
}

func TestTheVerificationNotificationGoesThroughTheModel(t *testing.T) {
	m := newMember()

	if err := m.SendEmailVerificationNotification(context.Background()); err != nil {
		t.Fatalf("SendEmailVerificationNotification answered %v", err)
	}
	if len(m.sent) != 1 || m.sent[0] != "verify-email-notification" {
		t.Fatalf("the notification that was sent is %v", m.sent)
	}
}

func TestATraitWithNoModelBehindItSaysSoInsteadOfLosingTheWrite(t *testing.T) {
	var alone auth.MustVerifyEmailTrait

	if err := alone.MarkEmailAsVerified(context.Background()); err == nil {
		t.Fatal("a trait with no save reported success and wrote nothing")
	}
	if err := alone.SendEmailVerificationNotification(context.Background()); err == nil {
		t.Fatal("a trait with no notify reported that it sent something")
	}
}

func TestTheTraitSatisfiesTheContract(t *testing.T) {
	var contract auth.MustVerifyEmail = newMember()

	if contract.HasVerifiedEmail() {
		t.Fatal("a fresh account claims a verified address")
	}
}
