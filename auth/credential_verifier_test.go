package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
)

var _ func(auth.UserProvider, func() auth.Timebox, bool, int) *auth.CredentialVerifier = auth.NewCredentialVerifier

func TestCredentialVerifierReturnsTheUserWithoutCreatingIdentityOrSession(t *testing.T) {
	provider := &fakeProvider{
		user:     &fakeUser{id: 7, password: "hashed:secret"},
		email:    "person@example.com",
		password: "secret",
	}
	session := newFakeSession()
	jar := newFakeCookieJar()
	dispatcher := &fakeDispatcher{}
	guard := auth.NewSessionGuard("web", provider, session, newFakeRequest(), &recordingTimebox{}, true, 1, "app-key")
	guard.SetCookieJar(jar)
	guard.SetDispatcher(dispatcher)
	timebox := &recordingTimebox{}

	verifier := auth.NewCredentialVerifier(provider, func() auth.Timebox {
		return timebox
	}, false, 1)

	user, err := verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("Verify refused the right password: %v", err)
	}
	if user != provider.user {
		t.Fatal("Verify returned somebody other than the validated user")
	}
	if guard.HasUser() || guard.GetUser() != nil {
		t.Fatal("Verify created request-local identity")
	}
	if len(session.data) != 0 || session.regenerated != 0 {
		t.Fatalf("Verify wrote or regenerated the session: data=%v regenerated=%d", session.data, session.regenerated)
	}
	if len(jar.queued) != 0 {
		t.Fatalf("Verify queued a cookie: %v", jar.queued)
	}
	if len(dispatcher.dispatched) != 0 {
		t.Fatalf("Verify fired guard events: %#v", dispatcher.dispatched)
	}
	if timebox.calls != 1 || !timebox.earlyAsked {
		t.Fatalf("the successful verification used the timebox incorrectly: %#v", timebox)
	}
	if len(provider.rehashes) != 0 {
		t.Fatalf("a verifier with rehash disabled still rewrote the password: %#v", provider.rehashes)
	}
	if provider.credentialLookups != 1 || provider.credentialValidates != 1 {
		t.Fatalf("Verify looked up or validated the account more than once: lookups=%d validations=%d", provider.credentialLookups, provider.credentialValidates)
	}
	if len(provider.tokensWritten) != 0 || provider.user.GetRememberToken() != "" {
		t.Fatalf("Verify created remember state: writes=%v token=%q", provider.tokensWritten, provider.user.GetRememberToken())
	}
}

func TestCredentialVerifierRehashesOnlyAfterSuccessAndDoesNotRefuseARehashFailure(t *testing.T) {
	rehashFailure := errors.New("password store unavailable")
	provider := &fakeProvider{
		user:      &fakeUser{id: 7, password: "hashed:secret"},
		email:     "person@example.com",
		password:  "secret",
		rehashErr: rehashFailure,
	}
	verifier := auth.NewCredentialVerifier(provider, func() auth.Timebox {
		return &recordingTimebox{}
	}, true, 1)

	user, err := verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "secret",
	})
	if err != nil || user != provider.user {
		t.Fatalf("Verify answered (%v, %v), want the user despite the rehash failure", user, err)
	}
	if len(provider.rehashes) != 1 || provider.rehashes[0].user != provider.user || provider.rehashes[0].force {
		t.Fatalf("the successful verification rehashed incorrectly: %#v", provider.rehashes)
	}

	_, err = verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "wrong",
	})
	if err != auth.ErrInvalidCredentials {
		t.Fatalf("Verify answered %v for the wrong password, want ErrInvalidCredentials", err)
	}
	if len(provider.rehashes) != 1 {
		t.Fatalf("the refused verification rehashed the password: %#v", provider.rehashes)
	}
}

func TestCredentialVerifierPreservesAProviderErrorAfterTheTimebox(t *testing.T) {
	storageFailure := errors.New("user store unavailable")
	provider := &fakeProvider{retrieveCredentialsErr: storageFailure}
	timebox := &recordingTimebox{}
	verifier := auth.NewCredentialVerifier(provider, func() auth.Timebox {
		return timebox
	}, true, 17000)

	user, err := verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "secret",
	})
	if user != nil || !errors.Is(err, storageFailure) {
		t.Fatalf("Verify answered (%v, %v), want the provider error", user, err)
	}
	if timebox.calls != 1 || timebox.microsecs != 17000 || timebox.earlyAsked {
		t.Fatalf("the provider error did not take the refusal timebox path: %#v", timebox)
	}
	if provider.credentialValidates != 0 {
		t.Fatal("Verify validated credentials after their account lookup failed")
	}
	if provider.credentialLookups != 1 {
		t.Fatalf("Verify looked up credentials %d times after the provider error, want 1", provider.credentialLookups)
	}
	if len(provider.rehashes) != 0 {
		t.Fatalf("Verify rehashed after the account lookup failed: %#v", provider.rehashes)
	}
}

func TestCredentialVerifierMakesAnAbsentAccountAndAWrongPasswordIndistinguishable(t *testing.T) {
	provider := &fakeProvider{
		user:     &fakeUser{id: 7, password: "hashed:secret"},
		email:    "person@example.com",
		password: "secret",
	}
	var timeboxes []*recordingTimebox
	verifier := auth.NewCredentialVerifier(provider, func() auth.Timebox {
		timebox := &recordingTimebox{}
		timeboxes = append(timeboxes, timebox)
		return timebox
	}, true, 0)

	wrongUser, wrongErr := verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "wrong",
	})
	missingUser, missingErr := verifier.Verify(context.Background(), map[string]any{
		"email":    "nobody@example.com",
		"password": "secret",
	})

	if wrongUser != nil || missingUser != nil {
		t.Fatalf("a refusal returned a user: wrong=%v missing=%v", wrongUser, missingUser)
	}
	if wrongErr != auth.ErrInvalidCredentials || missingErr != auth.ErrInvalidCredentials {
		t.Fatalf("refusals differ: wrong=%v missing=%v", wrongErr, missingErr)
	}
	if len(timeboxes) != 2 || timeboxes[0] == timeboxes[1] {
		t.Fatalf("Verify did not ask the factory for one fresh timebox per call: %#v", timeboxes)
	}
	for _, timebox := range timeboxes {
		if timebox.calls != 1 || timebox.microsecs != 200000 || timebox.earlyAsked {
			t.Fatalf("a refusal did not take the default timebox path: %#v", timebox)
		}
	}
	if len(provider.rehashes) != 0 {
		t.Fatalf("a refusal rehashed the password: %#v", provider.rehashes)
	}
}

func TestCredentialVerifierWithNoFactoryUsesAFreshTimeboxForEveryCall(t *testing.T) {
	provider := &fakeProvider{
		user:     &fakeUser{id: 7, password: "hashed:secret"},
		email:    "person@example.com",
		password: "secret",
	}
	verifier := auth.NewCredentialVerifier(provider, nil, false, 20000)

	if _, err := verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "secret",
	}); err != nil {
		t.Fatalf("Verify refused the right password: %v", err)
	}

	start := time.Now()
	_, err := verifier.Verify(context.Background(), map[string]any{
		"email":    "person@example.com",
		"password": "wrong",
	})
	elapsed := time.Since(start)
	if err != auth.ErrInvalidCredentials {
		t.Fatalf("Verify answered %v for the wrong password, want ErrInvalidCredentials", err)
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("the refusal after a success took %s: Verify reused the timebox whose early-return state was set", elapsed)
	}
}
