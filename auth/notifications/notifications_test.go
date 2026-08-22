package notifications_test

import (
	"strings"
	"testing"
	"time"

	authnotifications "github.com/arandu-io/hesape/auth/notifications"
	"github.com/arandu-io/hesape/notifications"
	"github.com/arandu-io/hesape/notifications/messages"
)

// recipient is a notifications.Notifiable with an address.
type recipient struct {
	id      string
	address string
}

func (r recipient) NotifiableID() string   { return r.id }
func (r recipient) NotifiableType() string { return "user" }
func (r recipient) RouteFor(c notifications.ChannelName) string {
	if c == notifications.ChannelMail {
		return r.address
	}
	return ""
}

// restore puts both notifications back on their built-in URLs and messages, so
// that a test setting a callback does not leak into the next one.
func restore(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		authnotifications.ResetPassword{}.CreateUrlUsing(nil)
		authnotifications.ResetPassword{}.ToMailUsing(nil)
		authnotifications.VerifyEmail{}.CreateUrlUsing(nil)
		authnotifications.VerifyEmail{}.ToMailUsing(nil)
	})
}

func TestResetPasswordGoesByMailAndNothingElse(t *testing.T) {
	via := authnotifications.NewResetPassword("tok").Via(recipient{id: "7"})
	if len(via) != 1 || via[0] != notifications.ChannelMail {
		t.Fatalf("via = %v, want mail only", via)
	}
}

func TestResetPasswordCarriesTheLinkAndTheExpiry(t *testing.T) {
	restore(t)
	authnotifications.ResetPassword{}.CreateUrlUsing(func(to notifications.Notifiable, token string) string {
		return "https://app.example.com/reset-password/" + token + "?email=" + to.RouteFor(notifications.ChannelMail)
	})

	n := authnotifications.NewResetPassword("tok-114")
	n.Expire = 15 * time.Minute
	mail := n.ToMail(recipient{id: "7", address: "ada@example.com"})

	if err := mail.Validate(); err != nil {
		t.Fatalf("the message does not validate: %v", err)
	}
	if mail.SubjectLine != "Reset your password" {
		t.Fatalf("subject = %q", mail.SubjectLine)
	}
	if mail.ActionText != "Reset Password" {
		t.Fatalf("action = %q", mail.ActionText)
	}
	if !strings.Contains(mail.ActionURL, "tok-114") {
		t.Fatalf("the link does not carry the token: %q", mail.ActionURL)
	}
	if body := strings.Join(append(mail.IntroLines, mail.OutroLines...), " "); !strings.Contains(body, "15 minutes") {
		t.Fatalf("the message does not say when the link dies: %q", body)
	}
}

// TestResetPasswordDefaultsToTheSixtyMinuteWindow pins that a notification built
// without an expiry says sixty minutes.
func TestResetPasswordDefaultsToTheSixtyMinuteWindow(t *testing.T) {
	restore(t)
	authnotifications.ResetPassword{}.CreateUrlUsing(func(notifications.Notifiable, string) string {
		return "https://app.example.com/reset"
	})

	mail := authnotifications.NewResetPassword("tok").ToMail(recipient{id: "7", address: "ada@example.com"})
	// The expiry line is written after the button, so it lands in OutroLines,
	// which is where a closing sentence belongs.
	body := strings.Join(append(mail.IntroLines, mail.OutroLines...), " ")
	if !strings.Contains(body, "60 minutes") {
		t.Fatalf("body = %q, want the sixty-minute default", body)
	}
}

// TestTheBuiltInResetURLIsARelativePathAndFailsValidation is the deliberate
// loud failure: without CreateUrlUsing there is no host to put in the link, and
// a mail client cannot resolve a path.
func TestTheBuiltInResetURLIsARelativePathAndFailsValidation(t *testing.T) {
	restore(t)

	mail := authnotifications.NewResetPassword("tok-114").
		ToMail(recipient{id: "7", address: "ada@example.com"})

	if !strings.HasPrefix(mail.ActionURL, "/reset-password/tok-114") {
		t.Fatalf("url = %q, want the built-in reset path", mail.ActionURL)
	}
	if !strings.Contains(mail.ActionURL, "email=ada%40example.com") {
		t.Fatalf("url = %q, want the address on the query string, escaped", mail.ActionURL)
	}
	if err := mail.Validate(); err == nil {
		t.Fatal("a message with a relative link validated: nobody would find out until the mail arrived")
	}
}

func TestToMailUsingReplacesTheWholeMessage(t *testing.T) {
	restore(t)
	authnotifications.ResetPassword{}.ToMailUsing(func(_ notifications.Notifiable, token string) messages.Mail {
		return messages.NewMail().Subject("Your link").Line("token " + token)
	})

	mail := authnotifications.NewResetPassword("tok-114").ToMail(recipient{id: "7", address: "ada@example.com"})
	if mail.SubjectLine != "Your link" {
		t.Fatalf("subject = %q, want the one the callback wrote", mail.SubjectLine)
	}
	if len(mail.IntroLines) != 1 || !strings.Contains(mail.IntroLines[0], "tok-114") {
		t.Fatalf("lines = %v", mail.IntroLines)
	}
}

func TestVerifyEmailCarriesTheConfirmationLink(t *testing.T) {
	restore(t)
	authnotifications.VerifyEmail{}.CreateUrlUsing(func(to notifications.Notifiable) string {
		return "https://app.example.com/verify-email/" + to.NotifiableID() + "?signature=abc"
	})

	mail := authnotifications.NewVerifyEmail().ToMail(recipient{id: "7", address: "ada@example.com"})

	if err := mail.Validate(); err != nil {
		t.Fatalf("the message does not validate: %v", err)
	}
	if mail.SubjectLine != "Verify your email address" {
		t.Fatalf("subject = %q", mail.SubjectLine)
	}
	if mail.ActionText != "Verify Email Address" {
		t.Fatalf("action = %q", mail.ActionText)
	}
	if !strings.Contains(mail.ActionURL, "signature=abc") {
		t.Fatalf("url = %q, want the signed one the callback minted", mail.ActionURL)
	}
}

// TestTheBuiltInVerificationURLIsTheIDAndHashShape pins the id-and-hash path the
// built-in URL takes. It is unsigned, and the package comment says why that
// must not reach production.
func TestTheBuiltInVerificationURLIsTheIDAndHashShape(t *testing.T) {
	restore(t)

	mail := authnotifications.NewVerifyEmail().ToMail(recipient{id: "7", address: "ada@example.com"})

	// sha1("ada@example.com").
	const hash = "b6b8fb1a4bfa8d5cb4a2c34f3a1a7f9bf5b0f6a9"
	if !strings.HasPrefix(mail.ActionURL, "/verify-email/7/") {
		t.Fatalf("url = %q, want the built-in verification path", mail.ActionURL)
	}
	if got := strings.TrimPrefix(mail.ActionURL, "/verify-email/7/"); len(got) != len(hash) {
		t.Fatalf("the hash is %q, want a sha1 of the address", got)
	}
	if err := mail.Validate(); err == nil {
		t.Fatal("a message with a relative link validated")
	}
}

// TestVerifyEmailHandsTheURLToTheMessageCallback pins the order: the URL is
// built before ToMailUsing is consulted, so a project that rewrites the message
// still gets the link this notification would have used.
func TestVerifyEmailHandsTheURLToTheMessageCallback(t *testing.T) {
	restore(t)
	authnotifications.VerifyEmail{}.CreateUrlUsing(func(notifications.Notifiable) string {
		return "https://app.example.com/confirm/abc"
	})

	var seen string
	authnotifications.VerifyEmail{}.ToMailUsing(func(_ notifications.Notifiable, verificationURL string) messages.Mail {
		seen = verificationURL
		return messages.NewMail().Subject("Confirm").Action("Confirm", verificationURL)
	})

	authnotifications.NewVerifyEmail().ToMail(recipient{id: "7", address: "ada@example.com"})
	if seen != "https://app.example.com/confirm/abc" {
		t.Fatalf("the callback saw %q", seen)
	}
}

// TestTheKeysAreStableNames is what the stored rows are filtered on, and what a
// rename must not change.
func TestTheKeysAreStableNames(t *testing.T) {
	for _, n := range []notifications.Notification{
		authnotifications.NewResetPassword("tok"),
		authnotifications.NewVerifyEmail(),
	} {
		if !n.Key().Valid() {
			t.Fatalf("%T has the key %q, which nothing will ever match", n, n.Key())
		}
	}
	if got := authnotifications.NewResetPassword("tok").Key(); got != authnotifications.KeyResetPassword {
		t.Fatalf("key = %q", got)
	}
	if got := authnotifications.NewVerifyEmail().Key(); got != authnotifications.KeyVerifyEmail {
		t.Fatalf("key = %q", got)
	}
}
