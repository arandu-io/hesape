package otp_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/otp"
)

// rfcSecret is the shared secret both RFC 4226 and RFC 6238 publish their test
// values against: the ASCII string "12345678901234567890".
var rfcSecret = []byte("12345678901234567890")

// TestHOTPMatchesTheRFC4226TestValues is the interoperability proof for the
// event-based algorithm.
//
// The counters and codes are Table 2 of Appendix D of RFC 4226. Nothing here is
// this package's opinion: if these ten numbers come out, an authenticator
// application computing the eleventh will agree with us.
func TestHOTPMatchesTheRFC4226TestValues(t *testing.T) {
	codes := []string{
		"755224", "287082", "359152", "969429", "338314",
		"254676", "287922", "162583", "399871", "520489",
	}

	for counter, want := range codes {
		got, err := otp.HOTP(rfcSecret, uint64(counter), 6)
		if err != nil {
			t.Errorf("counter %d: %v", counter, err)
			continue
		}
		if got != want {
			t.Errorf("counter %d: HOTP returned %q, and the RFC says %q", counter, got, want)
		}
	}
}

// TestHOTPReducesToTheLowDigits fixes which end of the number the code is taken
// from. RFC 4226 gives counter 7 the truncated value 82162583 and the six-digit
// code 162583, so the reduction keeps the low places and not the high ones.
//
// The leading-zero padding this reduction needs is exercised by
// TestTOTPMatchesTheRFC6238TestVectors, where the RFC publishes a code that
// starts with one.
func TestHOTPReducesToTheLowDigits(t *testing.T) {
	eight, err := otp.HOTP(rfcSecret, 7, 8)
	if err != nil {
		t.Errorf("eight digits: %v", err)
	} else if want := "82162583"; eight != want {
		t.Errorf("eight digits returned %q, and the RFC's truncated value is %q", eight, want)
	}

	six, err := otp.HOTP(rfcSecret, 7, 6)
	if err != nil {
		t.Errorf("six digits: %v", err)
	} else if want := "162583"; six != want {
		t.Errorf("six digits returned %q, and the RFC says %q", six, want)
	}
}

// TestHOTPRefusesAnUnusableSecret keeps an empty secret from producing a code
// that looks exactly like a real one.
func TestHOTPRefusesAnUnusableSecret(t *testing.T) {
	for _, secret := range [][]byte{nil, {}} {
		got, err := otp.HOTP(secret, 0, 6)
		if !errors.Is(err, otp.ErrSecret) {
			t.Errorf("HOTP with a %d-byte secret returned %q and %v, and it should be ErrSecret", len(secret), got, err)
		}
	}
}

// TestHOTPRefusesACodeLengthItCannotProduce walks both sides of the range and
// the range itself, so the refusal and the acceptance are both exercised.
func TestHOTPRefusesACodeLengthItCannotProduce(t *testing.T) {
	for _, digits := range []int{-1, 0, 5, 9, 100} {
		if _, err := otp.HOTP(rfcSecret, 0, digits); !errors.Is(err, otp.ErrDigits) {
			t.Errorf("%d digits returned %v, and it should be ErrDigits", digits, err)
		}
	}
	for digits := otp.MinDigits; digits <= otp.MaxDigits; digits++ {
		code, err := otp.HOTP(rfcSecret, 0, digits)
		if err != nil {
			t.Errorf("%d digits: %v", digits, err)
			continue
		}
		if len(code) != digits {
			t.Errorf("%d digits produced %q, which is %d characters", digits, code, len(code))
		}
	}
}
