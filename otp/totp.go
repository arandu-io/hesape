package otp

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultPeriod is the length of a time step: thirty seconds, which is what
	// an authenticator application assumes when it is not told otherwise.
	DefaultPeriod = 30 * time.Second

	// DefaultSkew is how many steps either side of the current one are
	// accepted.
	//
	// One, because the phone's clock is not the server's and a person who
	// starts typing at the twenty-ninth second finishes in the next step.
	// Widening it multiplies the codes that are valid at any instant, which is
	// the same as shortening the code.
	DefaultSkew = 1
)

var (
	// ErrPeriod is a time step that cannot be used.
	ErrPeriod = errors.New("otp: the period is not usable")

	// ErrTime is an instant with no time step: RFC 6238 counts steps from the
	// Unix epoch, so nothing before it has one. The zero [time.Time] is the way
	// this is usually reached.
	ErrTime = errors.New("otp: the instant is before the Unix epoch")

	// ErrMismatch is a code that is not one of the codes the secret produces in
	// the accepted window. It says nothing about which of them it was closest
	// to, and there is nothing more to learn from it.
	ErrMismatch = errors.New("otp: the code does not match")
)

// TOTP is the time-based algorithm of RFC 6238: the counter [HOTP] takes is the
// number of whole time steps since the Unix epoch.
//
// The zero value means the defaults -- six digits, a thirty-second step and one
// step of tolerance -- which are the values an authenticator application uses
// when the provisioning URI omits them. Any field set at all means every field
// is read as written, so TOTP{Digits: 8} is a configuration with no period and
// is refused rather than half-guessed.
type TOTP struct {
	// Digits is the code length, between [MinDigits] and [MaxDigits].
	Digits int

	// Period is the length of a time step. It must be a whole number of
	// seconds, because seconds are all a provisioning URI can carry: a period
	// this package honoured but could not tell the phone about would produce
	// codes only one side computes.
	Period time.Duration

	// Skew is how many steps either side of the current one [TOTP.Verify]
	// accepts. Zero accepts only the current step.
	Skew uint
}

// Default returns the configuration an authenticator application assumes: six
// digits, thirty seconds, one step of tolerance.
func Default() TOTP {
	return TOTP{Digits: DefaultDigits, Period: DefaultPeriod, Skew: DefaultSkew}
}

// Validate reports whether the configuration can produce codes, resolving the
// zero value to [Default] first.
func (t TOTP) Validate() error {
	t = t.Resolve()
	if t.Digits < MinDigits || t.Digits > MaxDigits {
		return fmt.Errorf("%w: %d, and it must be between %d and %d", ErrDigits, t.Digits, MinDigits, MaxDigits)
	}
	if t.Period < time.Second || t.Period%time.Second != 0 {
		return fmt.Errorf("%w: %s, and it must be a whole number of seconds, at least one", ErrPeriod, t.Period)
	}
	return nil
}

// Generate returns the code secret produces at the instant at.
func (t TOTP) Generate(secret []byte, at time.Time) (string, error) {
	t = t.Resolve()
	if err := t.Validate(); err != nil {
		return "", err
	}
	step, err := t.step(at)
	if err != nil {
		return "", err
	}
	return HOTP(secret, step, t.Digits)
}

// Verify checks code against the codes secret produces around at, and returns
// the time step the accepted code belongs to.
//
// The step is returned because this package remembers nothing, and a code that
// is correct stays correct for the rest of its step. Whoever owns the account
// must record the returned step and refuse a second code that resolves to it;
// without that, a code read over a shoulder works for as long as it is on the
// screen. A caller that discards the step has a replay window the length of
// [TOTP.Period].
//
// The failure is [ErrMismatch] for every wrong code, whatever was wrong with
// it.
func (t TOTP) Verify(secret []byte, code string, at time.Time) (uint64, error) {
	t = t.Resolve()
	if err := t.Validate(); err != nil {
		return 0, err
	}
	if len(code) != t.Digits {
		// Length is not a secret -- it is the size of the box on the screen --
		// so refusing on it leaks nothing, and it keeps the comparison below
		// over strings of equal length.
		return 0, fmt.Errorf("%w: it is %d characters and a code is %d", ErrMismatch, len(code), t.Digits)
	}

	center, err := t.step(at)
	if err != nil {
		return 0, err
	}

	skew := uint64(t.Skew)
	// The window is clamped at the epoch rather than allowed to wrap: step zero
	// minus one step is not a very large number, it is nothing at all.
	first := center - min(center, skew)

	var matched uint64
	var found bool
	for step := first; step <= center+skew; step++ {
		candidate, err := HOTP(secret, step, t.Digits)
		if err != nil {
			return 0, err
		}
		// hmac.Equal and not ==: a comparison that stops at the first wrong
		// character tells whoever is guessing how much of the guess was right,
		// one character per attempt.
		if hmac.Equal([]byte(candidate), []byte(code)) {
			matched, found = step, true
		}
		// No break. The whole window is walked whether or not something has
		// matched, so how long this takes does not say which step it was.
	}
	if !found {
		return 0, ErrMismatch
	}
	return matched, nil
}

// step returns the number of whole periods between the Unix epoch and at.
func (t TOTP) step(at time.Time) (uint64, error) {
	seconds := at.Unix()
	if seconds < 0 {
		return 0, fmt.Errorf("%w: %s", ErrTime, at.UTC().Format(time.RFC3339))
	}
	return uint64(seconds) / uint64(t.Period/time.Second), nil
}

// Resolve returns the configuration with the zero value replaced by [Default],
// and anything else left exactly as written.
//
// It is exported because whoever builds a provisioning URI has to write the
// digit count and the period into it, and reading them off the struct would
// mean writing this rule out a second time -- in the one place where getting it
// wrong produces a phone configured differently from the server that enrolled
// it.
func (t TOTP) Resolve() TOTP {
	if t == (TOTP{}) {
		return Default()
	}
	return t
}
