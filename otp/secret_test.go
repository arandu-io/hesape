package otp_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/otp"
)

// TestEncodeSecretNeverPads walks every remainder base32 has, so the lengths
// that would be padded are all exercised rather than only the one that happens
// to divide evenly.
//
// [otp.SecretSize] is twenty, a multiple of five, and base32 pads nothing at
// multiples of five. A test that only used a fresh secret would pass with the
// padding switched back on.
func TestEncodeSecretNeverPads(t *testing.T) {
	sawPaddedLength := false
	for size := 1; size <= 21; size++ {
		encoded := otp.EncodeSecret(bytes.Repeat([]byte{0xA5}, size))
		if strings.Contains(encoded, "=") {
			t.Errorf("%d bytes encoded to %q, which carries padding", size, encoded)
		}
		if size%5 != 0 {
			sawPaddedLength = true
		}
	}
	if !sawPaddedLength {
		t.Error("no length that base32 would pad was exercised")
	}
}

// TestSecretRoundTrips is the property that matters: what a person is shown is
// what the algorithm gets back.
func TestSecretRoundTrips(t *testing.T) {
	for size := 1; size <= 21; size++ {
		want := bytes.Repeat([]byte{byte(size), 0x00, 0xFF}, size)[:size]
		got, err := otp.DecodeSecret(otp.EncodeSecret(want))
		if err != nil {
			t.Errorf("%d bytes: %v", size, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%d bytes: decoded to %x, and it was %x", size, got, want)
		}
	}
}

// TestDecodeSecretAcceptsWhatAPersonTypes covers the second way a secret
// arrives: copied off a screen, in whatever case and spacing the person used.
func TestDecodeSecretAcceptsWhatAPersonTypes(t *testing.T) {
	secret := bytes.Repeat([]byte{0x48, 0x65}, 8)
	canonical := otp.EncodeSecret(secret)

	forms := map[string]string{
		"as shown":     canonical,
		"lower case":   strings.ToLower(canonical),
		"mixed case":   strings.ToLower(canonical[:4]) + canonical[4:],
		"with spaces":  canonical[:4] + " " + canonical[4:8] + " " + canonical[8:],
		"with hyphens": canonical[:4] + "-" + canonical[4:],
		"with a tab":   canonical[:4] + "\t" + canonical[4:],
		"padded":       canonical + "======",
	}

	for name, form := range forms {
		got, err := otp.DecodeSecret(form)
		if err != nil {
			t.Errorf("%s (%q): %v", name, form, err)
			continue
		}
		if !bytes.Equal(got, secret) {
			t.Errorf("%s: decoded to %x, and it should be %x", name, got, secret)
		}
	}
}

// TestDecodeSecretRefusesWhatIsNotASecret keeps a typo from becoming an empty
// secret that then fails somewhere less obvious.
func TestDecodeSecretRefusesWhatIsNotASecret(t *testing.T) {
	for name, form := range map[string]string{
		"empty":            "",
		"only separators":  "  --  ",
		"only padding":     "====",
		"not base32":       "01890!!!",
		"an odd remainder": "A",
	} {
		got, err := otp.DecodeSecret(form)
		if !errors.Is(err, otp.ErrSecret) {
			t.Errorf("%s (%q): decoded to %x with %v, and it should be ErrSecret", name, form, got, err)
		}
	}
}

// TestNewSecretIsFreshAndTheRightSize is what it says. Two secrets being equal
// would mean the randomness is not, and one byte of it is enough to notice.
func TestNewSecretIsFreshAndTheRightSize(t *testing.T) {
	seen := make(map[string]bool, 32)
	for i := 0; i < 32; i++ {
		secret := otp.NewSecret()
		if len(secret) != otp.SecretSize {
			t.Fatalf("NewSecret returned %d bytes, and SecretSize is %d", len(secret), otp.SecretSize)
		}
		key := string(secret)
		if seen[key] {
			t.Fatalf("NewSecret returned the same secret twice: %x", secret)
		}
		seen[key] = true

		// A fresh secret has to work end to end, or the size is the only thing
		// proved about it.
		if _, err := otp.DecodeSecret(otp.EncodeSecret(secret)); err != nil {
			t.Fatalf("a fresh secret did not survive its own text form: %v", err)
		}
	}
}
