package otp

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
)

// SecretSize is how many random bytes [NewSecret] draws.
//
// Twenty: the 160 bits RFC 4226 recommends, and the width of the digest the
// code is truncated from, so a longer secret would buy nothing the truncation
// keeps.
const SecretSize = 20

// secretEncoding is base32 as RFC 4648 defines it, with the padding left off.
//
// Base32 and not base64 because the secret is also typed in by hand when the
// camera will not focus, and base32 has no character whose case matters and no
// pair a person confuses on a phone screen. Without padding because the
// provisioning URI should omit it, and because nobody should have to type "=".
var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a fresh secret of [SecretSize] random bytes.
//
// There is no error to return: crypto/rand fills the slice or the process dies
// trying, and a caller cannot recover from an operating system that has stopped
// producing randomness.
func NewSecret() []byte {
	secret := make([]byte, SecretSize)
	// Errors are documented as impossible since Go 1.24: Read either fills the
	// slice entirely or panics.
	_, _ = rand.Read(secret)
	return secret
}

// EncodeSecret returns the base32 text of secret, which is what goes in a
// provisioning URI and what a person retypes by hand.
//
// It never emits padding. A "=" in a URI query has to be escaped by whoever
// builds the URI and unescaped by whoever reads it, and an implementation that
// forgets one half produces a secret that decodes to the wrong bytes rather
// than failing.
func EncodeSecret(secret []byte) string {
	return secretEncoding.EncodeToString(secret)
}

// DecodeSecret reads the base32 text of a secret.
//
// It is deliberately forgiving about what arrives, because one of the two ways
// a secret reaches this function is a person copying it off a screen: lower
// case is folded up, spaces and hyphens between groups are dropped, and
// trailing padding is accepted even though [EncodeSecret] never writes any.
// None of that changes which bytes a valid secret decodes to; it only decides
// whether a person's second attempt is refused for a reason they cannot see.
func DecodeSecret(encoded string) ([]byte, error) {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '\t' || r == '-' || r == '=':
			return -1
		case r >= 'a' && r <= 'z':
			return r - ('a' - 'A')
		default:
			return r
		}
	}, encoded)

	if cleaned == "" {
		return nil, fmt.Errorf("%w: it is empty", ErrSecret)
	}
	secret, err := secretEncoding.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("%w: it is not base32", ErrSecret)
	}
	if len(secret) == 0 {
		// Base32 packs five bits per character, so a single character decodes
		// to no bytes at all and does it without complaining. Returning that
		// would hand back an empty secret that fails later, somewhere with less
		// to say about why.
		return nil, fmt.Errorf("%w: %q is too short to carry a byte", ErrSecret, cleaned)
	}
	return secret, nil
}
