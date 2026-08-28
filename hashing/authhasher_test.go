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
// here answers Check with an error and auth.Hasher declares
// Check(value, hashedValue string) bool. Nothing in the repository satisfied
// auth.Hasher except a fake in a test file, so authentication had no production
// hasher at all.
var (
	_ auth.Hasher = (*hashing.AuthHasher)(nil)
	_ auth.Hasher = hashing.ForAuth(nil)
	_ auth.Hasher = hashing.ForAuth(hashing.NewBcryptHasher())
)

func TestAuthHasherMakesAHashItsOwnCheckAccepts(t *testing.T) {
	hasher := hashing.ForAuth(fastBcrypt())

	hashed, err := hasher.Make(storedPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !strings.HasPrefix(hashed, "$2") {
		t.Fatalf("Make wrote %q, want a bcrypt hash", hashed)
	}
	if !hasher.Check(storedPassword, hashed) {
		t.Fatal("Check refused the password Make hashed")
	}
	if hasher.Check(storedPassword+"r", hashed) {
		t.Fatal("Check accepted a password that was not the one hashed")
	}
}

func TestAuthHasherCheckRefusesAnEmptyHash(t *testing.T) {
	if hashing.ForAuth(fastBcrypt()).Check("anything", "") {
		t.Fatal("Check accepted a password against an empty hash")
	}
	if hashing.ForAuth(nil).Check("anything", "") {
		t.Fatal("Check accepted a password against an empty hash")
	}
}

// The narrow contract has nowhere to put an error, and the answer that fails
// safely is false -- never true, and never a panic.
func TestAuthHasherCheckIsFalseForACorruptColumn(t *testing.T) {
	for name, hasher := range map[string]*hashing.AuthHasher{
		"bcrypt":   hashing.ForAuth(fastBcrypt()),
		"argon2id": hashing.ForAuth(nil),
	} {
		t.Run(name, func(t *testing.T) {
			if hasher.Check("anything", "$2y$not-a-hash") {
				t.Fatal("Check answered true for an unreadable hash")
			}
		})
	}
}

func TestAuthHasherNeedsRehashFollowsTheHashersParameters(t *testing.T) {
	weak, err := fastBcrypt().Make(storedPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	if hashing.ForAuth(fastBcrypt()).NeedsRehash(weak) {
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

// A nil hasher is this package's own path, and the assertion is on what lands in
// the password column: argon2id.
//
// bcrypt was the answer here, and it disagreed with the argon2id hashing.Make
// writes -- two defaults for one operation, deciding between them what an
// application's password column would hold.
func TestForAuthWithNoHasherWritesArgon2id(t *testing.T) {
	hasher := hashing.ForAuth(nil)

	hashed, err := hasher.Make(storedPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	p, ok := hashing.Info(hashed)
	if !ok {
		t.Fatalf("ForAuth(nil) wrote %q, which is not a hash this package reads", hashed)
	}
	if p.Algorithm != hashing.Argon2id {
		t.Fatalf("ForAuth(nil) wrote %q, want %q", p.Algorithm, hashing.Argon2id)
	}
	if hashing.NeedsRehash(hashed) {
		t.Fatal("ForAuth(nil) wrote a hash that needs a rehash the moment it is written")
	}
	if !hasher.Check(storedPassword, hashed) {
		t.Fatal("Check refused the password Make hashed")
	}
}

// An account imported from an application that hashed with bcrypt signs in
// through the adapter, and is reported as due for a rewrite.
//
// The hash is one this package wrote at 12 rounds, copied down as a literal, so
// what the test reads back is not what the run just produced.
func TestForAuthWithNoHasherReadsAnImportedBcryptColumn(t *testing.T) {
	hasher := hashing.ForAuth(nil)
	imported := storedHashes["bcrypt at 12 rounds"].hash

	if !hasher.Check(storedPassword, imported) {
		t.Fatal("an imported bcrypt account cannot sign in")
	}
	if hasher.Check(storedPassword+"r", imported) {
		t.Fatal("Check accepted a password that was not the one hashed")
	}
	if !hasher.NeedsRehash(imported) {
		t.Fatal("an imported bcrypt hash was not flagged for a rewrite, so it would stay bcrypt forever")
	}
}
