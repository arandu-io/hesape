package hashing_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/arandu-io/hesape/hashing"
)

const validPassword = "correct horse battery"

func TestMakeAndCheck(t *testing.T) {
	hash, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if strings.Contains(hash, validPassword) {
		t.Fatal("the hash must not contain the password")
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("hash is not in PHC format: %s", hash)
	}
	if err := hashing.Check(validPassword, hash); err != nil {
		t.Fatalf("Check on the right password: %v", err)
	}
}

func TestCheckRejectsTheWrongPassword(t *testing.T) {
	hash, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	err = hashing.Check(validPassword+"x", hash)
	if !errors.Is(err, hashing.ErrInvalidPassword) {
		t.Fatalf("error = %v, want ErrInvalidPassword", err)
	}
}

// TestHashIsSalted proves the salt is per hash: two hashes of the same password
// must differ, or the database becomes a rainbow table lookup.
func TestHashIsSalted(t *testing.T) {
	a, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	b, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}

	if a == b {
		t.Fatal("two hashes of the same password are identical: the salt is not random")
	}
}

func TestMakeRejectsShortPassword(t *testing.T) {
	_, err := hashing.Make(strings.Repeat("a", hashing.MinPasswordLen-1))
	if err == nil {
		t.Fatal("Make accepted a password below the minimum length")
	}
}

func TestMakeRejectsLongPassword(t *testing.T) {
	_, err := hashing.Make(strings.Repeat("a", hashing.MaxPasswordLen+1))
	if err == nil {
		t.Fatal("Make accepted a password above the maximum length")
	}
}

// TestMakeCountsRunes guards the boundary against a byte count: a passphrase of
// accented characters is shorter than its UTF-8 encoding and must not be
// refused for it.
func TestMakeCountsRunes(t *testing.T) {
	if _, err := hashing.Make(strings.Repeat("á", hashing.MaxPasswordLen)); err != nil {
		t.Fatalf("Make refused %d runes: %v", hashing.MaxPasswordLen, err)
	}
}

func TestCheckRejectsMalformedHash(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"not phc":          "plaintext",
		"other scheme":     "$scrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5",
		"missing parts":    "$argon2id$v=19$m=65536,t=3,p=4",
		"bad params":       "$argon2id$v=19$m=x,t=y,p=z$c2FsdA$a2V5",
		"bad base64":       "$argon2id$v=19$m=65536,t=3,p=4$!!!$!!!",
		"unknown version":  argonString("argon2id", 20, 65536, 3, 4),
		"zero passes":      argonString("argon2id", argon2.Version, 65536, 0, 4),
		"zero parallelism": argonString("argon2id", argon2.Version, 65536, 3, 0),
		"short salt":       "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$" + base64.RawStdEncoding.EncodeToString(make([]byte, 32)),
		"short key":        "$argon2id$v=19$m=65536,t=3,p=4$" + base64.RawStdEncoding.EncodeToString(make([]byte, 16)) + "$a2V5",
		"truncated bcrypt": "$2a$10$short",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			err := hashing.Check(validPassword, encoded)
			if err == nil {
				t.Fatal("Check accepted a malformed hash")
			}
			// A corrupt column is not a wrong password, and reporting it as one
			// hides a broken database behind a login failure.
			if errors.Is(err, hashing.ErrInvalidPassword) {
				t.Fatalf("a malformed hash reported ErrInvalidPassword: %v", err)
			}
			if hashing.IsHashed(encoded) {
				t.Fatal("IsHashed accepted a malformed hash")
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if hashing.NeedsRehash(current) {
		t.Fatal("a hash written with the current parameters must not need a rehash")
	}

	legacy := argonString("argon2id", argon2.Version, 4096, 1, 1)
	if !hashing.NeedsRehash(legacy) {
		t.Fatal("a hash with weaker parameters must be flagged for rehash")
	}
	if !hashing.NeedsRehash("plaintext") {
		t.Fatal("a value that is not a hash must be flagged for rehash")
	}
}

func TestInfoReadsArgon2id(t *testing.T) {
	hash, err := hashing.Make(validPassword)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	p, ok := hashing.Info(hash)
	if !ok {
		t.Fatal("Info did not read a hash this package wrote")
	}
	if p.Algorithm != hashing.Argon2id {
		t.Fatalf("Algorithm = %q, want %q", p.Algorithm, hashing.Argon2id)
	}
	if p.Memory != 64*1024 || p.Time != 3 || p.Threads != 4 || p.KeyLen != 32 {
		t.Fatalf("Params = %+v, want the compiled-in argon2id parameters", p)
	}
	if p.Cost != 0 {
		t.Fatalf("Cost = %d on an argon2 hash, want 0", p.Cost)
	}
}

func TestIsHashedRejectsPlaintext(t *testing.T) {
	for _, v := range []string{"", validPassword, "$", "$2", "$argon2"} {
		if hashing.IsHashed(v) {
			t.Fatalf("IsHashed(%q) = true", v)
		}
	}
}

// TestCheckReadsBcrypt is the imported-database case: a users table written by
// a PHP application holds "$2y$" and must authenticate before it is rehashed.
// The minor version letter does not change the digest for an ASCII password, so
// rewriting it produces a hash the other dialects would have written.
func TestCheckReadsBcrypt(t *testing.T) {
	written, err := bcrypt.GenerateFromPassword([]byte(validPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	if !strings.HasPrefix(string(written), "$2a$") {
		t.Fatalf("bcrypt wrote %q, the test assumes the $2a$ dialect", string(written)[:4])
	}

	for _, minor := range []string{"a", "b", "y"} {
		t.Run("$2"+minor+"$", func(t *testing.T) {
			hash := "$2" + minor + "$" + strings.TrimPrefix(string(written), "$2a$")

			if err := hashing.Check(validPassword, hash); err != nil {
				t.Fatalf("Check on the right password: %v", err)
			}
			if err := hashing.Check(validPassword+"x", hash); !errors.Is(err, hashing.ErrInvalidPassword) {
				t.Fatalf("error = %v, want ErrInvalidPassword", err)
			}

			p, ok := hashing.Info(hash)
			if !ok {
				t.Fatal("Info did not read a bcrypt hash")
			}
			if p.Algorithm != hashing.Bcrypt || p.Cost != bcrypt.MinCost {
				t.Fatalf("Params = %+v, want bcrypt at cost %d", p, bcrypt.MinCost)
			}
			if !hashing.NeedsRehash(hash) {
				t.Fatal("a bcrypt hash must be flagged for rehash: Make writes argon2id")
			}
		})
	}
}

// TestCheckReadsArgon2i covers the other algorithm PHP's password_hash emits.
func TestCheckReadsArgon2i(t *testing.T) {
	const (
		memory  = 65536
		time    = 3
		threads = 4
	)
	salt := []byte("sixteen byte slt")
	key := argon2.Key([]byte(validPassword), salt, time, memory, threads, 32)
	hash := fmt.Sprintf("$argon2i$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, time, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)

	if err := hashing.Check(validPassword, hash); err != nil {
		t.Fatalf("Check on the right password: %v", err)
	}
	if err := hashing.Check(validPassword+"x", hash); !errors.Is(err, hashing.ErrInvalidPassword) {
		t.Fatalf("error = %v, want ErrInvalidPassword", err)
	}

	p, ok := hashing.Info(hash)
	if !ok {
		t.Fatal("Info did not read an argon2i hash")
	}
	if p.Algorithm != hashing.Argon2i {
		t.Fatalf("Algorithm = %q, want %q", p.Algorithm, hashing.Argon2i)
	}
	if !hashing.NeedsRehash(hash) {
		t.Fatal("an argon2i hash must be flagged for rehash: Make writes argon2id")
	}
}

// argonString builds a PHC string whose salt and key are well formed and whose
// digest is meaningless, which is what the parameter guards are read against.
func argonString(alg string, version, memory, time, threads int) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		alg, version, memory, time, threads,
		base64.RawStdEncoding.EncodeToString(make([]byte, 16)),
		base64.RawStdEncoding.EncodeToString(make([]byte, 32)),
	)
}
