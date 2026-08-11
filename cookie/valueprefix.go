package cookie

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
)

// cookieValuePrefixLength is the length of what CookieValuePrefix.Create
// writes: forty hexadecimal characters of HMAC-SHA1 and the pipe. It is the 41
// that CookieValuePrefix::remove() has as a literal.
const cookieValuePrefixLength = sha1.Size*2 + 1

// cookieValuePrefix carries the methods of [CookieValuePrefix]. It is
// unexported and empty because the PHP class is never instantiated -- its three
// methods are static, and the type exists only to hold them.
type cookieValuePrefix struct{}

// CookieValuePrefix answers to Illuminate\Cookie\CookieValuePrefix, whose three
// methods are all static. It is a value and not a type so that the call reads
// the way the PHP one does:
//
//	CookieValuePrefix::create($name, $key)     // PHP
//	cookie.CookieValuePrefix.Create(name, key) // Go
//
// Go has no static methods, and a package-level func named Create alone would
// name no type at all -- cookie.Create says nothing about what it creates. The
// method names below are the PHP method names, capitalised to export.
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

// Create answers to CookieValuePrefix::create().
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
func (cookieValuePrefix) Create(cookieName string, key []byte) string {
	mac := hmac.New(sha1.New, key)
	mac.Write([]byte(cookieName + "v2"))
	return hex.EncodeToString(mac.Sum(nil)) + "|"
}

// Remove answers to CookieValuePrefix::remove().
//
// It cuts the first 41 characters off and returns the rest, without checking
// that they were a prefix -- the checking is [cookieValuePrefix.Validate]. A
// value shorter than the prefix returns "", which is what PHP's substr() has
// done past the end of a string since PHP 8, and not the false it returned
// before.
func (cookieValuePrefix) Remove(cookieValue string) string {
	if len(cookieValue) < cookieValuePrefixLength {
		return ""
	}
	return cookieValue[cookieValuePrefixLength:]
}

// Validate answers to CookieValuePrefix::validate().
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
