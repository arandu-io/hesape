package otp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// DefaultDigits is the code length an authenticator application assumes
	// when it is not told otherwise.
	DefaultDigits = 6

	// MinDigits is the shortest code this package will produce.
	//
	// Six, and not fewer, because a shorter code is a smaller space to guess
	// and nothing displays one.
	MinDigits = 6

	// MaxDigits is the longest code this package will produce.
	//
	// Eight, and not more, because dynamic truncation yields a 31-bit number:
	// past ten digits the extra places would be zeroes, and past eight no
	// authenticator application shows them.
	MaxDigits = 8
)

// ErrSecret is a secret that cannot be used: empty, or not readable as base32
// where a text form was given.
var ErrSecret = errors.New("otp: the secret is not usable")

// ErrDigits is a code length outside [MinDigits], [MaxDigits].
var ErrDigits = errors.New("otp: the code length is out of range")

// HOTP returns the RFC 4226 code that secret and counter produce, digits long.
//
// It is the whole of the event-based algorithm: HMAC-SHA-1 over the counter
// written big-endian in eight bytes, dynamically truncated, reduced to digits
// decimal places and padded with leading zeroes. The padding is not cosmetic --
// a code is compared as text, and "012345" and "12345" are different codes.
//
// [TOTP] calls this with a counter taken from the clock. A caller with its own
// counter -- a hardware token that increments per press -- calls it directly.
func HOTP(secret []byte, counter uint64, digits int) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("%w: it is empty", ErrSecret)
	}
	if digits < MinDigits || digits > MaxDigits {
		return "", fmt.Errorf("%w: %d, and it must be between %d and %d", ErrDigits, digits, MinDigits, MaxDigits)
	}

	var moving [8]byte
	binary.BigEndian.PutUint64(moving[:], counter)

	mac := hmac.New(sha1.New, secret)
	// hash.Hash documents that Write never returns an error.
	_, _ = mac.Write(moving[:])

	return format(truncate(mac.Sum(nil)), digits), nil
}

// truncate is the dynamic truncation of RFC 4226 section 5.3.
//
// The offset is read from the digest rather than fixed, so that which four
// bytes become the code depends on the secret. A fixed offset would mean an
// attacker who learned those four bytes of one digest learned where to look in
// every other.
func truncate(sum []byte) uint32 {
	// The low nibble of the last byte is at most 15, and a SHA-1 digest is 20
	// bytes, so the four bytes read below are always inside it.
	offset := int(sum[len(sum)-1] & 0x0f)

	// The top bit is masked off so the number does not depend on how a platform
	// reads a sign. Every implementation must drop the same bit or produce a
	// different code for the same secret.
	return binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
}

// format reduces value to digits decimal places, padded with leading zeroes.
func format(value uint32, digits int) string {
	return fmt.Sprintf("%0*d", digits, value%pow10[digits])
}

// pow10 is indexed by code length. Only [MinDigits] to [MaxDigits] are ever
// read; the shorter entries exist so the index is the digit count itself and
// not the digit count minus something.
var pow10 = [MaxDigits + 1]uint32{
	1, 10, 100, 1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000,
}
