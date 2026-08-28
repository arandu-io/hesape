package otp_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/otp"
)

// eightDigits is the configuration RFC 6238 publishes its test vectors under:
// eight digits, a thirty-second step. Skew is irrelevant to generation and is
// set to the default so nothing about the zero value is being exercised here by
// accident.
var eightDigits = otp.TOTP{Digits: 8, Period: otp.DefaultPeriod, Skew: otp.DefaultSkew}

// TestTOTPMatchesTheRFC6238TestVectors is the interoperability proof.
//
// The times and codes are the SHA-1 rows of Table 1 in Appendix B of RFC 6238,
// under the shared secret that appendix names. The other rows of that table are
// SHA-256 and SHA-512, which this package does not compute and will not: the
// applications people already have installed speak SHA-1.
//
// The codes are eight digits because the RFC's table is eight digits. That is
// the reason the code length is a parameter at all.
func TestTOTPMatchesTheRFC6238TestVectors(t *testing.T) {
	vectors := []struct {
		seconds int64
		code    string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}

	sawLeadingZero := false
	for _, v := range vectors {
		at := time.Unix(v.seconds, 0).UTC()
		got, err := eightDigits.Generate(rfcSecret, at)
		if err != nil {
			t.Errorf("%d: %v", v.seconds, err)
			continue
		}
		if got != v.code {
			t.Errorf("%d: Generate returned %q, and the RFC says %q", v.seconds, got, v.code)
		}
		if strings.HasPrefix(v.code, "0") {
			sawLeadingZero = true
		}

		// The same vectors read backwards. Verify has to accept the code the
		// RFC says the secret produces, and the step it reports has to be the
		// one the table's T column names.
		step, err := eightDigits.Verify(rfcSecret, v.code, at)
		if err != nil {
			t.Errorf("%d: Verify refused the RFC's own code: %v", v.seconds, err)
			continue
		}
		if want := uint64(v.seconds / 30); step != want {
			t.Errorf("%d: Verify reported step %d, and the RFC's T is %d", v.seconds, step, want)
		}
	}

	if !sawLeadingZero {
		t.Error("no vector with a leading zero was exercised, so the padding is unproven")
	}
}

// TestTOTPDefaultsToWhatAnAuthenticatorAssumes fixes the zero value, which is
// what every caller that does not care will use.
func TestTOTPDefaultsToWhatAnAuthenticatorAssumes(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()

	zero, err := otp.TOTP{}.Generate(rfcSecret, at)
	if err != nil {
		t.Errorf("the zero value: %v", err)
	}
	explicit, err := otp.Default().Generate(rfcSecret, at)
	if err != nil {
		t.Errorf("Default: %v", err)
	}
	if zero != explicit {
		t.Errorf("the zero value produced %q and Default produced %q", zero, explicit)
	}
	if len(zero) != otp.DefaultDigits {
		t.Errorf("the default produced %q, which is %d characters and not %d", zero, len(zero), otp.DefaultDigits)
	}

	// The RFC's eight-digit code for this instant is 07081804, and six digits
	// is the low six of it.
	if want := "081804"; zero != want {
		t.Errorf("the default produced %q, and the low six digits of the RFC's value are %q", zero, want)
	}
}

// TestVerifyAcceptsOneStepEitherSideAndNoMore is the window. It walks four
// steps on each side, so both the accepting branch and the refusing branch run
// several times each and neither can be empty.
func TestVerifyAcceptsOneStepEitherSideAndNoMore(t *testing.T) {
	// A time in the middle of its step, so that "one step away" is a real step
	// away and not a rounding artefact.
	at := time.Unix(1111111100, 0).UTC()
	center := uint64(1111111100 / 30)

	for offset := -4; offset <= 4; offset++ {
		step := uint64(int64(center) + int64(offset))
		code, err := otp.HOTP(rfcSecret, step, otp.DefaultDigits)
		if err != nil {
			t.Errorf("offset %+d: %v", offset, err)
			continue
		}

		got, err := otp.Default().Verify(rfcSecret, code, at)
		accepted := err == nil
		wantAccepted := offset >= -1 && offset <= 1

		if accepted != wantAccepted {
			t.Errorf("offset %+d: accepted is %t and it should be %t (err %v)", offset, accepted, wantAccepted, err)
			continue
		}
		if accepted && got != step {
			t.Errorf("offset %+d: Verify reported step %d and the code was made at %d", offset, got, step)
		}
		if !accepted && !errors.Is(err, otp.ErrMismatch) {
			t.Errorf("offset %+d: refused with %v, and it should be ErrMismatch", offset, err)
		}
	}
}

// TestVerifyHonoursASkewOfZero proves the window is the configured number and
// not a constant that happens to be one.
func TestVerifyHonoursASkewOfZero(t *testing.T) {
	strict := otp.TOTP{Digits: otp.DefaultDigits, Period: otp.DefaultPeriod, Skew: 0}
	at := time.Unix(1111111100, 0).UTC()
	center := uint64(1111111100 / 30)

	for offset := -1; offset <= 1; offset++ {
		step := uint64(int64(center) + int64(offset))
		code, err := otp.HOTP(rfcSecret, step, otp.DefaultDigits)
		if err != nil {
			t.Errorf("offset %+d: %v", offset, err)
			continue
		}
		_, err = strict.Verify(rfcSecret, code, at)
		if (err == nil) != (offset == 0) {
			t.Errorf("offset %+d: err is %v, and only the current step should be accepted", offset, err)
		}
	}
}

// TestVerifyRefusesWhatIsNotACode covers the shapes that arrive from a form.
func TestVerifyRefusesWhatIsNotACode(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	valid, err := otp.Default().Generate(rfcSecret, at)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	cases := map[string]string{
		"empty":         "",
		"too short":     valid[:5],
		"too long":      valid + "0",
		"not digits":    "abcdef",
		"another code":  "000000",
		"with a space":  valid[:3] + " " + valid[3:],
		"right length":  strings.Repeat("9", otp.DefaultDigits),
		"the right one": valid,
	}

	for name, code := range cases {
		_, err := otp.Default().Verify(rfcSecret, code, at)
		if name == "the right one" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if !errors.Is(err, otp.ErrMismatch) {
			t.Errorf("%s: Verify returned %v, and it should be ErrMismatch", name, err)
		}
	}
}

// TestTheRefusedFixturesAreNotCodesTheSecretProduces guards the case above from
// becoming vacuous. "000000" and "999999" are refused there because they are
// wrong, and this fails if either ever becomes right for some step of the
// window -- at which point that case would be passing for no reason.
func TestTheRefusedFixturesAreNotCodesTheSecretProduces(t *testing.T) {
	center := uint64(1111111109 / 30)
	for _, fixture := range []string{"000000", "999999"} {
		for step := center - 1; step <= center+1; step++ {
			code, err := otp.HOTP(rfcSecret, step, otp.DefaultDigits)
			if err != nil {
				t.Errorf("step %d: %v", step, err)
				continue
			}
			if code == fixture {
				t.Errorf("step %d now produces %q, so the refusal case for it proves nothing", step, fixture)
			}
		}
	}
}

// TestAnInstantBeforeTheEpochHasNoStep covers the zero time.Time, which is what
// a caller that forgot to set a field passes.
func TestAnInstantBeforeTheEpochHasNoStep(t *testing.T) {
	for name, at := range map[string]time.Time{
		"the zero value":              {},
		"one second before the epoch": time.Unix(-1, 0),
	} {
		if _, err := otp.Default().Generate(rfcSecret, at); !errors.Is(err, otp.ErrTime) {
			t.Errorf("Generate at %s returned %v, and it should be ErrTime", name, err)
		}
		if _, err := otp.Default().Verify(rfcSecret, "000000", at); !errors.Is(err, otp.ErrTime) {
			t.Errorf("Verify at %s returned %v, and it should be ErrTime", name, err)
		}
	}
}

// TestVerifyDoesNotWrapAroundTheEpoch fixes the clamp: at the first step there
// is no step below it, and the window must be short rather than enormous.
func TestVerifyDoesNotWrapAroundTheEpoch(t *testing.T) {
	at := time.Unix(0, 0).UTC()
	code, err := otp.Default().Generate(rfcSecret, at)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	step, err := otp.Default().Verify(rfcSecret, code, at)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if step != 0 {
		t.Errorf("Verify reported step %d at the epoch, and it should be 0", step)
	}
}

// TestValidateRefusesAConfigurationThatCannotBeProvisioned covers each field,
// including a period a URI could not carry.
func TestValidateRefusesAConfigurationThatCannotBeProvisioned(t *testing.T) {
	cases := map[string]struct {
		config otp.TOTP
		want   error
	}{
		"no period":           {otp.TOTP{Digits: 6}, otp.ErrPeriod},
		"a negative period":   {otp.TOTP{Digits: 6, Period: -time.Second}, otp.ErrPeriod},
		"a fractional period": {otp.TOTP{Digits: 6, Period: 1500 * time.Millisecond}, otp.ErrPeriod},
		"too few digits":      {otp.TOTP{Digits: 5, Period: otp.DefaultPeriod}, otp.ErrDigits},
		"too many digits":     {otp.TOTP{Digits: 9, Period: otp.DefaultPeriod}, otp.ErrDigits},
	}

	for name, c := range cases {
		if err := c.config.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: Validate returned %v, and it should be %v", name, err, c.want)
		}
		if _, err := c.config.Generate(rfcSecret, time.Unix(59, 0)); !errors.Is(err, c.want) {
			t.Errorf("%s: Generate returned %v, and it should be %v", name, err, c.want)
		}
		if _, err := c.config.Verify(rfcSecret, "000000", time.Unix(59, 0)); !errors.Is(err, c.want) {
			t.Errorf("%s: Verify returned %v, and it should be %v", name, err, c.want)
		}
	}

	for name, config := range map[string]otp.TOTP{
		"the zero value": {},
		"the default":    otp.Default(),
		"eight digits":   eightDigits,
		"a minute":       {Digits: 6, Period: time.Minute, Skew: 1},
	} {
		if err := config.Validate(); err != nil {
			t.Errorf("%s: Validate returned %v, and it is a usable configuration", name, err)
		}
	}
}

// TestGenerateAndVerifyRefuseAnUnusableSecret keeps a missing secret from
// reading as a wrong code, which is a different thing to tell a person.
func TestGenerateAndVerifyRefuseAnUnusableSecret(t *testing.T) {
	at := time.Unix(1111111109, 0).UTC()
	if _, err := otp.Default().Generate(nil, at); !errors.Is(err, otp.ErrSecret) {
		t.Errorf("Generate returned %v, and it should be ErrSecret", err)
	}
	if _, err := otp.Default().Verify(nil, "000000", at); !errors.Is(err, otp.ErrSecret) {
		t.Errorf("Verify returned %v, and it should be ErrSecret", err)
	}
}
