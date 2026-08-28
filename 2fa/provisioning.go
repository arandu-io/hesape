package twofactor

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/otp"
)

// ErrProvisioning is an enrolment that cannot be handed to an authenticator
// application. Every reason it is refused unwraps to this one, because the
// answer is the same in all of them: fix the enrolment, do not show a URI.
var ErrProvisioning = errors.New("twofactor: the enrolment cannot be provisioned")

// Provisioning is everything an authenticator application needs in order to
// start producing codes for one account.
//
// It is the request, not the record. Nothing here is stored by this package,
// and the same struct produces the QR code and the key a person types in when
// the camera will not focus -- [Provisioning.URI] and otp.EncodeSecret over the
// same [Provisioning.Secret].
type Provisioning struct {
	// Issuer names the application, and is what the person sees at the top of
	// the entry in their authenticator. It cannot contain a colon.
	Issuer string

	// Account names the person within the application: usually the address they
	// sign in with. It cannot contain a colon.
	//
	// It is shown under the issuer, and it is how somebody with three accounts
	// on the same service tells them apart -- so an identifier only the database
	// understands is a bad choice here.
	Account string

	// Secret is the shared secret, as raw bytes. otp.NewSecret produces one.
	//
	// This is key material. It goes into the URI, it reaches the phone, and it
	// must not reach a log.
	Secret []byte

	// TOTP is the code length, time step and tolerance. The zero value means
	// otp.Default, which is what an application assumes when it is not told.
	TOTP otp.TOTP
}

// URI returns the otpauth:// provisioning URI, which is what a QR code for this
// enrolment encodes.
//
// The label carries the issuer and the account separated by a colon, and the
// issuer is repeated as a parameter -- both, because an older application reads
// only the label and a newer one reads only the parameter, and an enrolment
// that names the service in one place shows up unnamed in half of them.
//
// The code length and period are written out even though they are the defaults.
// An application that assumes different ones would otherwise produce codes this
// server never accepts, and the person would have no way to tell why.
func (p Provisioning) URI() (string, error) {
	if err := p.validate(); err != nil {
		return "", err
	}
	config := p.TOTP.Resolve()

	var b strings.Builder
	b.WriteString("otpauth://totp/")
	b.WriteString(escape(p.Issuer, labelSafe))
	// A literal colon, which is what the format's own examples write. It is the
	// one character that may not appear inside either half, which is why both
	// are refused above if they contain one: escaped or not, an application
	// splits the label on it.
	b.WriteString(":")
	b.WriteString(escape(p.Account, labelSafe))

	b.WriteString("?secret=")
	b.WriteString(otp.EncodeSecret(p.Secret))
	b.WriteString("&issuer=")
	b.WriteString(escape(p.Issuer, ""))
	b.WriteString("&algorithm=SHA1")
	fmt.Fprintf(&b, "&digits=%d", config.Digits)
	fmt.Fprintf(&b, "&period=%d", int64(config.Period/time.Second))

	return b.String(), nil
}

// validate refuses the enrolments that would produce a URI an application reads
// as something other than what was meant.
func (p Provisioning) validate() error {
	for name, value := range map[string]string{"issuer": p.Issuer, "account": p.Account} {
		switch {
		case value == "":
			return fmt.Errorf("%w: the %s is empty, and it is what the person reads to know which account this is", ErrProvisioning, name)
		case strings.Contains(value, ":"):
			return fmt.Errorf("%w: the %s contains a colon, which is the character that separates the two halves of the label", ErrProvisioning, name)
		case strings.ContainsFunc(value, isControl):
			return fmt.Errorf("%w: the %s contains a control character", ErrProvisioning, name)
		}
	}
	if len(p.Secret) == 0 {
		return fmt.Errorf("%w: the secret is empty", ErrProvisioning)
	}
	if err := p.TOTP.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrProvisioning, err)
	}
	return nil
}

// isControl reports whether r is a character that has no business in a label a
// person is meant to read.
func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// labelSafe is the byte the label keeps unescaped beyond the unreserved set.
//
// An address is the usual account name, and the format's own examples write the
// at sign as itself. It is legal unescaped in a path segment, so escaping it
// would only make the label harder to read in the places that show it raw.
const labelSafe = "@"

// escape percent-encodes s, leaving alone the unreserved set of RFC 3986 and
// any byte listed in also.
//
// net/url has no function that does this job. QueryEscape writes a space as
// "+", which is form encoding rather than URI encoding: an application that
// reads the issuer literally then displays "ACME+Co", and the key uri format's
// own example writes that issuer "ACME%20Co". PathEscape leaves "&" and "="
// alone, which would let an account name invent query parameters. So the set is
// written out here, where it can be read.
func escape(s, also string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) || strings.IndexByte(also, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		// Upper case hexadecimal, which is what RFC 3986 says to produce even
		// though a reader must accept either.
		b.WriteString(fmt.Sprintf("%%%02X", c))
	}
	return b.String()
}

// isUnreserved reports whether c is in the set RFC 3986 says never needs
// escaping and must not be escaped by a normaliser.
func isUnreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	}
	return false
}
