package hashing_test

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/arandu-io/hesape/hashing"
)

// This hasher is the interface the adapter forwards to. The compiler is what
// checks it: a method that drifts out of the shape breaks here rather than at
// the call in hesape/auth.
var _ hashing.Hasher = (*hashing.BcryptHasher)(nil)

// fastBcrypt is bcrypt at the lowest cost the algorithm accepts. A test file
// that hashes a dozen values cannot afford the default of 12 rounds, which is a
// quarter of a second each.
func fastBcrypt() *hashing.BcryptHasher {
	return hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost})
}

func TestBcryptHasherMakeAndCheck(t *testing.T) {
	h := fastBcrypt()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("hash = %q, want a bcrypt hash", hash)
	}

	ok, err := h.Check(validPassword, hash)
	if err != nil || !ok {
		t.Fatalf("Check on the right password = %v, %v", ok, err)
	}
	ok, err = h.Check(validPassword+"x", hash)
	if err != nil {
		t.Fatalf("Check on the wrong password errored: %v", err)
	}
	if ok {
		t.Fatal("Check accepted the wrong password")
	}
	if ok, err := h.Check(validPassword, ""); ok || err != nil {
		t.Fatalf("Check with an empty hash = %v, %v; want false, nil", ok, err)
	}
}

// TestBcryptHasherDefaultRounds pins the default cost factor: 12.
func TestBcryptHasherDefaultRounds(t *testing.T) {
	cheap, err := fastBcrypt().Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !hashing.NewBcryptHasher().NeedsRehash(cheap) {
		t.Fatalf("the default hasher accepted cost %d, so its rounds are not 12", bcrypt.MinCost)
	}
}

// TestBcryptHasherMakeRefusesWhatBcryptCannotHash covers the one value bcrypt
// itself rejects. There is no length option to configure: a limit shorter than
// the algorithm's own only refuses passwords the algorithm would have taken.
func TestBcryptHasherMakeRefusesWhatBcryptCannotHash(t *testing.T) {
	if _, err := fastBcrypt().Make(strings.Repeat("a", 72)); err != nil {
		t.Fatalf("Make on a value of exactly 72 bytes: %v", err)
	}
	if _, err := fastBcrypt().Make(strings.Repeat("a", 73)); err == nil {
		t.Fatal("Make accepted a value bcrypt cannot hash")
	}
}

func TestBcryptHasherNeedsRehash(t *testing.T) {
	h := fastBcrypt()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	if h.NeedsRehash(hash) {
		t.Fatal("a hash written by this hasher must not need a rehash")
	}
	if !hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost + 1}).NeedsRehash(hash) {
		t.Fatal("a hasher at a higher cost must flag the hash for rehash")
	}
	if !h.NeedsRehash("plaintext") {
		t.Fatal("a value that is not a hash must be flagged for rehash")
	}
	if !h.NeedsRehash("") {
		t.Fatal("an empty value must be flagged for rehash")
	}

	argon2id, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.NeedsRehash(argon2id) {
		t.Fatal("an argon2id hash must be flagged for rehash by the bcrypt hasher")
	}
}

// The table this hasher exists for is one part way through a migration, so it
// has to read the argon2id rows the migration has already rewritten as well as
// the bcrypt rows it has not reached.
func TestBcryptHasherChecksTheHashesAMigrationLeavesBehind(t *testing.T) {
	argon2id, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	ok, err := fastBcrypt().Check(validPassword, argon2id)
	if err != nil || !ok {
		t.Fatalf("Check on an argon2id hash = %v, %v; want true, nil", ok, err)
	}
}

// An unreadable hash is an error and not a wrong password, and the two are
// different events: one is a corrupt column and the other is somebody mistyping.
func TestBcryptHasherCheckSeparatesACorruptColumnFromAWrongPassword(t *testing.T) {
	ok, err := fastBcrypt().Check(validPassword, "$2y$not-a-hash")
	if err == nil {
		t.Fatal("Check read a corrupt column as a wrong password")
	}
	if ok {
		t.Fatal("Check accepted a password against a corrupt column")
	}
}
