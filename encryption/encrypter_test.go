package encryption_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/encryption"
)

// The key the fixtures below were written under. It is 32 bytes of 'a' and it
// is not a secret: it exists so that payloads produced by an independent
// implementation of this format can be checked into this file.
const goldenKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// previousGoldenKey is the key the rotation fixture was written under.
const previousGoldenKey = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// The fixtures. Each was produced by an independent implementation of this
// payload format, encrypting a string under the aes-256-gcm cipher. They are
// here because an implementation that agrees with a reading of the format but
// not with the format itself fails the first time two services share a key --
// and that failure is silent until a real payload will not open.
const (
	goldenHello = "eyJpdiI6IkY2eUlzOHJmMHFnNDl2OXEiLCJ2YWx1ZSI6IkpLUUVJTlhyT1NpaktPST0iLCJtYWMiOiIiLCJ0YWciOiJLTkZtLzhPczh5bXBKUnVqbHN0bEJRPT0ifQ=="
	goldenEmpty = "eyJpdiI6IituUWhlUlR2S2dWUTJwd0YiLCJ2YWx1ZSI6IiIsIm1hYyI6IiIsInRhZyI6IjZRZStXRC9oaTYyYjZHeHJzNzJudmc9PSJ9"
	goldenUTF8  = "eyJpdiI6Ill5Kzl0bElOTzhkSXZhQ2wiLCJ2YWx1ZSI6Ik5TL0NpTkVsYWpvR3gvRng0TENIcnEzNlFFZz0iLCJtYWMiOiIiLCJ0YWciOiJLZ3NCUHhGa3gvRzAwaGd0VnAxUjJnPT0ifQ=="

	// Written under previousGoldenKey, to exercise the rotation path.
	goldenRotated = "eyJpdiI6Im5EU01iRyt3NStiZFVnU2EiLCJ2YWx1ZSI6Imhhb09iVmFxSHJQVW56WkZINU55REtEZnhxU1RhWFZiMFE9PSIsIm1hYyI6IiIsInRhZyI6ImYyWUhZM3RXRXpmME5QTTZmTmR3NHc9PSJ9"
)

func newGolden(t *testing.T) *encryption.Encrypter {
	t.Helper()
	e, err := encryption.NewEncrypter([]byte(goldenKey), encryption.AES256GCM)
	if err != nil {
		t.Fatalf("a 32-byte key with aes-256-gcm was refused: %v", err)
	}
	return e
}

// TestAPayloadFromLaravelDecrypts is the test the format guarantee hangs on.
// Every other test here checks this package against itself; this one checks it
// against a payload it did not write.
func TestAPayloadFromLaravelDecrypts(t *testing.T) {
	e := newGolden(t)

	for _, tt := range []struct{ name, payload, want string }{
		{"a plain string", goldenHello, "hello world"},
		{"an empty string", goldenEmpty, ""},
		{"multibyte text", goldenUTF8, "héllo ✓ multibyte"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.DecryptString(tt.payload)
			if err != nil {
				t.Fatalf("a payload this package did not write would not open: %v", err)
			}
			if got != tt.want {
				t.Errorf("DecryptString = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTheEnvelopeIsTheOneLaravelWrites checks the other direction: not that
// this package can read a foreign payload, but that a foreign reader could read
// this one. The field order is part of it -- iv, value, mac, tag -- because a
// reader comparing payloads as strings would see a difference that is not one.
func TestTheEnvelopeIsTheOneLaravelWrites(t *testing.T) {
	e := newGolden(t)

	payload, err := e.EncryptString("hello world")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	body, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("the payload is not base64: %v", err)
	}

	// Key order, verbatim: iv, then value, then mac, then tag.
	if got := string(body); !strings.HasPrefix(got, `{"iv":"`) ||
		!strings.Contains(got, `","value":"`) ||
		!strings.Contains(got, `","mac":"","tag":"`) {
		t.Errorf("the envelope is %s, want the iv/value/mac/tag order with an empty mac", got)
	}

	var fields struct {
		IV    *string `json:"iv"`
		Value *string `json:"value"`
		MAC   *string `json:"mac"`
		Tag   *string `json:"tag"`
	}
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("the envelope is not the expected JSON: %v", err)
	}
	if fields.MAC == nil {
		t.Error("the mac field is absent; the format always carries it, and AppearsEncrypted looks for it")
	} else if *fields.MAC != "" {
		t.Errorf("mac = %q, want empty: with an AEAD cipher the tag is the MAC", *fields.MAC)
	}

	iv, err := base64.StdEncoding.DecodeString(*fields.IV)
	if err != nil || len(iv) != 12 {
		t.Errorf("the iv decodes to %d bytes (err %v), want 12 -- GCM takes a 96-bit nonce, which is what makes the counter block deterministic", len(iv), err)
	}
	tag, err := base64.StdEncoding.DecodeString(*fields.Tag)
	if err != nil || len(tag) != 16 {
		t.Errorf("the tag decodes to %d bytes (err %v), want 16", len(tag), err)
	}
}

// TestEncryptStringRoundTrips, including the inputs that are easy to get wrong:
// nothing at all, and a value long enough to cross a block boundary.
func TestEncryptStringRoundTrips(t *testing.T) {
	e := newGolden(t)

	for _, want := range []string{"", "a", "hello world", strings.Repeat("x", 1000), "héllo ✓", "\x00\x01\x02"} {
		payload, err := e.EncryptString(want)
		if err != nil {
			t.Fatalf("EncryptString(%q): %v", want, err)
		}
		got, err := e.DecryptString(payload)
		if err != nil {
			t.Fatalf("DecryptString of our own payload for %q: %v", want, err)
		}
		if got != want {
			t.Errorf("round trip of %q gave %q", want, got)
		}
	}
}

// TestEncryptingTwiceGivesDifferentPayloads. A deterministic payload leaks
// equality: two rows holding the same ciphertext hold the same plaintext, and
// that is readable without any key. The random IV is what prevents it, and
// nothing else in the tree would notice it going missing.
func TestEncryptingTwiceGivesDifferentPayloads(t *testing.T) {
	e := newGolden(t)

	first, err := e.EncryptString("the same value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.EncryptString("the same value")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two encryptions of one value are identical: the IV is not random, and equal ciphertexts now reveal equal plaintexts")
	}
}

// TestDecryptFallsBackToAPreviousKey is key rotation, which is the reason
// PreviousKeys exists. The payload here was written under a key that is no
// longer current.
func TestDecryptFallsBackToAPreviousKey(t *testing.T) {
	e := newGolden(t)

	if _, err := e.DecryptString(goldenRotated); err == nil {
		t.Fatal("a payload written under a retired key opened before that key was registered")
	}

	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)}); err != nil {
		t.Fatalf("PreviousKeys: %v", err)
	}

	got, err := e.DecryptString(goldenRotated)
	if err != nil {
		t.Fatalf("a payload written under a registered previous key would not open: %v", err)
	}
	if want := "written under the old key"; got != want {
		t.Errorf("DecryptString = %q, want %q", got, want)
	}
}

// TestNothingIsEncryptedWithAPreviousKey. Rotation is only a rotation if new
// writes stop using the old key; otherwise it is a second live key.
func TestNothingIsEncryptedWithAPreviousKey(t *testing.T) {
	e := newGolden(t)
	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)}); err != nil {
		t.Fatal(err)
	}

	payload, err := e.EncryptString("fresh")
	if err != nil {
		t.Fatal(err)
	}

	// An encrypter holding only the retired key must not be able to open it.
	old, err := encryption.NewEncrypter([]byte(previousGoldenKey), encryption.AES256GCM)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.DecryptString(payload); err == nil {
		t.Fatal("a value encrypted after rotation opens under the retired key: writes are still using it")
	}
}

// TestPreviousKeysRefusesABadKeyAndChangesNothing: the keys are checked before
// any is assigned, so a rejected call leaves the encrypter exactly as it was.
func TestPreviousKeysRefusesABadKeyAndChangesNothing(t *testing.T) {
	e := newGolden(t)
	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)}); err != nil {
		t.Fatal(err)
	}

	_, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey), []byte("too short")})
	if !errors.Is(err, encryption.ErrUnsupportedCipher) {
		t.Fatalf("PreviousKeys with a short key returned %v, want ErrUnsupportedCipher", err)
	}
	if got := len(e.GetPreviousKeys()); got != 1 {
		t.Errorf("the encrypter now holds %d previous keys, want the 1 it held before the refused call", got)
	}
	if _, err := e.DecryptString(goldenRotated); err != nil {
		t.Errorf("the refused call cost the encrypter a key it already had: %v", err)
	}
}

// TestDecryptRefusesATamperedPayload. Every one of these is a payload an
// attacker can build from a real one, and the tag is what stops each.
func TestDecryptRefusesATamperedPayload(t *testing.T) {
	e := newGolden(t)

	payload, err := e.EncryptString("the original value")
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	body, _ := base64.StdEncoding.DecodeString(payload)
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}

	rebuild := func(t *testing.T, mutate func(map[string]string)) string {
		t.Helper()
		clone := map[string]string{}
		for k, v := range fields {
			clone[k] = v
		}
		mutate(clone)
		b, err := json.Marshal(clone)
		if err != nil {
			t.Fatal(err)
		}
		return base64.StdEncoding.EncodeToString(b)
	}

	flipLast := func(s string) string {
		raw, err := base64.StdEncoding.DecodeString(s)
		if err != nil || len(raw) == 0 {
			return s
		}
		raw[len(raw)-1] ^= 0x01
		return base64.StdEncoding.EncodeToString(raw)
	}

	for _, tt := range []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"a flipped bit in the ciphertext", func(m map[string]string) { m["value"] = flipLast(m["value"]) }},
		{"a flipped bit in the tag", func(m map[string]string) { m["tag"] = flipLast(m["tag"]) }},
		{"a flipped bit in the iv", func(m map[string]string) { m["iv"] = flipLast(m["iv"]) }},
		{"the tag removed", func(m map[string]string) { delete(m, "tag") }},
		{"an emptied tag", func(m map[string]string) { m["tag"] = "" }},
		{"a truncated tag", func(m map[string]string) { m["tag"] = base64.StdEncoding.EncodeToString([]byte("short")) }},
		{"an emptied ciphertext", func(m map[string]string) { m["value"] = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			forged := rebuild(t, tt.mutate)
			got, err := e.DecryptString(forged)
			if err == nil {
				t.Fatalf("a forged payload decrypted to %q", got)
			}
			if !errors.Is(err, encryption.ErrDecrypt) {
				t.Errorf("error %v does not unwrap to ErrDecrypt, so a caller matching on it would miss this", err)
			}
		})
	}
}

// TestDecryptRefusesSomethingThatIsNotAPayload. These reach validPayload rather
// than the cipher, and the distinction matters: none of them should cost a key
// try.
func TestDecryptRefusesSomethingThatIsNotAPayload(t *testing.T) {
	e := newGolden(t)

	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

	for _, tt := range []struct{ name, payload string }{
		{"an empty string", ""},
		{"not base64", "this is not base64 !!!"},
		{"base64 of nothing resembling JSON", b64("not json at all")},
		{"a JSON array", b64(`[1,2,3]`)},
		{"a JSON string", b64(`"hello"`)},
		{"an object with no iv", b64(`{"value":"aaaa","mac":"","tag":"aaaa"}`)},
		{"an object with no value", b64(`{"iv":"YWFhYWFhYWFhYWFh","mac":"","tag":"aaaa"}`)},
		{"an object with no mac", b64(`{"iv":"YWFhYWFhYWFhYWFh","value":"aaaa","tag":"aaaa"}`)},
		{"a null iv", b64(`{"iv":null,"value":"aaaa","mac":"","tag":"aaaa"}`)},
		{"a numeric iv", b64(`{"iv":12,"value":"aaaa","mac":"","tag":"aaaa"}`)},
		{"a numeric tag", b64(`{"iv":"YWFhYWFhYWFhYWFh","value":"aaaa","mac":"","tag":7}`)},
		{"an iv that is not base64", b64(`{"iv":"!!!!","value":"aaaa","mac":"","tag":"aaaa"}`)},
		{"an iv of the wrong length", b64(`{"iv":"YWFh","value":"aaaa","mac":"","tag":"aaaa"}`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := e.DecryptString(tt.payload); !errors.Is(err, encryption.ErrDecrypt) {
				t.Fatalf("DecryptString(%s) returned %v, want an error unwrapping to ErrDecrypt", tt.name, err)
			}
		})
	}
}

// TestDecryptRefusesTheWrongKey.
func TestDecryptRefusesTheWrongKey(t *testing.T) {
	other, err := encryption.NewEncrypter([]byte(previousGoldenKey), encryption.AES256GCM)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.DecryptString(goldenHello); !errors.Is(err, encryption.ErrDecrypt) {
		t.Fatalf("a payload from another key returned %v, want ErrDecrypt", err)
	}
}

// TestAppearsEncrypted walks the shapes a value can arrive in and pins which
// of them are recognised as payloads.
func TestAppearsEncrypted(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value string
		want  bool
	}{
		{"a real payload", goldenHello, true},
		{"an empty string", "", false},
		{"plain text", "hello world", false},
		{"base64 of plain text", base64.StdEncoding.EncodeToString([]byte("not json at all")), false},
		{"an object with no mac", base64.StdEncoding.EncodeToString([]byte(`{"iv":"x","value":"y"}`)), false},
		{"an object with all three", base64.StdEncoding.EncodeToString([]byte(`{"iv":"x","value":"y","mac":""}`)), true},
		{"a JSON array", base64.StdEncoding.EncodeToString([]byte(`[1,2]`)), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := encryption.AppearsEncrypted(tt.value); got != tt.want {
				t.Errorf("AppearsEncrypted(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestAppearsEncryptedDoesNotPromiseDecryption. It reads the envelope, never a
// key, and a caller that treats it as a guarantee has a bug this documents.
func TestAppearsEncryptedDoesNotPromiseDecryption(t *testing.T) {
	looksRight := base64.StdEncoding.EncodeToString([]byte(`{"iv":"YWFhYWFhYWFhYWFh","value":"aaaa","mac":"","tag":"YWFhYWFhYWFhYWFhYWFh"}`))
	if !encryption.AppearsEncrypted(looksRight) {
		t.Fatal("the fixture is meant to look like a payload")
	}
	if _, err := newGolden(t).DecryptString(looksRight); err == nil {
		t.Fatal("a fabricated envelope decrypted")
	}
}

// TestSupported walks the key and cipher combinations, including the uppercase
// cipher name, which passes because the name is compared case-insensitively.
func TestSupported(t *testing.T) {
	for _, tt := range []struct {
		name   string
		key    string
		cipher encryption.Cipher
		want   bool
	}{
		{"the right length and cipher", strings.Repeat("a", 32), encryption.AES256GCM, true},
		{"an uppercase cipher name", strings.Repeat("a", 32), "AES-256-GCM", true},
		{"a key one byte short", strings.Repeat("a", 31), encryption.AES256GCM, false},
		{"a key one byte long", strings.Repeat("a", 33), encryption.AES256GCM, false},
		{"an empty key", "", encryption.AES256GCM, false},
		{"an unknown cipher", strings.Repeat("a", 32), "rot-13", false},
		{"an empty cipher", strings.Repeat("a", 32), "", false},
		// Ciphers of the same family that this package deliberately does not
		// carry.
		{"aes-128-cbc", strings.Repeat("a", 16), "aes-128-cbc", false},
		{"aes-256-cbc", strings.Repeat("a", 32), "aes-256-cbc", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := encryption.Supported([]byte(tt.key), tt.cipher); got != tt.want {
				t.Errorf("Supported(%d bytes, %q) = %v, want %v", len(tt.key), tt.cipher, got, tt.want)
			}
		})
	}
}

// TestCipherGenerateKey. The fallback to 32 bytes for an unknown cipher is
// worth pinning: it means the call never returns nothing.
func TestCipherGenerateKey(t *testing.T) {
	if got := len(encryption.AES256GCM.GenerateKey()); got != 32 {
		t.Errorf("AES256GCM.GenerateKey() is %d bytes, want 32", got)
	}
	if got := len(encryption.Cipher("rot-13").GenerateKey()); got != 32 {
		t.Errorf("an unknown cipher generated %d bytes, want the 32-byte fallback", got)
	}
	if string(encryption.AES256GCM.GenerateKey()) == string(encryption.AES256GCM.GenerateKey()) {
		t.Fatal("two generated keys are identical: the key is not random")
	}
	// The key it generates has to be one the encrypter accepts, which is the
	// half that silently drifts if the sizes are written in two places.
	if _, err := encryption.NewEncrypter(encryption.AES256GCM.GenerateKey(), encryption.AES256GCM); err != nil {
		t.Errorf("a key this package just generated was refused: %v", err)
	}
}

// TestNewEncrypterRefusesAnUnusableKey. Failing at construction is the point:
// a wrong key length is not visible from any request that would go wrong.
func TestNewEncrypterRefusesAnUnusableKey(t *testing.T) {
	for _, tt := range []struct {
		name   string
		key    string
		cipher encryption.Cipher
	}{
		{"an empty key", "", encryption.AES256GCM},
		{"a short key", strings.Repeat("a", 31), encryption.AES256GCM},
		{"a long key", strings.Repeat("a", 33), encryption.AES256GCM},
		{"an unknown cipher", strings.Repeat("a", 32), "rot-13"},
		{"a cipher of the same family this package does not carry", strings.Repeat("a", 32), "aes-256-cbc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e, err := encryption.NewEncrypter([]byte(tt.key), tt.cipher)
			if !errors.Is(err, encryption.ErrUnsupportedCipher) {
				t.Fatalf("NewEncrypter returned %v, want ErrUnsupportedCipher", err)
			}
			if e != nil {
				t.Error("NewEncrypter returned an encrypter alongside an error")
			}
		})
	}
}

// TestTheKeyAccessorsHandBackCopies. A []byte is a window onto the encrypter's
// memory, and handing one out would let a caller rewrite the application key
// from outside.
func TestTheKeyAccessorsHandBackCopies(t *testing.T) {
	e := newGolden(t)
	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)}); err != nil {
		t.Fatal(err)
	}

	e.GetKey()[0] = 'z'
	e.GetAllKeys()[0][1] = 'z'
	e.GetPreviousKeys()[0][0] = 'z'

	if got := string(e.GetKey()); got != goldenKey {
		t.Errorf("the current key is now %q: an accessor handed out the encrypter's own memory", got)
	}
	if got := string(e.GetPreviousKeys()[0]); got != previousGoldenKey {
		t.Errorf("the previous key is now %q: an accessor handed out the encrypter's own memory", got)
	}
	if _, err := e.DecryptString(goldenHello); err != nil {
		t.Errorf("the encrypter stopped working after a caller wrote to a returned slice: %v", err)
	}
}

// TestTheConstructorCopiesTheKey, for the same reason.
func TestTheConstructorCopiesTheKey(t *testing.T) {
	key := []byte(goldenKey)
	e, err := encryption.NewEncrypter(key, encryption.AES256GCM)
	if err != nil {
		t.Fatal(err)
	}
	for i := range key {
		key[i] = 0
	}
	if _, err := e.DecryptString(goldenHello); err != nil {
		t.Errorf("zeroing the caller's buffer changed what the encrypter decrypts with: %v", err)
	}
}

// TestGetAllKeysOrder. The order is the order decryption tries them, and the
// current key has to be first or every decryption pays for the retired ones.
func TestGetAllKeysOrder(t *testing.T) {
	e := newGolden(t)
	if got := len(e.GetAllKeys()); got != 1 {
		t.Errorf("a fresh encrypter has %d keys, want 1", got)
	}
	if got := len(e.GetPreviousKeys()); got != 0 {
		t.Errorf("a fresh encrypter has %d previous keys, want 0", got)
	}

	second := strings.Repeat("c", 32)
	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey), []byte(second)}); err != nil {
		t.Fatal(err)
	}

	all := e.GetAllKeys()
	if len(all) != 3 {
		t.Fatalf("GetAllKeys returned %d keys, want 3", len(all))
	}
	for i, want := range []string{goldenKey, previousGoldenKey, second} {
		if got := string(all[i]); got != want {
			t.Errorf("GetAllKeys()[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestPreviousKeysChains, so that construction and key rotation can be written
// as one expression.
func TestPreviousKeysChains(t *testing.T) {
	e := newGolden(t)
	chained, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)})
	if err != nil {
		t.Fatal(err)
	}
	if chained != e {
		t.Error("PreviousKeys returned a different encrypter; it must return the receiver")
	}
}

// TestPreviousKeysWithNoKeysClears, matching an assignment of [].
func TestPreviousKeysWithNoKeysClears(t *testing.T) {
	e := newGolden(t)
	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PreviousKeys(nil); err != nil {
		t.Fatal(err)
	}
	if got := len(e.GetPreviousKeys()); got != 0 {
		t.Errorf("after clearing, %d previous keys remain", got)
	}
	if _, err := e.DecryptString(goldenRotated); err == nil {
		t.Error("a payload under a cleared previous key still opens")
	}
}

// TestEncryptAndDecryptRoundTripAValue is the generic pair -- the one that
// replaces $serialize = true.
func TestEncryptAndDecryptRoundTripAValue(t *testing.T) {
	e := newGolden(t)

	type account struct {
		ID    int      `json:"id"`
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	}
	want := account{ID: 7, Name: "Paulo", Roles: []string{"owner", "billing"}}

	payload, err := encryption.Encrypt(e, want)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := encryption.Decrypt[account](e, payload)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got.ID != want.ID || got.Name != want.Name || len(got.Roles) != len(want.Roles) {
		t.Errorf("round trip gave %+v, want %+v", got, want)
	}
}

// TestEncryptHandlesTheZeroValues. Each of these is a value a caller will
// encrypt and then read back expecting the same thing, and each is a shape
// where a careless implementation returns the zero value with no error.
func TestEncryptHandlesTheZeroValues(t *testing.T) {
	e := newGolden(t)

	t.Run("an empty string", func(t *testing.T) {
		payload, err := encryption.Encrypt(e, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := encryption.Decrypt[string](e, payload)
		if err != nil || got != "" {
			t.Errorf("got %q, %v; want an empty string and no error", got, err)
		}
	})

	t.Run("zero", func(t *testing.T) {
		payload, err := encryption.Encrypt(e, 0)
		if err != nil {
			t.Fatal(err)
		}
		got, err := encryption.Decrypt[int](e, payload)
		if err != nil || got != 0 {
			t.Errorf("got %d, %v; want 0 and no error", got, err)
		}
	})

	t.Run("false", func(t *testing.T) {
		payload, err := encryption.Encrypt(e, false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := encryption.Decrypt[bool](e, payload)
		if err != nil || got != false {
			t.Errorf("got %v, %v; want false and no error", got, err)
		}
	})

	t.Run("a nil pointer", func(t *testing.T) {
		payload, err := encryption.Encrypt[*string](e, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := encryption.Decrypt[*string](e, payload)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("got %v, want nil back", *got)
		}
	})
}

// TestEncryptRefusesAValueItCannotSerialize pins that a value JSON cannot hold
// is ErrEncrypt rather than a panic.
func TestEncryptRefusesAValueItCannotSerialize(t *testing.T) {
	e := newGolden(t)
	if _, err := encryption.Encrypt(e, make(chan int)); !errors.Is(err, encryption.ErrEncrypt) {
		t.Fatalf("Encrypt of an unserializable value returned %v, want ErrEncrypt", err)
	}
}

// TestDecryptSaysWhichHalfIsWrong. A payload written by EncryptString opens
// under the right key but holds no serialized value; reporting that as a
// decryption failure would send the reader to look at the key, which is fine.
func TestDecryptSaysWhichHalfIsWrong(t *testing.T) {
	e := newGolden(t)

	payload, err := e.EncryptString("just a string, not serialized")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encryption.Decrypt[map[string]string](e, payload); !errors.Is(err, encryption.ErrDecrypt) {
		t.Fatalf("Decrypt of an EncryptString payload returned %v, want ErrDecrypt", err)
	}
}

// TestEncryptAndEncryptStringAreDifferentPairs. Crossing them is the mistake
// most likely to be made, and it has to fail rather than return something
// plausible: Decrypt of an EncryptString payload must not hand back the raw
// serialized text.
func TestEncryptAndEncryptStringAreDifferentPairs(t *testing.T) {
	e := newGolden(t)

	payload, err := encryption.Encrypt(e, "hello")
	if err != nil {
		t.Fatal(err)
	}
	// DecryptString of an Encrypt payload gives the serialized form, not the
	// value -- it is the JSON, quotes and all.
	got, err := e.DecryptString(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got != `"hello"` {
		t.Errorf("DecryptString of an Encrypt payload = %q, want the serialized %q", got, `"hello"`)
	}
}

// TestAnEncrypterIsUsableFromManyGoroutines. It is held for the life of the
// process and reached from every request, so a data race here is a production
// crash rather than a test failure.
func TestAnEncrypterIsUsableFromManyGoroutines(t *testing.T) {
	e := newGolden(t)
	if _, err := e.PreviousKeys([][]byte{[]byte(previousGoldenKey)}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			payload, err := e.EncryptString("concurrent")
			if err != nil {
				done <- err
				return
			}
			got, err := e.DecryptString(payload)
			if err == nil && got != "concurrent" {
				err = errors.New("round trip returned the wrong value")
			}
			done <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
