package cookie

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
)

// cookieValuePrefixLength is the length of what CreateCookieValuePrefix writes:
// forty hexadecimal characters of HMAC-SHA1 and the pipe. It is the 41 that
// CookieValuePrefix::remove() has as a literal.
const cookieValuePrefixLength = sha1.Size*2 + 1

// CreateCookieValuePrefix answers to CookieValuePrefix::create(), which is a
// static method: the type is part of the identifier, and Go has no static
// methods to hang it on. Naming it Create alone would name no type at the
// package level.
//
// It returns the prefix that goes in front of a cookie value before the whole
// thing is encrypted: hex HMAC-SHA1 over the cookie's own name, followed by a
// pipe. The key is the application key, and PHP takes it as a string where this
// takes the bytes the encrypter hands out.
//
// The "v2" glued to the name is the PHP's, unchanged. It is a version marker
// from the release that introduced the prefix, and it stays because the digest
// has to match the one an Illuminate application computes over the same name
// and key, byte for byte.
//
// # Why the prefix exists
//
// Without it, an encrypted cookie says nothing about which cookie it is. An
// attacker who holds a valid ciphertext -- their own session cookie, say -- can
// send it back under a different name, and the application decrypts it happily
// and believes it, because decryption succeeded. Renaming a cookie is a
// one-line attack, and the whole defence is that the plaintext carries the name
// it was issued under, so a value moved to another name no longer matches.
func CreateCookieValuePrefix(cookieName string, key []byte) string {
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(cookieName + "v2"))
	return hex.EncodeToString(mac.Sum(nil)) + "|"
}

// RemoveCookieValuePrefix answers to CookieValuePrefix::remove(), which is a
// static method: the type is part of the identifier, and Go has no static
// methods to hang it on.
//
// It cuts the first 41 characters off and returns the rest, without checking
// that they were a prefix -- the checking is [ValidateCookieValuePrefix]. A
// value shorter than the prefix returns "", which is what PHP's substr() has
// done past the end of a string since PHP 8, and not the false it returned
// before.
func RemoveCookieValuePrefix(cookieValue string) string {
	if len(cookieValue) < cookieValuePrefixLength {
		return ""
	}
	return cookieValue[cookieValuePrefixLength:]
}

// ValidateCookieValuePrefix answers to CookieValuePrefix::validate(), which is
// a static method: the type is part of the identifier, and Go has no static
// methods to hang it on.
//
// It checks that the decrypted value opens with the prefix for this cookie's
// name under one of the keys, and returns the value with the prefix taken off.
// Every key is tried, so a value written before a key rotation is still read.
//
// PHP returns the value or null; this returns the value and false where PHP
// returns null, because "" is a legal cookie value and could not stand in for
// nothing. A caller that ignores the second result reads an empty string for a
// cookie that failed the check, which is the mistake this shape exists to make
// visible.
//
// The comparison is constant time. PHP compares with str_starts_with, which is
// not, and the digest being compared is a MAC over a name the attacker chooses.
// The result is identical; only the timing changes.
func ValidateCookieValuePrefix(cookieName, cookieValue string, keys [][]byte) (string, bool) {
	for _, key := range keys {
		prefix := CreateCookieValuePrefix(cookieName, key)
		if len(cookieValue) < len(prefix) {
			continue
		}
		if hmac.Equal([]byte(cookieValue[:len(prefix)]), []byte(prefix)) {
			return RemoveCookieValuePrefix(cookieValue), true
		}
	}
	return "", false
}
