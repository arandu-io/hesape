package hashing_test

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/arandu-io/hesape/hashing"
)

// fastBcrypt keeps the work factor at the floor: the tests exercise behaviour,
// and twelve rounds costs a second per call.
func fastBcrypt() *hashing.BcryptHasher {
	return hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost})
}

func TestAbstractHasherCheck(t *testing.T) {
	h := hashing.NewArgon2IdHasher()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	var base hashing.AbstractHasher
	if !base.Check(validPassword, hash) {
		t.Fatal("AbstractHasher.Check rejected the right password")
	}
	if base.Check(validPassword+"x", hash) {
		t.Fatal("AbstractHasher.Check accepted the wrong password")
	}
	// An empty hashed value is refused before it is verified.
	if base.Check(validPassword, "") {
		t.Fatal("AbstractHasher.Check accepted an empty hash")
	}
	if base.Check("", "") {
		t.Fatal("AbstractHasher.Check accepted two empty strings")
	}
	if base.Check(validPassword, "plaintext") {
		t.Fatal("AbstractHasher.Check accepted a value that is not a hash")
	}
}

func TestAbstractHasherInfo(t *testing.T) {
	h := hashing.NewArgonHasher(hashing.Options{Memory: 2048, Time: 3, Threads: 1})
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	p, ok := h.Info(hash)
	if !ok {
		t.Fatal("Info did not read a hash the hasher wrote")
	}
	if p.Algorithm != hashing.Argon2i || p.Memory != 2048 || p.Time != 3 || p.Threads != 1 {
		t.Fatalf("Params = %+v, want the hasher's own parameters", p)
	}
	if _, ok := h.Info("plaintext"); ok {
		t.Fatal("Info read a value that is not a hash")
	}
}

// TestArgonHasherDefaults pins the defaults: memory 1024, time 2, threads 2.
// They are read back off the hash the hasher writes.
func TestArgonHasherDefaults(t *testing.T) {
	h := hashing.NewArgonHasher()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	p, ok := hashing.Info(hash)
	if !ok {
		t.Fatal("Info did not read the hash")
	}
	if p.Algorithm != hashing.Argon2i {
		t.Fatalf("Algorithm = %q, want %q", p.Algorithm, hashing.Argon2i)
	}
	if p.Memory != 1024 || p.Time != 2 || p.Threads != 2 {
		t.Fatalf("Params = %+v, want m=1024,t=2,p=2", p)
	}
}

func TestArgon2IdHasherWritesArgon2id(t *testing.T) {
	h := hashing.NewArgon2IdHasher()
	if h.Algorithm() != hashing.Argon2id {
		t.Fatalf("Algorithm() = %q, want %q", h.Algorithm(), hashing.Argon2id)
	}
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash = %q, want an argon2id PHC string", hash)
	}
	ok, err := h.Check(validPassword, hash)
	if err != nil || !ok {
		t.Fatalf("Check = %v, %v; want true, nil", ok, err)
	}
}

func TestArgonHasherMakeAndCheck(t *testing.T) {
	for _, h := range []*hashing.ArgonHasher{
		hashing.NewArgonHasher(),
		&hashing.NewArgon2IdHasher().ArgonHasher,
	} {
		hash, err := h.Make(validPassword)
		if err != nil {
			t.Fatalf("Make: %v", err)
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

		// An empty hashed value is false before anything is verified.
		ok, err = h.Check(validPassword, "")
		if ok || err != nil {
			t.Fatalf("Check with an empty hash = %v, %v; want false, nil", ok, err)
		}
	}
}

// TestArgonHasherMakeEmptyValue covers the zero-length password. It is hashed
// rather than refused, because refusing it here would hide a caller that lost
// the field on the way in.
func TestArgonHasherMakeEmptyValue(t *testing.T) {
	h := hashing.NewArgon2IdHasher()
	hash, err := h.Make("")
	if err != nil {
		t.Fatalf("Make(\"\"): %v", err)
	}
	if ok, err := h.Check("", hash); err != nil || !ok {
		t.Fatalf("Check on the empty password = %v, %v; want true, nil", ok, err)
	}
	if ok, _ := h.Check("x", hash); ok {
		t.Fatal("the hash of the empty password verified another password")
	}
}

// TestArgonHasherMakeOptions covers $options overriding the hasher's own costs
// for a single call, without changing the hasher.
func TestArgonHasherMakeOptions(t *testing.T) {
	h := hashing.NewArgonHasher()
	hash, err := h.Make(validPassword, hashing.Options{Memory: 4096, Time: 1, Threads: 1})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	p, _ := hashing.Info(hash)
	if p.Memory != 4096 || p.Time != 1 || p.Threads != 1 {
		t.Fatalf("Params = %+v, want the option costs", p)
	}

	after, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if p, _ := hashing.Info(after); p.Memory != 1024 {
		t.Fatalf("Memory = %d after an options call, want the hasher's own 1024", p.Memory)
	}
}

func TestArgonHasherMakeRejectsImpossibleParameters(t *testing.T) {
	cases := map[string]*hashing.ArgonHasher{
		"zero time":    hashing.NewArgonHasher().SetTime(0),
		"zero threads": hashing.NewArgonHasher().SetThreads(0),
		"memory below eight times threads": hashing.NewArgonHasher().
			SetMemory(8).SetThreads(4),
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.Make(validPassword); err == nil {
				t.Fatal("Make accepted parameters argon2 cannot run with")
			}
		})
	}
}

// TestArgonHasherCheckVerifiesAlgorithm covers the Verify option: Check fails
// when the stored hash was written by another algorithm.
func TestArgonHasherCheckVerifiesAlgorithm(t *testing.T) {
	argon2i, err := hashing.NewArgonHasher().Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	verifying := hashing.NewArgon2IdHasher(hashing.Options{Verify: true})
	ok, err := verifying.Check(validPassword, argon2i)
	if !errors.Is(err, hashing.ErrWrongAlgorithm) {
		t.Fatalf("error = %v, want ErrWrongAlgorithm", err)
	}
	if ok {
		t.Fatal("Check accepted a hash written by another algorithm")
	}

	// Without Verify, the algorithm is read off the hash itself and it does
	// not matter which hasher was asked.
	ok, err = hashing.NewArgon2IdHasher().Check(validPassword, argon2i)
	if err != nil || !ok {
		t.Fatalf("Check without verify = %v, %v; want true, nil", ok, err)
	}

	// An empty hashed value is false before the algorithm check runs.
	if ok, err := verifying.Check(validPassword, ""); ok || err != nil {
		t.Fatalf("Check with an empty hash = %v, %v; want false, nil", ok, err)
	}

	// A value that is not a hash has no algorithm, so the check fails.
	if _, err := verifying.Check(validPassword, "plaintext"); !errors.Is(err, hashing.ErrWrongAlgorithm) {
		t.Fatalf("error = %v, want ErrWrongAlgorithm", err)
	}
}

func TestArgonHasherNeedsRehash(t *testing.T) {
	h := hashing.NewArgon2IdHasher()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	if h.NeedsRehash(hash) {
		t.Fatal("a hash written by this hasher must not need a rehash")
	}
	if !h.NeedsRehash(hash, hashing.Options{Memory: 4096}) {
		t.Fatal("a stronger option must flag the hash for rehash")
	}
	if !h.NeedsRehash("plaintext") {
		t.Fatal("a value that is not a hash must be flagged for rehash")
	}
	if !h.NeedsRehash("") {
		t.Fatal("an empty value must be flagged for rehash")
	}

	// The algorithm is part of what password_needs_rehash compares.
	if !hashing.NewArgonHasher().NeedsRehash(hash) {
		t.Fatal("an argon2id hash must be flagged for rehash by the argon2i hasher")
	}

	bcryptHash, err := fastBcrypt().Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.NeedsRehash(bcryptHash) {
		t.Fatal("a bcrypt hash must be flagged for rehash by an argon hasher")
	}

	// A weaker hash is a rehash too, not only a stronger one.
	weaker, err := h.Make(validPassword, hashing.Options{Memory: 512, Time: 1, Threads: 1})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.NeedsRehash(weaker) {
		t.Fatal("a hash with weaker parameters must be flagged for rehash")
	}
}

func TestArgonHasherVerifyConfiguration(t *testing.T) {
	h := hashing.NewArgon2IdHasher()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.VerifyConfiguration(hash) {
		t.Fatal("VerifyConfiguration rejected the hasher's own hash")
	}

	// Weaker than configured is still valid: the comparison is not an equality.
	weaker, err := h.Make(validPassword, hashing.Options{Memory: 512, Time: 1, Threads: 1})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.VerifyConfiguration(weaker) {
		t.Fatal("VerifyConfiguration rejected a hash weaker than the configuration")
	}

	// Stronger than configured is not.
	stronger, err := h.Make(validPassword, hashing.Options{Memory: 8192})
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if h.VerifyConfiguration(stronger) {
		t.Fatal("VerifyConfiguration accepted a hash stronger than the configuration")
	}

	// Wrong algorithm, and values that carry no options at all.
	if h.VerifyConfiguration(mustMake(t, hashing.NewArgonHasher())) {
		t.Fatal("VerifyConfiguration accepted an argon2i hash")
	}
	if h.VerifyConfiguration("plaintext") {
		t.Fatal("VerifyConfiguration accepted a value that is not a hash")
	}
	if h.VerifyConfiguration("") {
		t.Fatal("VerifyConfiguration accepted an empty value")
	}
}

func TestArgonHasherSetters(t *testing.T) {
	h := hashing.NewArgonHasher()
	if got := h.SetMemory(4096).SetTime(3).SetThreads(1); got != h {
		t.Fatal("the setters must return the hasher so calls chain")
	}
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	p, _ := hashing.Info(hash)
	if p.Memory != 4096 || p.Time != 3 || p.Threads != 1 {
		t.Fatalf("Params = %+v, want the values the setters wrote", p)
	}

	id := hashing.NewArgon2IdHasher()
	if got := id.SetMemory(2048).SetTime(1).SetThreads(1); got != id {
		t.Fatal("the argon2id setters must return the argon2id hasher")
	}
	hash, err = id.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	p, _ = hashing.Info(hash)
	if p.Algorithm != hashing.Argon2id || p.Memory != 2048 {
		t.Fatalf("Params = %+v, want argon2id at m=2048", p)
	}
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
	if h.Algorithm() != hashing.Bcrypt {
		t.Fatalf("Algorithm() = %q, want %q", h.Algorithm(), hashing.Bcrypt)
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
	h := hashing.NewBcryptHasher()
	cheap, err := fastBcrypt().Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.NeedsRehash(cheap) {
		t.Fatalf("the default hasher accepted cost %d, so its rounds are not 12", bcrypt.MinCost)
	}
	if h.NeedsRehash(cheap, hashing.Options{Rounds: bcrypt.MinCost}) {
		t.Fatal("the rounds option did not reach needsRehash")
	}
}

// TestBcryptHasherLimit covers the Limit option. The comparison is not an
// equality, so a value of exactly the limit is accepted even though the message
// reads "less than": the behaviour is in the body, not in the message.
func TestBcryptHasherLimit(t *testing.T) {
	h := hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost, Limit: 8})

	if _, err := h.Make("12345678"); err != nil {
		t.Fatalf("Make on a value of exactly the limit: %v", err)
	}
	_, err := h.Make("123456789")
	if !errors.Is(err, hashing.ErrValueTooLong) {
		t.Fatalf("error = %v, want ErrValueTooLong", err)
	}

	// A zero limit is no limit: nothing is refused for its length.
	long := strings.Repeat("a", 64)
	if _, err := fastBcrypt().Make(long); err != nil {
		t.Fatalf("Make with no limit: %v", err)
	}

	// bcrypt itself stops at 72 bytes, which is the RuntimeException case.
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
	if !h.NeedsRehash(hash, hashing.Options{Rounds: bcrypt.MinCost + 1}) {
		t.Fatal("a higher rounds option must flag the hash for rehash")
	}
	if !h.NeedsRehash("plaintext") {
		t.Fatal("a value that is not a hash must be flagged for rehash")
	}
	if !h.NeedsRehash("") {
		t.Fatal("an empty value must be flagged for rehash")
	}
	if !h.NeedsRehash(mustMake(t, hashing.NewArgon2IdHasher())) {
		t.Fatal("an argon2id hash must be flagged for rehash by the bcrypt hasher")
	}
}

func TestBcryptHasherVerifyConfiguration(t *testing.T) {
	h := hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost + 1})

	cheap, err := fastBcrypt().Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.VerifyConfiguration(cheap) {
		t.Fatal("VerifyConfiguration rejected a hash below the configured cost")
	}

	own, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if !h.VerifyConfiguration(own) {
		t.Fatal("VerifyConfiguration rejected the hasher's own hash")
	}

	stronger := hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost + 3})
	expensive, err := stronger.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if h.VerifyConfiguration(expensive) {
		t.Fatal("VerifyConfiguration accepted a hash above the configured cost")
	}

	if h.VerifyConfiguration(mustMake(t, hashing.NewArgon2IdHasher())) {
		t.Fatal("VerifyConfiguration accepted an argon2id hash")
	}
	if h.VerifyConfiguration("plaintext") {
		t.Fatal("VerifyConfiguration accepted a value that is not a hash")
	}
}

func TestBcryptHasherCheckVerifiesAlgorithm(t *testing.T) {
	argonHash := mustMake(t, hashing.NewArgon2IdHasher())

	verifying := hashing.NewBcryptHasher(hashing.Options{Rounds: bcrypt.MinCost, Verify: true})
	ok, err := verifying.Check(validPassword, argonHash)
	if !errors.Is(err, hashing.ErrWrongAlgorithm) {
		t.Fatalf("error = %v, want ErrWrongAlgorithm", err)
	}
	if ok {
		t.Fatal("Check accepted a hash written by another algorithm")
	}

	ok, err = fastBcrypt().Check(validPassword, argonHash)
	if err != nil || !ok {
		t.Fatalf("Check without verify = %v, %v; want true, nil", ok, err)
	}
}

func TestBcryptHasherSetRounds(t *testing.T) {
	h := fastBcrypt()
	if got := h.SetRounds(bcrypt.MinCost + 1); got != h {
		t.Fatal("SetRounds must return the hasher so calls chain")
	}
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	p, ok := hashing.Info(hash)
	if !ok || p.Cost != bcrypt.MinCost+1 {
		t.Fatalf("Params = %+v, want cost %d", p, bcrypt.MinCost+1)
	}
}

// hasher is the surface AbstractHasher's subclasses share, and asserting the
// three types satisfy it is how the parity is checked by the compiler.
type hasher interface {
	Make(value string, options ...hashing.Options) (string, error)
	Check(value, hashedValue string, options ...hashing.Options) (bool, error)
	NeedsRehash(hashedValue string, options ...hashing.Options) bool
	VerifyConfiguration(value string) bool
	Info(hashedValue string) (hashing.Params, bool)
	Algorithm() hashing.Algorithm
}

var (
	_ hasher = (*hashing.ArgonHasher)(nil)
	_ hasher = (*hashing.Argon2IdHasher)(nil)
	_ hasher = (*hashing.BcryptHasher)(nil)
)

func mustMake(t *testing.T, h hasher) string {
	t.Helper()
	hash, err := h.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	return hash
}
