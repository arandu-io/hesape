package hashing_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/hashing"
)

// The assertion this whole file exists for: a hasher from this package, behind
// the adapter, IS the interface hesape/auth consumes.
//
// It did not compile before hashing.AuthHasher was written, because every hasher
// here declares Check(value, hashedValue string, options ...Options)
// (bool, error) and auth.Hasher declares Check(value, hashedValue string) bool.
// Nothing in the repository satisfied auth.Hasher except a fake in a test file,
// so authentication had no production hasher at all.
var (
	_ auth.Hasher = (*hashing.AuthHasher)(nil)
	_ auth.Hasher = hashing.ForAuth(hashing.NewBcryptHasher())
	_ auth.Hasher = hashing.ForAuth(hashing.NewArgon2IdHasher())
)

// cheapBcryptHasher is bcrypt at the lowest cost the algorithm accepts. Tests
// hash a dozen passwords and the default of 12 rounds is a quarter of a second
// each. It is cheapBcrypt, which is that cost as a configuration section, built
// into a hasher.
func cheapBcryptHasher() *hashing.BcryptHasher {
	return hashing.NewBcryptHasher(hashing.Options{Rounds: 4})
}

// cheapArgon2id is argon2id with the smallest parameters the parser accepts, for
// the same reason.
func cheapArgon2id() *hashing.Argon2IdHasher {
	return hashing.NewArgon2IdHasher(hashing.Options{Memory: 64, Time: 1, Threads: 1})
}

func TestAuthHasherMakesAHashItsOwnCheckAccepts(t *testing.T) {
	hasher := hashing.ForAuth(cheapBcryptHasher())

	hashed, err := hasher.Make("correct horse battery staple")
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !strings.HasPrefix(hashed, "$2") {
		t.Fatalf("Make wrote %q, want a bcrypt hash", hashed)
	}
	if !hasher.Check("correct horse battery staple", hashed) {
		t.Fatal("Check refused the password Make hashed")
	}
	if hasher.Check("correct horse battery stapler", hashed) {
		t.Fatal("Check accepted a password that was not the one hashed")
	}
}

func TestAuthHasherCheckRefusesAnEmptyHash(t *testing.T) {
	if hashing.ForAuth(cheapBcryptHasher()).Check("anything", "") {
		t.Fatal("Check accepted a password against an empty hash")
	}
}

// A hasher built with 'verify' answers ErrWrongAlgorithm for a hash written by
// another algorithm. The narrow contract has nowhere to put an error, and the
// answer that fails safely is false -- never true, and never a panic.
func TestAuthHasherCheckIsFalseWhenTheHasherErrors(t *testing.T) {
	argon, err := cheapArgon2id().Make("correct horse battery staple")
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	strict := hashing.NewBcryptHasher(hashing.Options{Rounds: 4, Verify: true})

	// The underlying hasher reports the mismatch as an error, not as false.
	if _, err := strict.Check("correct horse battery staple", argon); err == nil {
		t.Fatal("the bcrypt hasher accepted an argon2id hash under 'verify'; the test proves nothing")
	}
	if hashing.ForAuth(strict).Check("correct horse battery staple", argon) {
		t.Fatal("Check answered true for a hash its hasher refused with an error")
	}
}

func TestAuthHasherCheckIsFalseForACorruptColumn(t *testing.T) {
	if hashing.ForAuth(cheapBcryptHasher()).Check("anything", "$2y$not-a-hash") {
		t.Fatal("Check answered true for an unreadable hash")
	}
}

func TestAuthHasherNeedsRehashFollowsTheHashersParameters(t *testing.T) {
	weak, err := cheapBcryptHasher().Make("correct horse battery staple")
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	if hashing.ForAuth(cheapBcryptHasher()).NeedsRehash(weak) {
		t.Fatal("NeedsRehash asked to rewrite a hash made with its own parameters")
	}

	stronger := hashing.ForAuth(hashing.NewBcryptHasher(hashing.Options{Rounds: 6}))
	if !stronger.NeedsRehash(weak) {
		t.Fatal("NeedsRehash left a hash made at 4 rounds alone under a 6-round hasher")
	}
	if !stronger.NeedsRehash("not a hash at all") {
		t.Fatal("NeedsRehash left an unreadable hash alone")
	}
}

// A nil hasher is bcrypt on its class defaults, which is what
// HashManager::getDefaultDriver falls back to.
func TestForAuthWithNoHasherIsBcrypt(t *testing.T) {
	hasher := hashing.ForAuth(nil)

	if _, ok := hasher.Hasher().(*hashing.BcryptHasher); !ok {
		t.Fatalf("ForAuth(nil) forwards to %T, want *hashing.BcryptHasher", hasher.Hasher())
	}
}

// A *HashManager is a Hasher, so an application that configures hashing through
// the hashing.driver key wires the manager into the guard.
func TestForAuthTakesAHashManager(t *testing.T) {
	manager, err := hashing.NewHashManager(fakeConfig{
		"hashing.driver": hashing.DriverBcrypt,
		"hashing.bcrypt": cheapBcrypt(),
	})
	if err != nil {
		t.Fatalf("NewHashManager: %v", err)
	}

	hasher := hashing.ForAuth(manager)

	hashed, err := hasher.Make("correct horse battery staple")
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !hasher.Check("correct horse battery staple", hashed) {
		t.Fatal("the manager behind the adapter refused the password it hashed")
	}
	if params, ok := hashing.Info(hashed); !ok || params.Cost != 4 {
		t.Fatalf("the manager hashed at %+v, want the configured cost of 4", params)
	}
}
