package twofactor_test

import (
	"errors"
	"strings"
	"testing"

	twofactor "github.com/arandu-io/hesape/2fa"
)

// expectedAlphabet is written out here rather than read from the package, so
// that changing the alphabet is a change to two files and somebody has to mean
// it. Thirty-two characters, with no I, O, 0 or 1.
const expectedAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// TestGenerateRecoveryCodesProducesCodesAPersonCanCopy checks the shape, the
// alphabet and the freshness together, because each of the three is a way the
// mechanism fails quietly.
func TestGenerateRecoveryCodesProducesCodesAPersonCanCopy(t *testing.T) {
	const batches = 16
	seen := map[string]bool{}
	used := map[rune]bool{}

	for batch := 0; batch < batches; batch++ {
		codes, err := twofactor.GenerateRecoveryCodes(twofactor.DefaultRecoveryCodes)
		if err != nil {
			t.Fatalf("GenerateRecoveryCodes: %v", err)
		}
		if len(codes) != twofactor.DefaultRecoveryCodes {
			t.Fatalf("GenerateRecoveryCodes returned %d codes, and it was asked for %d",
				len(codes), twofactor.DefaultRecoveryCodes)
		}

		for _, code := range codes {
			if len(code) != twofactor.RecoveryCodeLength {
				t.Errorf("%q is %d characters, and a recovery code is %d",
					code, len(code), twofactor.RecoveryCodeLength)
			}
			if seen[code] {
				t.Errorf("%q was issued twice, so spending it once would spend two", code)
			}
			seen[code] = true

			// The canonical form is what a store hashes, so a generated code
			// must already be in it.
			if normalized := twofactor.NormalizeCode(code); normalized != code {
				t.Errorf("%q is not in canonical form: it normalizes to %q", code, normalized)
			}

			for _, r := range code {
				used[r] = true
				if strings.ContainsRune("IO01", r) {
					t.Errorf("%q contains %q, which is half of a pair a person misreads on paper", code, r)
				}
				if !strings.ContainsRune(expectedAlphabet, r) {
					t.Errorf("%q contains %q, which is outside the alphabet %q", code, r, expectedAlphabet)
				}
			}
		}
	}

	// Sixteen batches of eight ten-character codes is 1280 characters. Seeing
	// far fewer than thirty-two distinct ones would mean the draw is skewed, and
	// the entropy the length claims is not there.
	if len(used) != 32 {
		t.Errorf("%d distinct characters were drawn across %d codes, and the alphabet has 32",
			len(used), batches*twofactor.DefaultRecoveryCodes)
	}
}

// TestGenerateRecoveryCodesHonoursTheCount walks a real range rather than one
// value, so the loop that builds them is exercised at its edge.
func TestGenerateRecoveryCodesHonoursTheCount(t *testing.T) {
	for n := 1; n <= 12; n++ {
		codes, err := twofactor.GenerateRecoveryCodes(n)
		if err != nil {
			t.Errorf("%d: %v", n, err)
			continue
		}
		if len(codes) != n {
			t.Errorf("%d: returned %d codes", n, len(codes))
		}
	}
}

// TestGenerateRecoveryCodesRefusesANumberItCannotIssue keeps a zero from
// reading as "the account has recovery codes" when it has none.
func TestGenerateRecoveryCodesRefusesANumberItCannotIssue(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		codes, err := twofactor.GenerateRecoveryCodes(n)
		if !errors.Is(err, twofactor.ErrRecoveryCount) {
			t.Errorf("%d returned %v, and it should be ErrRecoveryCount", n, err)
		}
		if codes != nil {
			t.Errorf("%d returned %v alongside its refusal", n, codes)
		}
	}
}

// TestARecoveryCodeSurvivesBeingRetyped is the round trip that matters at the
// keyboard: what was printed, typed back in whatever case and grouping, is the
// string the store was given.
func TestARecoveryCodeSurvivesBeingRetyped(t *testing.T) {
	codes, err := twofactor.GenerateRecoveryCodes(1)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	issued := codes[0]

	forms := map[string]string{
		"as printed":  issued,
		"lower case":  strings.ToLower(issued),
		"in groups":   issued[:5] + "-" + issued[5:],
		"with spaces": issued[:2] + " " + issued[2:6] + " " + issued[6:],
	}
	for name, typed := range forms {
		if got := twofactor.NormalizeCode(typed); got != issued {
			t.Errorf("%s (%q) normalized to %q, and it was issued as %q", name, typed, got, issued)
		}
	}
}
