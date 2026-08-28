package onetime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
)

var testKey = []byte("0123456789abcdef0123456789abcdef")

// fixedRandom hands out bytes that were chosen rather than drawn.
type fixedRandom struct {
	data []byte
	read int
}

func (f *fixedRandom) Read(p []byte) (int, error) {
	if f.read >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.read:])
	f.read += n
	return n, nil
}

func testCodes(t *testing.T, store cache.Store, at time.Time) *Codes {
	t.Helper()

	codes, err := New(store, testKey, Config{Now: func() time.Time { return at }})
	if err != nil {
		t.Fatalf("building the codes: %v", err)
	}
	return codes
}

// TestTheScopeBindsPurposeAndSubject checks the first of the two places the pair
// is bound: the name of the record.
//
// Changing either string has to change the name, or one lookup would find the
// other's record. The last case is the one a joined string would fail: with a
// separator, "reset" plus "a:b" and "reset:a" plus "b" name the same entry.
func TestTheScopeBindsPurposeAndSubject(t *testing.T) {
	codes := testCodes(t, cache.NewArrayStore(), time.Now())

	base := codes.scope("confirm-address", "person-1")

	if same := codes.scope("delete-account", "person-1"); same == base {
		t.Error("two purposes name the same record")
	}
	if same := codes.scope("confirm-address", "person-2"); same == base {
		t.Error("two subjects name the same record")
	}
	if codes.scope("reset", "a:b") == codes.scope("reset:a", "b") {
		t.Error("the purpose and the subject run together, so one pair names another pair's record")
	}
	if codes.scope("confirm-address", "person-1") != base {
		t.Error("the same purpose and subject named two different records")
	}
}

// TestTheDigestBindsEverythingItIsMeantTo checks the second place, which is what
// still refuses a code if the first one is ever loosened: the stored digest
// changes when any of the four inputs does.
func TestTheDigestBindsEverythingItIsMeantTo(t *testing.T) {
	codes := testCodes(t, cache.NewArrayStore(), time.Now())

	base := codes.digest("confirm-address", "person-1", "nonce-1", "123456")

	cases := map[string][]byte{
		"the purpose": codes.digest("delete-account", "person-1", "nonce-1", "123456"),
		"the subject": codes.digest("confirm-address", "person-2", "nonce-1", "123456"),
		"the issue":   codes.digest("confirm-address", "person-1", "nonce-2", "123456"),
		"the code":    codes.digest("confirm-address", "person-1", "nonce-1", "123457"),
	}
	for changed, other := range cases {
		if bytes.Equal(base, other) {
			t.Errorf("changing %s left the digest untouched", changed)
		}
	}

	if !bytes.Equal(base, codes.digest("confirm-address", "person-1", "nonce-1", "123456")) {
		t.Error("the same four inputs produced two different digests")
	}

	// The fields are length-prefixed, so no rearrangement of the same characters
	// collides. Without that, a subject ending in what the next field begins
	// with would let one person's digest be another's.
	if bytes.Equal(
		codes.digest("confirm", "address-1", "nonce", "123456"),
		codes.digest("confirmaddress", "-1", "nonce", "123456"),
	) {
		t.Error("the digest runs its fields together, so a different pair produced the same digest")
	}
}

// TestTheDigestIsKeyed is what makes storing a digest of six digits worth
// anything: a million codes is a table anybody can build, so the value of the
// stored digest rests entirely on the reader not having the key.
func TestTheDigestIsKeyed(t *testing.T) {
	mine := testCodes(t, cache.NewArrayStore(), time.Now())

	theirs, err := New(cache.NewArrayStore(), []byte("ffffffffffffffffffffffffffffffff"), Config{})
	if err != nil {
		t.Fatalf("building the second codes: %v", err)
	}

	if bytes.Equal(
		mine.digest("confirm-address", "person-1", "nonce-1", "123456"),
		theirs.digest("confirm-address", "person-1", "nonce-1", "123456"),
	) {
		t.Error("two applications with different keys computed the same digest, so the digest is not keyed at all")
	}
	if bytes.Equal(mine.key, testKey) {
		t.Error("the application key is used directly, so a digest here could be replayed against another use of the same secret")
	}
}

// TestTheStoreNeverHoldsTheCodeOrTheSubject reads back everything the store
// holds after an issue and looks for what must not be in it.
//
// The plain search is not enough on its own -- a code written into a JSON []byte
// field arrives as base64 and would slip past it -- so the record is decoded and
// its digest examined as well.
func TestTheStoreNeverHoldsTheCodeOrTheSubject(t *testing.T) {
	store := cache.NewArrayStore()
	codes := testCodes(t, store, time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC))

	// Chosen bytes rather than drawn ones: the digits below are the code, and
	// the sixteen that follow are the nonce, so every byte the store ends up
	// holding is known before the call.
	codes.random = &fixedRandom{data: append([]byte{1, 2, 3, 4, 5, 6}, bytes.Repeat([]byte{0xAB}, 16)...)}

	const subject = "person@example.test"
	code, err := codes.Issue(context.Background(), "confirm-address", subject)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if code != "123456" {
		t.Fatalf("the rigged randomness produced %q, want 123456, so this test is not looking at what it thinks", code)
	}

	entries := store.All()
	if len(entries) != 1 {
		t.Fatalf("the store holds %d entries after one issue, want 1", len(entries))
	}

	for key, entry := range entries {
		if strings.Contains(key, code) || strings.Contains(key, subject) || strings.Contains(key, "confirm-address") {
			t.Errorf("the key %q carries the code, the subject or the purpose", key)
		}
		if bytes.Contains(entry.Value, []byte(code)) {
			t.Errorf("the entry under %q carries the code in the clear: %s", key, entry.Value)
		}
		if bytes.Contains(entry.Value, []byte(subject)) {
			t.Errorf("the entry under %q carries the subject: %s", key, entry.Value)
		}
		if bytes.Contains(entry.Value, []byte(base64.StdEncoding.EncodeToString([]byte(code)))) {
			t.Errorf("the entry under %q carries the code encoded rather than hashed: %s", key, entry.Value)
		}
		if bytes.Contains(entry.Value, []byte(hex.EncodeToString([]byte(code)))) {
			t.Errorf("the entry under %q carries the code in hexadecimal: %s", key, entry.Value)
		}

		var stored record
		if err := json.Unmarshal(entry.Value, &stored); err != nil {
			t.Fatalf("the stored record does not decode: %v", err)
		}
		if string(stored.Digest) == code || bytes.Contains(stored.Digest, []byte(code)) {
			t.Errorf("the stored digest is the code, or contains it: %q", stored.Digest)
		}
		if len(stored.Digest) != 32 {
			t.Errorf("the stored digest is %d bytes, want the 32 of a full digest", len(stored.Digest))
		}
		if !bytes.Equal(stored.Digest, codes.digest("confirm-address", subject, stored.Nonce, code)) {
			t.Error("the stored digest is not the keyed digest of the code, so what is stored is something else")
		}
	}
}

// TestTheGeneratorThrowsAwayTheBiasedBytes proves the rejection is real.
//
// Ten does not divide 256, so bytes 250 to 255 have to be discarded rather than
// folded in. The source below hands out exactly those six first: if they were
// taken modulo ten they would become 0 through 5 and the code would begin with
// them, and the test would see it.
func TestTheGeneratorThrowsAwayTheBiasedBytes(t *testing.T) {
	codes := testCodes(t, cache.NewArrayStore(), time.Now())
	codes.random = &fixedRandom{data: []byte{250, 251, 252, 253, 254, 255, 9, 8, 7, 6, 5, 4}}

	code, err := codes.generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if code != "987654" {
		t.Errorf("the code is %q, want 987654 -- the six bytes above the last multiple of ten were not thrown away", code)
	}
}

// TestTheGeneratorIsUniformOverTheDigits feeds the generator every byte it
// accepts, exactly once, and counts what comes out.
//
// Two hundred and forty bytes, 0 through 239: none of them is rejected, and
// twenty-four of them map to each digit. Anything other than twenty-four of each
// means the mapping favours some digits, which is the whole of what modulo bias
// is.
func TestTheGeneratorIsUniformOverTheDigits(t *testing.T) {
	accepted := make([]byte, 240)
	for i := range accepted {
		accepted[i] = byte(i)
	}

	codes := testCodes(t, cache.NewArrayStore(), time.Now())
	codes.random = &fixedRandom{data: accepted}

	counts := map[rune]int{}
	produced := 0
	for {
		code, err := codes.generate()
		if err != nil {
			break
		}
		produced++
		for _, digit := range code {
			counts[digit]++
		}
	}

	if produced != 40 {
		t.Fatalf("%d codes came out of 240 bytes, want 40 -- the generator is not spending one byte a digit", produced)
	}
	if len(counts) != 10 {
		t.Fatalf("%d distinct digits came out, want 10", len(counts))
	}
	for digit, count := range counts {
		if count != 24 {
			t.Errorf("the digit %c came up %d times in 240, want 24", digit, count)
		}
	}
}

func TestIssueFailsWhenThereIsNoRandomnessToBeHad(t *testing.T) {
	t.Run("ForTheCode", func(t *testing.T) {
		codes := testCodes(t, cache.NewArrayStore(), time.Now())
		codes.random = &fixedRandom{}

		if _, err := codes.Issue(context.Background(), "confirm-address", "person-1"); err == nil {
			t.Fatal("Issue produced a code with no randomness to make one out of")
		}
	})

	t.Run("ForTheNonce", func(t *testing.T) {
		codes := testCodes(t, cache.NewArrayStore(), time.Now())
		// Enough for the code and not enough for the nonce.
		codes.random = &fixedRandom{data: []byte{1, 2, 3, 4, 5, 6}}

		_, err := codes.Issue(context.Background(), "confirm-address", "person-1")
		if err == nil {
			t.Fatal("Issue produced a record with no nonce in it")
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			t.Errorf("Issue = %v, want it to carry the failure of the source", err)
		}
	})
}

// TestAnUnreadableRecordIsRefused covers the branch nothing else reaches: a
// record that is in the store and is not a record.
func TestAnUnreadableRecordIsRefused(t *testing.T) {
	store := cache.NewArrayStore()
	codes := testCodes(t, store, time.Now())
	ctx := context.Background()

	scope := codes.scope("confirm-address", "person-1")
	if err := store.Put(ctx, recordKey(scope), []byte("this is not a record"), time.Minute); err != nil {
		t.Fatalf("writing the rubbish: %v", err)
	}

	if err := codes.Consume(ctx, "confirm-address", "person-1", "123456"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Consume against an unreadable record = %v, want ErrUnavailable", err)
	}

	// Issue steps over the same rubbish rather than refusing: it is about to
	// replace it, and a cooldown it cannot read is a cooldown that has passed.
	if _, err := codes.Issue(ctx, "confirm-address", "person-1"); err != nil {
		t.Errorf("Issue over an unreadable record = %v, want nil", err)
	}
}

// TestTheKeysAreNamedApart is a small guard on the three key spaces one code
// occupies: sharing any two of them would make the attempt counter, the record
// and the spent marker the same entry.
func TestTheKeysAreNamedApart(t *testing.T) {
	const scope, nonce = "scope", "nonce"

	keys := []string{recordKey(scope), attemptKey(scope, nonce), spentKey(scope, nonce)}
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			t.Errorf("%q names two different things", key)
		}
		seen[key] = true
		if !strings.HasPrefix(key, "onetime:") {
			t.Errorf("%q is not under this package's prefix, so it can collide with another package's entry", key)
		}
	}
}
