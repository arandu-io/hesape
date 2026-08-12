package events_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/auth/events"
)

// account satisfies every user contract the events are typed against, so that
// one value can be put in all thirteen.
type account struct{ id string }

func (a *account) GetAuthIdentifierName() string { return "id" }
func (a *account) GetAuthIdentifier() any        { return a.id }
func (a *account) GetAuthPasswordName() string   { return "password" }
func (a *account) GetAuthPassword() string       { return "" }
func (a *account) GetRememberToken() string      { return "" }
func (a *account) SetRememberToken(string)       {}
func (a *account) GetRememberTokenName() string  { return "remember_token" }

func (a *account) GetEmailForPasswordReset() string                            { return "ada@example.com" }
func (a *account) SendPasswordResetNotification(context.Context, string) error { return nil }

func (a *account) HasVerifiedEmail() bool                                  { return true }
func (a *account) MarkEmailAsVerified(context.Context) error               { return nil }
func (a *account) SendEmailVerificationNotification(context.Context) error { return nil }
func (a *account) GetEmailForVerification() string                         { return "ada@example.com" }

// TestEveryEventCarriesWhatThePHPConstructorTakes is a compile-time check
// spelled as a test: each struct is built with every field named, so a field
// that is renamed or dropped fails here rather than in whatever dispatches it.
func TestEveryEventCarriesWhatThePHPConstructorTakes(t *testing.T) {
	user := &account{id: "7"}
	credentials := map[string]any{"email": "ada@example.com", "password": "secret"}
	request := httptest.NewRequest("POST", "/login", nil)

	if e := (events.Attempting{Guard: "web", Credentials: credentials, Remember: true}); !e.Remember || e.Guard != "web" {
		t.Fatalf("Attempting = %+v", e)
	}
	if e := (events.Authenticated{Guard: "web", User: user}); e.User != auth.Authenticatable(user) {
		t.Fatalf("Authenticated = %+v", e)
	}
	if e := (events.CurrentDeviceLogout{Guard: "web", User: user}); e.Guard != "web" {
		t.Fatalf("CurrentDeviceLogout = %+v", e)
	}
	if e := (events.Failed{Guard: "web", User: nil, Credentials: credentials}); e.User != nil {
		t.Fatalf("Failed = %+v: a failure with no matching account carries no user", e)
	}
	if e := (events.Lockout{Request: request}); e.Request != request {
		t.Fatalf("Lockout = %+v", e)
	}
	if e := (events.Login{Guard: "web", User: user, Remember: true}); !e.Remember {
		t.Fatalf("Login = %+v", e)
	}
	if e := (events.Logout{Guard: "web", User: user}); e.Guard != "web" {
		t.Fatalf("Logout = %+v", e)
	}
	if e := (events.OtherDeviceLogout{Guard: "web", User: user}); e.Guard != "web" {
		t.Fatalf("OtherDeviceLogout = %+v", e)
	}
	if e := (events.PasswordReset{User: user}); e.User == nil {
		t.Fatalf("PasswordReset = %+v", e)
	}
	if e := (events.PasswordResetLinkSent{User: user}); e.User.GetEmailForPasswordReset() == "" {
		t.Fatalf("PasswordResetLinkSent = %+v", e)
	}
	if e := (events.Registered{User: user}); e.User == nil {
		t.Fatalf("Registered = %+v", e)
	}
	if e := (events.Validated{Guard: "web", User: user}); e.Guard != "web" {
		t.Fatalf("Validated = %+v", e)
	}
	if e := (events.Verified{User: user}); !e.User.HasVerifiedEmail() {
		t.Fatalf("Verified = %+v", e)
	}
}
