package cookie_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/cookie"
)

// The two digests below were produced outside this package, by
//
//	printf 'laravel_sessionv2' | openssl dgst -sha1 -hmac 'super-secret-key'
//	printf 'otherv2'           | openssl dgst -sha1 -hmac 'super-secret-key'
//
// which is the same call PHP's hash_hmac('sha1', ...) makes. A prefix computed
// here has to match a prefix computed there, or a cookie written by an
// Illuminate application is unreadable by this one.
const (
	prefixKey     = "super-secret-key"
	sessionDigest = "fccddc1e78db8b92ca4f48c1d74f56b6a06f939a"
	otherDigest   = "cdc1efb6b5ca7d5c05ce3bf472a807a8389471b4"
)

func TestCreateCookieValuePrefixMatchesTheDigestPHPWrites(t *testing.T) {
	got := cookie.CreateCookieValuePrefix("laravel_session", []byte(prefixKey))
	if want := sessionDigest + "|"; got != want {
		t.Fatalf("CreateCookieValuePrefix = %q, want %q", got, want)
	}
	if len(got) != 41 {
		t.Fatalf("prefix is %d characters, want the 41 that remove() cuts off", len(got))
	}
}

func TestCreateCookieValuePrefixIsBoundToTheName(t *testing.T) {
	session := cookie.CreateCookieValuePrefix("laravel_session", []byte(prefixKey))
	other := cookie.CreateCookieValuePrefix("other", []byte(prefixKey))

	if session == other {
		t.Fatal("two names produced the same prefix, so a cookie could be renamed and still read")
	}
	if other != otherDigest+"|" {
		t.Fatalf("prefix for \"other\" = %q, want %q", other, otherDigest+"|")
	}
}

func TestCreateCookieValuePrefixIsBoundToTheKey(t *testing.T) {
	first := cookie.CreateCookieValuePrefix("name", []byte("one"))
	second := cookie.CreateCookieValuePrefix("name", []byte("two"))

	if first == second {
		t.Fatal("two keys produced the same prefix")
	}
}

func TestRemoveCookieValuePrefixCutsExactlyThePrefix(t *testing.T) {
	value := cookie.CreateCookieValuePrefix("name", []byte(prefixKey)) + "the value"

	if got := cookie.RemoveCookieValuePrefix(value); got != "the value" {
		t.Fatalf("RemoveCookieValuePrefix = %q, want %q", got, "the value")
	}
}

func TestRemoveCookieValuePrefixOnSomethingShorterThanThePrefix(t *testing.T) {
	// PHP 8 returns "" from substr() past the end of the string, where PHP 7
	// returned false. Nothing here may panic on a short value: the value came
	// off a request.
	for _, value := range []string{"", "short", strings.Repeat("a", 40), strings.Repeat("a", 41)} {
		if got := cookie.RemoveCookieValuePrefix(value); got != "" {
			t.Errorf("RemoveCookieValuePrefix(%q) = %q, want the empty string", value, got)
		}
	}
}

func TestValidateCookieValuePrefixReturnsTheValueWithoutThePrefix(t *testing.T) {
	keys := [][]byte{[]byte(prefixKey)}
	value := cookie.CreateCookieValuePrefix("laravel_session", []byte(prefixKey)) + "abc123"

	got, ok := cookie.ValidateCookieValuePrefix("laravel_session", value, keys)
	if !ok {
		t.Fatal("a value written for this name did not validate")
	}
	if got != "abc123" {
		t.Fatalf("ValidateCookieValuePrefix = %q, want %q", got, "abc123")
	}
}

func TestValidateCookieValuePrefixRefusesACookieMovedToAnotherName(t *testing.T) {
	// This is the whole reason the prefix exists. The attacker holds a valid
	// value -- their own -- and sends it back under the name of a cookie that
	// belongs to somebody else. It decrypts, so decryption cannot catch it; the
	// name inside it is what catches it.
	keys := [][]byte{[]byte(prefixKey)}
	stolen := cookie.CreateCookieValuePrefix("remember_me", []byte(prefixKey)) + "attacker-value"

	if got, ok := cookie.ValidateCookieValuePrefix("laravel_session", stolen, keys); ok {
		t.Fatalf("a value issued for remember_me validated as laravel_session and gave %q", got)
	}
}

func TestValidateCookieValuePrefixTriesEveryKey(t *testing.T) {
	current, previous := []byte("current"), []byte("previous")
	written := cookie.CreateCookieValuePrefix("name", previous) + "value"

	if _, ok := cookie.ValidateCookieValuePrefix("name", written, [][]byte{current}); ok {
		t.Fatal("a value written under the previous key validated against the current one alone")
	}
	got, ok := cookie.ValidateCookieValuePrefix("name", written, [][]byte{current, previous})
	if !ok {
		t.Fatal("a value written under the previous key did not validate when that key was offered")
	}
	if got != "value" {
		t.Fatalf("ValidateCookieValuePrefix = %q, want %q", got, "value")
	}
}

func TestValidateCookieValuePrefixOnTheEdges(t *testing.T) {
	keys := [][]byte{[]byte(prefixKey)}

	// An empty value, no keys at all, and a value that is exactly the prefix
	// and nothing else. The last one is legal: the cookie's value was "".
	if _, ok := cookie.ValidateCookieValuePrefix("name", "", keys); ok {
		t.Error("an empty value validated")
	}
	if _, ok := cookie.ValidateCookieValuePrefix("name", "anything", nil); ok {
		t.Error("a value validated with no keys to validate it against")
	}
	got, ok := cookie.ValidateCookieValuePrefix("name", cookie.CreateCookieValuePrefix("name", []byte(prefixKey)), keys)
	if !ok {
		t.Fatal("a prefix with an empty value behind it did not validate")
	}
	if got != "" {
		t.Fatalf("ValidateCookieValuePrefix = %q, want the empty string", got)
	}
}
