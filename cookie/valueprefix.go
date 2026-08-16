package cookie

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
)

// cookieValuePrefixLength is the length of what CookieValuePrefix.Create
// writes: forty hexadecimal characters of HMAC-SHA1 and the pipe. It is the
// 41 bytes [cookieValuePrefix.Remove] cuts off the front of a value.
const cookieValuePrefixLength = sha1.Size*2 + 1

// cookieValuePrefix carries the methods of [CookieValuePrefix]. It is
// unexported and empty because its three methods need no state of their
// own; the type exists only to hold them, reached through the single
// [CookieValuePrefix] value.
type cookieValuePrefix struct{}

// CookieValuePrefix binds a cookie's value to the name it was issued under,
// so a value copied from one cookie cannot be replayed under another. Its
// three methods are reached through this value rather than written as
// package-level functions, because Go has no static methods and a function
// named Create alone would name no type at all -- cookie.Create says
// nothing about what it creates.
//
// # Why the prefix exists
//
// Without it, an encrypted cookie says nothing about which cookie it is. An
// attacker who holds a valid ciphertext -- their own session cookie, say -- can
// send it back under a different name, and the application decrypts it happily
// and believes it, because decryption succeeded. Renaming a cookie is a
// one-line attack, and the whole defence is that the plaintext carries the name
// it was issued under, so a value moved to another name no longer matches.
var CookieValuePrefix cookieValuePrefix

// Create returns the prefix that goes in front of a cookie value before the
// whole thing is encrypted: hex HMAC-SHA1 over the cookie's own name,
// followed by a pipe. The key is the application key, taken as the bytes the
// encrypter hands out.
//
// The "v2" glued to the name is a version marker from the release that
// introduced the prefix, and it stays fixed because changing it would change
// the digest for every cookie already issued.
func (cookieValuePrefix) Create(cookieName string, key []byte) string {
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(cookieName + "v2"))
	return hex.EncodeToString(mac.Sum(nil)) + "|"
}

// Remove cuts the first 41 characters off and returns the rest, without
// checking that they were a prefix -- the checking is
// [cookieValuePrefix.Validate]. A value shorter than the prefix returns "".
func (cookieValuePrefix) Remove(cookieValue string) string {
	if len(cookieValue) < cookieValuePrefixLength {
		return ""
	}
	return cookieValue[cookieValuePrefixLength:]
}

// Validate checks that the decrypted value opens with the prefix for this
// cookie's name under one of the keys, and returns the value with the prefix
// taken off. Every key is tried, so a value written before a key rotation is
// still read.
//
// It returns the value and a bool rather than using an empty string as a
// sentinel, because "" is a legal cookie value and could not stand in for
// absence. A caller that ignores the second result reads an empty string for
// a cookie that failed the check, which is the mistake this shape exists to
// make visible.
//
// The comparison uses [hmac.Equal] and runs in constant time: the digest
// being compared is a MAC over a name the attacker chooses, so a comparison
// that stopped at the first mismatched byte would leak information worth
// timing.
func (p cookieValuePrefix) Validate(cookieName, cookieValue string, keys [][]byte) (string, bool) {
	for _, key := range keys {
		prefix := p.Create(cookieName, key)
		if len(cookieValue) < len(prefix) {
			continue
		}
		if hmac.Equal([]byte(cookieValue[:len(prefix)]), []byte(prefix)) {
			return p.Remove(cookieValue), true
		}
	}
	return "", false
}
