package twofactor_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/otp"
)

// memoryGuard is a [twofactor.ReplayGuard] that remembers in a map. It is a
// test double and not a store: the package under test ships no store, which is
// the whole point of the interface.
type memoryGuard struct {
	mu    sync.Mutex
	spent map[string]bool
	fail  error
}

func newMemoryGuard() *memoryGuard {
	return &memoryGuard{spent: map[string]bool{}}
}

func (g *memoryGuard) Spend(_ context.Context, subject string, step uint64) (bool, error) {
	if g.fail != nil {
		return false, g.fail
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	key := subject + ":" + strconv.FormatUint(step, 10)
	if g.spent[key] {
		return false, nil
	}
	g.spent[key] = true
	return true, nil
}

// fixedClock returns a clock stopped at at.
func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

// enrolment is a secret and the instant a code is being checked at. The secret
// is the one both RFCs publish their vectors against, so the codes here are the
// same codes those tests assert.
var (
	testSecret = []byte("12345678901234567890")
	testTime   = time.Unix(1111111109, 0).UTC()
)

func currentCode(t *testing.T) string {
	t.Helper()
	code, err := otp.Default().Generate(testSecret, testTime)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return code
}

// TestVerifyAcceptsTheCurrentCodeOnce is the property the replay guard exists
// for: right code, once.
func TestVerifyAcceptsTheCurrentCodeOnce(t *testing.T) {
	auth := twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(testTime)}
	code := currentCode(t)

	if err := auth.Verify(context.Background(), "person", testSecret, code); err != nil {
		t.Fatalf("the first use was refused: %v", err)
	}

	err := auth.Verify(context.Background(), "person", testSecret, code)
	if !errors.Is(err, twofactor.ErrReplayed) {
		t.Errorf("the second use returned %v, and it should be ErrReplayed", err)
	}
	if !errors.Is(err, twofactor.ErrInvalidCode) {
		t.Errorf("ErrReplayed does not unwrap to ErrInvalidCode, so a caller has to check for both")
	}
}

// TestVerifyKeepsSubjectsApart fixes that the memory is per account. A guard
// keyed on the step alone would let the first person to sign in each half
// minute lock everybody else out.
func TestVerifyKeepsSubjectsApart(t *testing.T) {
	auth := twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(testTime)}
	code := currentCode(t)

	if err := auth.Verify(context.Background(), "first", testSecret, code); err != nil {
		t.Errorf("the first subject was refused: %v", err)
	}
	if err := auth.Verify(context.Background(), "second", testSecret, code); err != nil {
		t.Errorf("the second subject was refused because the first had used the step: %v", err)
	}
}

// TestVerifyRefusesWhatIsNotTheCode covers the wrong-code path and proves the
// step is not spent by it -- otherwise anybody could burn the account holder's
// step by typing rubbish.
func TestVerifyRefusesWhatIsNotTheCode(t *testing.T) {
	guard := newMemoryGuard()
	auth := twofactor.Authenticator{Guard: guard, Now: fixedClock(testTime)}

	for name, code := range map[string]string{
		"empty":     "",
		"too short": "12345",
		"wrong":     "000000",
		"letters":   "abcdef",
	} {
		err := auth.Verify(context.Background(), "person", testSecret, code)
		if !errors.Is(err, twofactor.ErrInvalidCode) {
			t.Errorf("%s: Verify returned %v, and it should be ErrInvalidCode", name, err)
		}
		if errors.Is(err, twofactor.ErrReplayed) {
			t.Errorf("%s: a wrong code was reported as a replay", name)
		}
	}

	// The real code still works, so none of the above spent its step.
	if err := auth.Verify(context.Background(), "person", testSecret, currentCode(t)); err != nil {
		t.Errorf("the right code was refused after the wrong ones: %v", err)
	}
}

// TestVerifyAcceptsTheCodeAsAPersonTypesIt covers the grouping an authenticator
// puts on the screen.
func TestVerifyAcceptsTheCodeAsAPersonTypesIt(t *testing.T) {
	code := currentCode(t)
	for name, typed := range map[string]string{
		"as shown":      code,
		"with a space":  code[:3] + " " + code[3:],
		"with a hyphen": code[:3] + "-" + code[3:],
		"padded":        "  " + code + " ",
	} {
		auth := twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(testTime)}
		if err := auth.Verify(context.Background(), "person", testSecret, typed); err != nil {
			t.Errorf("%s (%q): %v", name, typed, err)
		}
	}
}

// TestVerifyFailsClosed is the security property that separates this from a
// convenience wrapper: nothing about a broken configuration or an unreachable
// guard lets a code through.
func TestVerifyFailsClosed(t *testing.T) {
	code := currentCode(t)
	ctx := context.Background()

	t.Run("no guard", func(t *testing.T) {
		auth := twofactor.Authenticator{Now: fixedClock(testTime)}
		err := auth.Verify(ctx, "person", testSecret, code)
		if !errors.Is(err, twofactor.ErrNotConfigured) {
			t.Errorf("Verify returned %v, and it should be ErrNotConfigured", err)
		}
		if errors.Is(err, twofactor.ErrInvalidCode) {
			t.Error("a missing guard was reported as a wrong code, which an application retries")
		}
	})

	t.Run("no subject", func(t *testing.T) {
		auth := twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(testTime)}
		if err := auth.Verify(ctx, "", testSecret, code); !errors.Is(err, twofactor.ErrNotConfigured) {
			t.Errorf("Verify returned %v, and it should be ErrNotConfigured", err)
		}
	})

	t.Run("the guard cannot answer", func(t *testing.T) {
		unreachable := errors.New("the store is not reachable")
		guard := newMemoryGuard()
		guard.fail = unreachable

		auth := twofactor.Authenticator{Guard: guard, Now: fixedClock(testTime)}
		err := auth.Verify(ctx, "person", testSecret, code)
		if err == nil {
			t.Fatal("a code was accepted while the replay guard was unreachable")
		}
		if !errors.Is(err, unreachable) {
			t.Errorf("Verify returned %v, and it should carry the guard's own error", err)
		}
		if !errors.Is(err, twofactor.ErrNotConfigured) {
			t.Errorf("Verify returned %v, and it should be ErrNotConfigured", err)
		}
	})

	t.Run("no secret", func(t *testing.T) {
		auth := twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(testTime)}
		err := auth.Verify(ctx, "person", nil, code)
		if !errors.Is(err, otp.ErrSecret) {
			t.Errorf("Verify returned %v, and it should be ErrSecret", err)
		}
		if errors.Is(err, twofactor.ErrInvalidCode) {
			t.Error("a missing secret was reported as a wrong code, and it is not the person's mistake")
		}
	})

	t.Run("a configuration that cannot produce codes", func(t *testing.T) {
		auth := twofactor.Authenticator{
			TOTP:  otp.TOTP{Digits: 9, Period: otp.DefaultPeriod},
			Guard: newMemoryGuard(),
			Now:   fixedClock(testTime),
		}
		if err := auth.Verify(ctx, "person", testSecret, code); !errors.Is(err, otp.ErrDigits) {
			t.Errorf("Verify returned %v, and it should be ErrDigits", err)
		}
	})
}

// TestVerifyUsesTheClockItWasGiven proves Now is read rather than decorative: a
// code from one instant does not authenticate at another.
func TestVerifyUsesTheClockItWasGiven(t *testing.T) {
	code := currentCode(t)
	elsewhere := testTime.Add(10 * time.Minute)

	auth := twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(elsewhere)}
	if err := auth.Verify(context.Background(), "person", testSecret, code); !errors.Is(err, twofactor.ErrInvalidCode) {
		t.Errorf("a code from ten minutes earlier returned %v, and it should be ErrInvalidCode", err)
	}

	auth = twofactor.Authenticator{Guard: newMemoryGuard(), Now: fixedClock(testTime)}
	if err := auth.Verify(context.Background(), "person", testSecret, code); err != nil {
		t.Errorf("the same code at its own instant returned %v", err)
	}
}

// TestNormalizeCode fixes the canonical form, which is what a store hashes.
func TestNormalizeCode(t *testing.T) {
	cases := map[string]string{
		"123 456":     "123456",
		"123-456":     "123456",
		"  123456\t":  "123456",
		"abcde-fghjk": "ABCDEFGHJK",
		"ABCDE FGHJK": "ABCDEFGHJK",
		"":            "",
		" 123456 ":    "123456",
	}
	for in, want := range cases {
		if got := twofactor.NormalizeCode(in); got != want {
			t.Errorf("NormalizeCode(%q) returned %q, and it should be %q", in, got, want)
		}
	}
}
