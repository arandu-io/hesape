package encryption_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/encryption"
)

func signer() *encryption.Signer {
	return encryption.NewSigner([]byte("an application key long enough to be one"))
}

// goldenSignerKey is the key the fixtures below were signed under. It is 32
// bytes of 'a' and it is not a secret: it exists so that tokens this package
// issued can be checked into this file.
const goldenSignerKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// The fixtures. Both were signed by an earlier build of this package and pasted
// here, which is the whole point of them: a round trip agrees with itself even
// when the signing and the checking changed together, and what has to hold is
// that a link already sitting in somebody's inbox still opens.
const (
	// Purpose "verify-email", expiring in the year 2286, carrying a payload
	// written the way the application writes one -- each field behind its length.
	goldenLiveToken = "ODp0ZW5hbnQtMXw0OnUtNDJ8MTc6cmVhZGVyQGV4YW1wbGUuY29t.9999999999.lew3tvMqP0v9Av293YP39mxzj6v4NiPxnqLuzQvakcg"

	// Purpose "password-reset", expired in November 2023.
	goldenExpiredToken = "ODp0ZW5hbnQtMXw0OnUtNDI.1700000000.12pPtsWmeqR8C-gLYKL8B2mXxh1ZzAymA06YOH6SdDU"
)

// TestATokenIssuedByAnEarlierBuildStillVerifies.
//
// A link is in an inbox for as long as it is valid, which outlasts a deploy.
// Anything that changes what gets signed -- the field order, the separator, the
// length in front of the purpose, the encoding of the payload -- refuses every
// one of those links at once, and this fixture is what says so before the
// release rather than after it.
func TestATokenIssuedByAnEarlierBuildStillVerifies(t *testing.T) {
	s := encryption.NewSigner([]byte(goldenSignerKey))

	got, err := s.Verify("verify-email", goldenLiveToken)
	if err != nil {
		t.Fatalf("a token an earlier build of this package issued was refused: %v", err)
	}
	if want := "8:tenant-1|4:u-42|17:reader@example.com"; got != want {
		t.Errorf("the payload came back as %q, want %q", got, want)
	}
}

// TestAnOldTokenThatHasRunOutIsRefusedForTheRightReason.
//
// It pins two things at once, and only the fixture can pin them together.
// ErrExpired is reachable only after hmac.Equal has passed, so reaching it says
// the signature over a token issued before this build still verifies; and
// reaching it rather than a payload says the expiry is still enforced here,
// where the signature is checked, rather than left to a caller to look at.
// A build that stopped checking the expiry would hand back the payload and pass
// every round-trip test in this file.
func TestAnOldTokenThatHasRunOutIsRefusedForTheRightReason(t *testing.T) {
	s := encryption.NewSigner([]byte(goldenSignerKey))

	payload, err := s.Verify("password-reset", goldenExpiredToken)
	if !errors.Is(err, encryption.ErrExpired) {
		t.Fatalf("an expired token from an earlier build reported %v, and returned %q", err, payload)
	}
}

// TestTheSignerDoesNotHoldOntoTheCallersKey.
//
// The Encrypter copies the key it is given and says why; the Signer is the
// other half of the same package holding the same secret, so it owes the same
// promise. Without the copy, a caller that reuses its buffer changes what the
// application signs, and nothing anywhere reports it -- the links simply stop
// verifying.
func TestTheSignerDoesNotHoldOntoTheCallersKey(t *testing.T) {
	key := []byte(goldenSignerKey)
	s := encryption.NewSigner(key)

	token := s.Sign("verify-email", "user-1", time.Hour)
	for i := range key {
		key[i] = 'z'
	}

	if _, err := s.Verify("verify-email", token); err != nil {
		t.Fatalf("zeroing the caller's buffer changed what the signer signs with: %v", err)
	}
}

// TestASignedPayloadComesBackUnchanged is the base case: without it, nothing
// below is testing anything.
func TestASignedPayloadComesBackUnchanged(t *testing.T) {
	s := signer()

	token := s.Sign("verify-email", "user-1|reader@example.com", time.Hour)
	got, err := s.Verify("verify-email", token)
	if err != nil {
		t.Fatalf("a token this application just issued was refused: %v", err)
	}
	if got != "user-1|reader@example.com" {
		t.Errorf("the payload came back as %q", got)
	}
}

// TestATokenDoesNotWorkForAnotherPurpose.
//
// This is the reason the type exists rather than a bare HMAC. A verification
// link and a password reset link are both "a link in an e-mail with an id in
// it", signed by the same key -- and without the purpose in the signature,
// clicking the first one resets the password of the second.
func TestATokenDoesNotWorkForAnotherPurpose(t *testing.T) {
	s := signer()

	token := s.Sign("verify-email", "user-1", time.Hour)
	if _, err := s.Verify("password-reset", token); err == nil {
		t.Fatal("a verification token was accepted as a password reset")
	}
}

// TestATokenIsRefusedUnderAPurposeThatIsAPrefixOfItsOwn.
//
// The near miss, from outside: a purpose one character short of the one the
// token was issued for is a different purpose, not a shorter spelling of it.
// The collision this file cannot reach -- a purpose that ends where the body
// begins -- is held in internal_test.go, against sign itself, because a token
// carrying it is refused for an unrelated reason before Verify returns.
func TestATokenIsRefusedUnderAPurposeThatIsAPrefixOfItsOwn(t *testing.T) {
	s := signer()

	token := s.Sign("verify", "payload", time.Hour)
	if _, err := s.Verify("verif", token); err == nil {
		t.Fatal("a token verified under a purpose it was not issued for")
	}
}

// TestTheExpiryCannotBeMovedByEditingTheURL: the expiry is inside the signature,
// so a link that has run out cannot be extended by anyone holding it.
func TestTheExpiryCannotBeMovedByEditingTheURL(t *testing.T) {
	s := signer()

	token := s.Sign("verify-email", "user-1", -time.Minute)
	parts := strings.Split(token, ".")

	forged := parts[0] + "." + "9999999999" + "." + parts[2]
	if _, err := s.Verify("verify-email", forged); !errors.Is(err, encryption.ErrSignature) {
		t.Fatalf("an edited expiry was accepted: %v", err)
	}
}

// TestAnExpiredTokenSaysSo.
//
// It is the one failure a person can act on. Told "this link is not valid" they
// look for a bug; told it expired they ask for another one.
func TestAnExpiredTokenSaysSo(t *testing.T) {
	s := signer()

	_, err := s.Verify("verify-email", s.Sign("verify-email", "user-1", -time.Second))
	if !errors.Is(err, encryption.ErrExpired) {
		t.Fatalf("an expired token reported %v", err)
	}
	if !errors.Is(err, encryption.ErrSignature) {
		t.Error("ErrExpired does not unwrap to ErrSignature: a caller that checks one has to check both")
	}
}

// TestAnotherKeyDoesNotVerify: two applications sharing a domain do not share
// each other's links.
func TestAnotherKeyDoesNotVerify(t *testing.T) {
	token := signer().Sign("verify-email", "user-1", time.Hour)

	other := encryption.NewSigner([]byte("a different application key entirely"))
	if _, err := other.Verify("verify-email", token); err == nil {
		t.Fatal("a token signed with another key was accepted")
	}
}

// TestGarbageIsRefusedAndNotPanicked. The token arrives in a URL, so every shape
// of it is attacker-chosen.
func TestGarbageIsRefusedAndNotPanicked(t *testing.T) {
	s := signer()

	for _, token := range []string{"", ".", "..", "a.b.c", "a.b.c.d", "!!.!!.!!",
		strings.Repeat("a", 10000)} {
		if _, err := s.Verify("verify-email", token); err == nil {
			t.Errorf("%q was accepted", token)
		}
	}
}
