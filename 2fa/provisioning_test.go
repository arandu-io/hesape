package twofactor_test

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	twofactor "github.com/arandu-io/hesape/2fa"
	"github.com/arandu-io/hesape/otp"
)

// exampleSecret is the secret the key uri format's own example uses, so the URI
// this package builds can be compared with the one that document publishes.
// Base32: JBSWY3DPEHPK3PXP.
var exampleSecret = []byte{'H', 'e', 'l', 'l', 'o', '!', 0xDE, 0xAD, 0xBE, 0xEF}

// TestURIMatchesTheKeyUriFormat compares the whole string, not pieces of it.
//
// The expected value follows the format's own example: the label is
// "issuer:account" with the space in the issuer written %20 and the at sign in
// the account left alone, and the issuer appears again as a parameter.
func TestURIMatchesTheKeyUriFormat(t *testing.T) {
	p := twofactor.Provisioning{
		Issuer:  "ACME Co",
		Account: "john.doe@email.com",
		Secret:  exampleSecret,
	}

	got, err := p.URI()
	if err != nil {
		t.Fatalf("URI: %v", err)
	}

	want := "otpauth://totp/ACME%20Co:john.doe@email.com" +
		"?secret=JBSWY3DPEHPK3PXP&issuer=ACME%20Co&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Errorf("URI returned\n  %s\nand it should be\n  %s", got, want)
	}
}

// TestURIEscapesRatherThanFormEncodes is the bug this escaping exists to avoid.
//
// A space written "+" is form encoding. An authenticator that reads the issuer
// as a URI shows "ACME+Co", and the account is named wrongly on the person's
// phone for as long as the enrolment lasts. It is the classic mistake here
// because the standard library's QueryEscape does exactly that.
func TestURIEscapesRatherThanFormEncodes(t *testing.T) {
	p := twofactor.Provisioning{
		Issuer:  "A+B & Co",
		Account: "someone with spaces",
		Secret:  exampleSecret,
	}

	got, err := p.URI()
	if err != nil {
		t.Fatalf("URI: %v", err)
	}
	if strings.Contains(got, "+") {
		t.Errorf("URI returned %q, which carries a bare +, so a space or a plus is form encoded", got)
	}
	for _, want := range []string{"A%2BB%20%26%20Co", "someone%20with%20spaces"} {
		if !strings.Contains(got, want) {
			t.Errorf("URI returned %q, and it should contain %q", got, want)
		}
	}
}

// TestURIReadsBackAsWhatWasPutIn parses the result with the standard library
// and checks that every field survives the round trip. It is the assertion that
// does not depend on any expected string being right.
func TestURIReadsBackAsWhatWasPutIn(t *testing.T) {
	cases := map[string]twofactor.Provisioning{
		"plain": {
			Issuer:  "Arandu",
			Account: "person@example.com",
			Secret:  exampleSecret,
		},
		"spaces and punctuation": {
			Issuer:  "Big Corporation, S.A.",
			Account: "alice+tag@bigco.example",
			Secret:  exampleSecret,
		},
		"not ascii": {
			Issuer:  "Praça Digital",
			Account: "joão@exemplo.com.br",
			Secret:  exampleSecret,
		},
		"eight digits and a minute": {
			Issuer:  "Arandu",
			Account: "person@example.com",
			Secret:  exampleSecret,
			TOTP:    otp.TOTP{Digits: 8, Period: time.Minute, Skew: 1},
		},
	}

	for name, p := range cases {
		raw, err := p.URI()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}

		parsed, err := url.Parse(raw)
		if err != nil {
			t.Errorf("%s: the URI does not parse: %v", name, err)
			continue
		}
		if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
			t.Errorf("%s: the scheme and type are %s://%s", name, parsed.Scheme, parsed.Host)
		}

		label := strings.TrimPrefix(parsed.Path, "/")
		issuer, account, found := strings.Cut(label, ":")
		if !found {
			t.Errorf("%s: the label %q has no colon in it", name, label)
			continue
		}
		if issuer != p.Issuer {
			t.Errorf("%s: the label's issuer read back as %q, and it was %q", name, issuer, p.Issuer)
		}
		if account != p.Account {
			t.Errorf("%s: the label's account read back as %q, and it was %q", name, account, p.Account)
		}

		query := parsed.Query()
		if got := query.Get("issuer"); got != p.Issuer {
			t.Errorf("%s: the issuer parameter read back as %q, and it was %q", name, got, p.Issuer)
		}

		secret, err := otp.DecodeSecret(query.Get("secret"))
		if err != nil {
			t.Errorf("%s: the secret parameter does not decode: %v", name, err)
		} else if string(secret) != string(p.Secret) {
			t.Errorf("%s: the secret read back as %x, and it was %x", name, secret, p.Secret)
		}

		config := p.TOTP.Resolve()
		wantDigits := strconv.Itoa(config.Digits)
		if got := query.Get("digits"); got != wantDigits {
			t.Errorf("%s: digits read back as %q, and the configuration says %q", name, got, wantDigits)
		}
		wantPeriod := strconv.Itoa(int(config.Period / time.Second))
		if got := query.Get("period"); got != wantPeriod {
			t.Errorf("%s: period read back as %q, and the configuration says %q", name, got, wantPeriod)
		}
		if got := query.Get("algorithm"); got != "SHA1" {
			t.Errorf("%s: algorithm read back as %q", name, got)
		}
	}
}

// TestURIRefusesAnEnrolmentItCannotName covers every refusal, so each branch of
// the validation runs.
func TestURIRefusesAnEnrolmentItCannotName(t *testing.T) {
	cases := map[string]twofactor.Provisioning{
		"no issuer": {
			Account: "person@example.com", Secret: exampleSecret,
		},
		"no account": {
			Issuer: "Arandu", Secret: exampleSecret,
		},
		"a colon in the issuer": {
			Issuer: "Arandu: the framework", Account: "person@example.com", Secret: exampleSecret,
		},
		"a colon in the account": {
			Issuer: "Arandu", Account: "person:example.com", Secret: exampleSecret,
		},
		"a newline in the issuer": {
			Issuer: "Arandu\nInc", Account: "person@example.com", Secret: exampleSecret,
		},
		"a newline in the account": {
			Issuer: "Arandu", Account: "person@example.com\r", Secret: exampleSecret,
		},
		"no secret": {
			Issuer: "Arandu", Account: "person@example.com",
		},
		"a configuration that cannot produce codes": {
			Issuer: "Arandu", Account: "person@example.com", Secret: exampleSecret,
			TOTP: otp.TOTP{Digits: 9, Period: otp.DefaultPeriod},
		},
	}

	for name, p := range cases {
		got, err := p.URI()
		if !errors.Is(err, twofactor.ErrProvisioning) {
			t.Errorf("%s: URI returned %q and %v, and it should be ErrProvisioning", name, got, err)
		}
		if got != "" {
			t.Errorf("%s: URI returned %q alongside its refusal", name, got)
		}
	}
}

// TestURIKeepsTheConfigurationTheEnrolmentWasMadeUnder fixes the reason the
// parameters are written out at all: an application that assumed six digits for
// an eight-digit enrolment would produce codes the server never accepts.
func TestURIKeepsTheConfigurationTheEnrolmentWasMadeUnder(t *testing.T) {
	p := twofactor.Provisioning{
		Issuer:  "Arandu",
		Account: "person@example.com",
		Secret:  exampleSecret,
		TOTP:    otp.TOTP{Digits: 8, Period: 60 * time.Second, Skew: 1},
	}
	got, err := p.URI()
	if err != nil {
		t.Fatalf("URI: %v", err)
	}
	for _, want := range []string{"&digits=8", "&period=60"} {
		if !strings.Contains(got, want) {
			t.Errorf("URI returned %q, and it should contain %q", got, want)
		}
	}
}
