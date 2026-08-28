package twofactor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/arandu-io/hesape/otp"
)

var (
	// ErrInvalidCode is a code that does not authenticate. Every reason it does
	// not unwraps to this, because the person at the keyboard is told the same
	// thing either way and telling them more is telling whoever is guessing
	// more.
	ErrInvalidCode = errors.New("twofactor: the code is not valid")

	// ErrReplayed is a code that was right and has already been spent. It
	// unwraps to [ErrInvalidCode], so a caller that does not care about the
	// difference does not have to check for both -- and one that does can log
	// it, because a replay is somebody else's attempt and not a typo.
	ErrReplayed = fmt.Errorf("%w: it has already been used", ErrInvalidCode)

	// ErrNotConfigured is a check that cannot be made: no replay guard, or no
	// subject to remember the code against.
	//
	// It is not a variety of [ErrInvalidCode], and that is the point. A
	// misconfigured check must not read as a wrong code, because a wrong code is
	// a thing an application shrugs at and tries again.
	ErrNotConfigured = errors.New("twofactor: the check cannot be made")
)

// ReplayGuard remembers which time steps a subject has already spent.
//
// It exists because a correct code stays correct for the whole of its time
// step: without this, a code read over somebody's shoulder, or captured by
// whatever put the phishing page in front of them, works for as long as it is
// still on the screen.
//
// It is an interface and not an implementation because the memory belongs to
// whoever owns the account. This package stores nothing, and a store it shipped
// would be a second place where the shape of an account is decided.
type ReplayGuard interface {
	// Spend records step as used by subject and reports whether it was still
	// free -- true the first time, false every time after.
	//
	// It must be atomic. A read followed by a write at the call site is exactly
	// the race this interface exists to close, and two requests arriving
	// together with the same stolen code is the case that matters.
	//
	// It must fail rather than guess. An implementation that cannot reach its
	// storage returns an error, and the verification is refused; there is no
	// path on which storage being down lets a code through, because that path
	// is the one an attacker would create on purpose.
	//
	// The subject is opaque here: it is whatever identifies the account to the
	// caller. It must not be empty, and two accounts must never share one.
	Spend(ctx context.Context, subject string, step uint64) (bool, error)
}

// Authenticator checks the codes an authenticator application produces.
//
// It holds no secret and no account. The secret is handed to
// [Authenticator.Verify] by whoever stored it, and the memory of which codes
// have been spent is the [ReplayGuard]'s.
type Authenticator struct {
	// TOTP is the configuration the enrolment was provisioned under. The zero
	// value means otp.Default, and it has to be the same configuration the
	// provisioning URI carried or the two sides compute different codes.
	TOTP otp.TOTP

	// Guard is where a spent step is recorded. It is required: with no guard
	// there is no way to refuse a second use, so [Authenticator.Verify] refuses
	// everything instead.
	Guard ReplayGuard

	// Now is the clock. A nil Now means time.Now; a test sets it.
	Now func() time.Time
}

// Verify reports whether code authenticates subject against secret.
//
// It returns nil only when the code was one the secret produces in the accepted
// window and the step it belongs to had not been spent. The order is
// deliberate: the code is checked first and the step spent afterwards, so that
// somebody typing wrong codes cannot burn the steps of the person whose account
// it is.
//
// Every failure is refusal. A guard that cannot answer is refused, an empty
// subject is refused, and a configuration that cannot produce codes is refused;
// there is no argument of this function that turns the check off.
func (a Authenticator) Verify(ctx context.Context, subject string, secret []byte, code string) error {
	if a.Guard == nil {
		return fmt.Errorf("%w: no replay guard is configured, and without one a code cannot be spent", ErrNotConfigured)
	}
	if subject == "" {
		return fmt.Errorf("%w: no subject was named, and a spent step has to belong to somebody", ErrNotConfigured)
	}

	now := time.Now
	if a.Now != nil {
		now = a.Now
	}

	step, err := a.TOTP.Verify(secret, NormalizeCode(code), now())
	if err != nil {
		if errors.Is(err, otp.ErrMismatch) {
			return fmt.Errorf("%w: %w", ErrInvalidCode, err)
		}
		// A bad secret or an unusable configuration. It is not the person's
		// mistake and it must not be reported as one.
		return err
	}

	fresh, err := a.Guard.Spend(ctx, subject, step)
	if err != nil {
		return fmt.Errorf("%w: the replay guard could not be consulted: %w", ErrNotConfigured, err)
	}
	if !fresh {
		return ErrReplayed
	}
	return nil
}

// NormalizeCode strips what a person adds around a code and leaves what the
// comparison needs: spaces and hyphens go, and letters are folded up.
//
// Both codes a person types are normalised the same way and for the same
// reason. An authenticator shows six digits in two groups and people copy the
// gap; a recovery code is read off paper and typed with whatever grouping was
// printed. Neither should be refused for how it was spaced.
//
// It is the canonical form. A [RecoveryStore] hashes and compares what this
// returns, so that what was stored at issue and what arrives from a form are
// the same string.
func NormalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' {
			return -1
		}
		return unicode.ToUpper(r)
	}, code)
}
