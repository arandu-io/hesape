package twofactor

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

const (
	// RecoveryCodeLength is how many characters a recovery code has.
	//
	// Ten, drawn from a 32-character alphabet, is fifty bits. It has to survive
	// being guessed by whoever already has the password, and it has to survive
	// being copied onto paper by hand.
	RecoveryCodeLength = 10

	// DefaultRecoveryCodes is how many codes an enrolment usually issues. Enough
	// that losing a couple is not losing the account, few enough to write down
	// in one sitting.
	DefaultRecoveryCodes = 8

	// recoveryAlphabet is thirty-two characters with the ambiguous ones left
	// out: no I or 1, no O or 0. Every character of a recovery code is read off
	// paper and typed by somebody who cannot try again very many times.
	//
	// Thirty-two exactly, and that is arithmetic rather than taste: 256 divides
	// by 32, so taking a random byte modulo the alphabet length is uniform. An
	// alphabet of any other size would make some characters likelier than
	// others, and the entropy claimed above would be a claim rather than a fact.
	recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

// ErrRecoveryCount is a number of codes that cannot be issued.
var ErrRecoveryCount = errors.New("twofactor: that number of recovery codes cannot be issued")

// GenerateRecoveryCodes returns n fresh recovery codes in canonical form.
//
// A recovery code is the only way back in when the phone is gone, and it is the
// only fallback there is: nothing here mails a code to an address, because an
// address is not a second factor. Which makes what the caller does with these
// the whole of their security -- they are shown once, they are stored hashed,
// and each is spent once.
//
// The codes come back in the form [NormalizeCode] produces, so the string that
// is hashed at issue is the string a form will produce later. A caller that
// wants to print them in groups may add separators for display; it must not
// store what it printed.
func GenerateRecoveryCodes(n int) ([]string, error) {
	if n <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrRecoveryCount, n)
	}

	// Errors are documented as impossible since Go 1.24: Read either fills the
	// slice entirely or panics. A caller cannot recover from an operating
	// system that has stopped producing randomness, and half a recovery code is
	// worse than none.
	buf := make([]byte, n*RecoveryCodeLength)
	_, _ = rand.Read(buf)

	codes := make([]string, 0, n)
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.Reset()
		b.Grow(RecoveryCodeLength)
		for _, c := range buf[i*RecoveryCodeLength : (i+1)*RecoveryCodeLength] {
			b.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
		}
		codes = append(codes, b.String())
	}
	return codes, nil
}

// RecoveryStore is where recovery codes live between being issued and being
// spent.
//
// It is an interface for the reason [ReplayGuard] is: the codes belong to
// whatever owns the account, and a store shipped here would be a second opinion
// about what an account is. What this package can state is what the store has
// to guarantee, and the guarantees are the security of the mechanism rather
// than implementation advice:
//
//   - The codes are stored hashed, with a password hash and not a bare digest.
//     They are the fallback for the second factor, so a leaked table of them is
//     a leaked table of second factors.
//   - The comparison is in constant time, which a password hash's own verifier
//     already gives.
//   - Spending a code is atomic, and a code spends exactly once. Two requests
//     arriving together with the same code is the case that decides whether
//     "once" is true.
//   - The lookup is scoped to the subject. One person's code must never open
//     another person's account, and a store that searches by code alone makes
//     that happen the first time two codes collide.
//   - The code is compared as [NormalizeCode] returns it, at issue and at use.
//   - Storage that cannot be reached refuses the attempt. There is no path on
//     which the store being down lets somebody in.
type RecoveryStore interface {
	// Consume spends one of subject's recovery codes and reports whether code
	// was one of them and still unspent.
	//
	// False is the ordinary answer for a wrong code and carries no error. An
	// error means the store could not decide, and the caller refuses.
	Consume(ctx context.Context, subject, code string) (bool, error)
}
