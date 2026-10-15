package accounttest

import (
	"errors"
	"testing"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/auth/account"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/otp"
)

func challengeCases[U account.Account]() []testCase[U] {
	return []testCase[U]{
		{"TheChallengeRefusesTheRightCodeOnceTheBudgetIsSpent", challengeBudgetSpent[U]},
		{"TheChallengeBudgetBelongsToTheAccountAndNotToTheSession", challengeBudgetPerAccount[U]},
		{"ASuccessfulChallengeGivesTheBudgetBack", challengeSuccessRefunds[U]},
		{"AChallengeThatCannotBeCountedIsRefused", challengeUncounted[U]},
	}
}

// challengeFixture is enrolled accounts, the secret their authenticators hold,
// and the store their challenge attempts are counted in.
type challengeFixture[U account.Account] struct {
	users  *account.Users[U]
	ids    map[string]string
	secret []byte
	store  cache.Store
}

func newChallenge[U account.Account](t *testing.T, e *env[U], names ...string) challengeFixture[U] {
	t.Helper()
	users := e.users(account.Hooks[U]{}, nil)
	secret := otp.NewSecret()
	sealed := sealSecret(t, secret)
	ids := map[string]string{}
	for _, name := range names {
		user := e.register(users, tenantA, name, name+"@example.test")
		e.seedFactor(tenantA, user.AccountID(), sealed, true)
		ids[name] = user.AccountID()
	}
	return challengeFixture[U]{users: users, ids: ids, secret: secret, store: cache.NewArrayStore()}
}

// factors is one instance of the application -- one replica, or the code
// behind one browser's session. Every instance built from a fixture counts in
// the same store, which is what a deployment shares.
func (f challengeFixture[U]) factors(e *env[U]) *account.Factors { return e.factors(f.users, f.store) }

// challengeBudgetSpent: five wrong codes and the sixth attempt is refused even
// though it is the right one. A budget that a correct guess could still slip
// past is a budget that only slows the guessing down. Signing in again does
// not hand the budget back: it belongs to the account.
func challengeBudgetSpent[U account.Account](t *testing.T, e *env[U]) {
	fixture := newChallenge(t, e, "ana")
	factors := fixture.factors(e)
	id := fixture.ids["ana"]
	right, wrong := codes(t, fixture.secret)

	for attempt := 1; attempt < account.ChallengeAttempts; attempt++ {
		err := factors.VerifyAuthenticator(e.ctx, tenantA, id, wrong)
		if !errors.Is(err, twofactor.ErrInvalidCode) || errors.Is(err, account.ErrTwoFactorLocked) {
			t.Fatalf("wrong code %d answered %v, want an invalid code that is not yet locked", attempt, err)
		}
	}
	err := factors.VerifyAuthenticator(e.ctx, tenantA, id, wrong)
	if !errors.Is(err, account.ErrTwoFactorLocked) {
		t.Fatalf("wrong code %d answered %v, want the challenge locked", account.ChallengeAttempts, err)
	}
	// A caller written before the lock existed still refuses it as a wrong code.
	if !errors.Is(err, twofactor.ErrInvalidCode) {
		t.Errorf("the lock does not match twofactor.ErrInvalidCode: %v", err)
	}
	var locked account.TwoFactorLockedError
	if !errors.As(err, &locked) || locked.Seconds() <= 0 {
		t.Errorf("the lock carries no retry delay: %v", err)
	}
	if err := factors.VerifyAuthenticator(e.ctx, tenantA, id, right); !errors.Is(err, account.ErrTwoFactorLocked) {
		t.Fatalf("the right code after the budget was spent answered %v, want the challenge locked", err)
	}
	if err := factors.ConsumeRecovery(e.ctx, tenantA, id, "ABCDE-FGHJK"); !errors.Is(err, account.ErrTwoFactorLocked) {
		t.Fatalf("a recovery code after the budget was spent answered %v, want the challenge locked", err)
	}
	if required, err := factors.Required(e.ctx, tenantA, id); err != nil || !required {
		t.Fatalf("Required = %v, %v", required, err)
	}
	if err := factors.VerifyAuthenticator(e.ctx, tenantA, id, right); !errors.Is(err, account.ErrTwoFactorLocked) {
		t.Fatalf("the right code after signing in again answered %v, want the challenge still locked", err)
	}
	// The refused right code never reached the replay guard, so the step it
	// belongs to is unspent.
	if step := e.count(`SELECT last_used_step FROM user_two_factor WHERE user_id = ?`, id); step != 0 {
		t.Fatalf("a locked challenge spent time step %d", step)
	}
}

// challengeBudgetPerAccount: two instances stand for two browsers -- or two
// replicas -- each carrying a pending sign-in for the same account.
// Alternating between them does not buy a second budget, and the budget of
// one account is not spent by another's.
func challengeBudgetPerAccount[U account.Account](t *testing.T, e *env[U]) {
	fixture := newChallenge(t, e, "ana", "bea")
	first, second := fixture.factors(e), fixture.factors(e)
	right, wrong := codes(t, fixture.secret)

	for attempt := 1; attempt <= account.ChallengeAttempts; attempt++ {
		instance := first
		if attempt%2 == 0 {
			instance = second
		}
		_ = instance.VerifyAuthenticator(e.ctx, tenantA, fixture.ids["ana"], wrong)
	}
	for name, instance := range map[string]*account.Factors{"first": first, "second": second} {
		if err := instance.VerifyAuthenticator(e.ctx, tenantA, fixture.ids["ana"], right); !errors.Is(err, account.ErrTwoFactorLocked) {
			t.Fatalf("the %s session answered %v after the account spent its budget across both", name, err)
		}
	}
	if err := first.VerifyAuthenticator(e.ctx, tenantA, fixture.ids["bea"], right); err != nil {
		t.Fatalf("another account of the same tenant was refused: %v", err)
	}
	if err := first.VerifyAuthenticator(e.ctx, tenantB, fixture.ids["ana"], right); errors.Is(err, account.ErrTwoFactorLocked) {
		t.Fatalf("the same user id in another tenant was locked: %v", err)
	}
}

// challengeSuccessRefunds: somebody who mistyped twice and then got in has not
// used up anything the next sign-in needs.
func challengeSuccessRefunds[U account.Account](t *testing.T, e *env[U]) {
	fixture := newChallenge(t, e, "ana")
	factors := fixture.factors(e)
	id := fixture.ids["ana"]
	right, wrong := codes(t, fixture.secret)

	for range account.ChallengeAttempts - 1 {
		_ = factors.VerifyAuthenticator(e.ctx, tenantA, id, wrong)
	}
	if err := factors.VerifyAuthenticator(e.ctx, tenantA, id, right); err != nil {
		t.Fatalf("the right code within the budget answered %v", err)
	}
	for attempt := 1; attempt < account.ChallengeAttempts; attempt++ {
		if err := factors.VerifyAuthenticator(e.ctx, tenantA, id, wrong); errors.Is(err, account.ErrTwoFactorLocked) {
			t.Fatalf("wrong code %d after a success answered locked: the success did not give the budget back", attempt)
		}
	}
}

// challengeUncounted: the request throttle fails open because it guards
// capacity; this guards an account, and an attempt nobody counted is an
// attempt outside the budget.
func challengeUncounted[U account.Account](t *testing.T, e *env[U]) {
	fixture := newChallenge(t, e, "ana")
	factors := e.factors(fixture.users, failingStore{cache.NewArrayStore()})
	right, _ := codes(t, fixture.secret)
	if err := factors.VerifyAuthenticator(e.ctx, tenantA, fixture.ids["ana"], right); err == nil {
		t.Fatal("the right code was accepted with no store to count the attempt in")
	}
	if step := e.count(`SELECT last_used_step FROM user_two_factor WHERE user_id = ?`, fixture.ids["ana"]); step != 0 {
		t.Fatalf("an uncounted challenge spent time step %d", step)
	}
}
