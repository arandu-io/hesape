package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Cipher names an encryption algorithm. It answers to the keys of
// Encrypter::$supportedCiphers in Illuminate\Encryption\Encrypter.
//
// Illuminate lists four -- aes-128-cbc, aes-256-cbc, aes-128-gcm, aes-256-gcm.
// This package supports one, and the reason is RULE 9: a second cipher is a
// second way to encrypt, and the two are not equally safe. The CBC pair is not
// authenticated, which is why Illuminate has to carry an HMAC alongside the
// ciphertext and check it by hand; getting that wrong is silent, and the
// mistake is only visible to whoever is forging payloads. AES-256-GCM
// authenticates as part of decrypting, so there is no second thing to check
// and no way to forget to check it.
//
// The value is compared case-insensitively, because every read of it in
// Illuminate is wrapped in strtolower.
type Cipher string

// AES256GCM is the cipher this package encrypts with. It answers to the
// 'aes-256-gcm' entry of Encrypter::$supportedCiphers.
const AES256GCM Cipher = "aes-256-gcm"

// ivLength is the nonce size of AES-GCM in bytes, which is what
// openssl_cipher_iv_length('aes-256-gcm') returns and what crypto/cipher
// produces from NewGCM.
const ivLength = 12

// tagLength is the AEAD tag size in bytes. Encrypter::ensureTagIsValid checks
// for exactly this, so a payload carrying a shorter or longer tag is refused
// before any key is tried.
const tagLength = 16

// size reports the key length the cipher requires, and whether the cipher is
// one this package has. It answers to the 'size' entry of
// Encrypter::$supportedCiphers.
func (c Cipher) size() (int, bool) {
	switch Cipher(strings.ToLower(string(c))) {
	case AES256GCM:
		return 32, true
	}
	return 0, false
}

// GenerateKey returns a new random key of the length the cipher requires.
//
// It answers to the static Encrypter::generateKey($cipher). The cipher is the
// receiver rather than an argument because Go has no overloading and the
// package already exports a GenerateKey that answers to a different PHP method
// -- KeyGenerateCommand::generateRandomKey, which returns the printable
// "base64:" form a .env file holds. Both names are the ones their PHP has; only
// the placement moved.
//
// An unrecognised cipher yields 32 bytes rather than an error, which is the
// `?? 32` fallback the PHP writes. There is no error to return: crypto/rand
// fills the slice or the process dies trying.
func (c Cipher) GenerateKey() []byte {
	size, ok := c.size()
	if !ok {
		size = 32
	}
	key := make([]byte, size)
	// Errors are documented as impossible since Go 1.24: Read either fills the
	// slice entirely or panics.
	_, _ = rand.Read(key)
	return key
}

// Supported reports whether the key and cipher combination is usable. It
// answers to the static Encrypter::supported($key, $cipher).
//
// Both halves matter: an unknown cipher fails, and so does a key of the wrong
// length for a known one. Illuminate measures the key in bytes
// (mb_strlen($key, '8bit')), which is what len on a []byte already is.
func Supported(key []byte, cipher Cipher) bool {
	size, ok := cipher.size()
	if !ok {
		return false
	}
	return len(key) == size
}

// ErrUnsupportedCipher is the RuntimeException that Encrypter::__construct and
// Encrypter::previousKeys throw when the key length does not match the cipher.
var ErrUnsupportedCipher = errors.New("encryption: unsupported cipher or incorrect key length; the supported cipher is: aes-256-gcm")

// ErrEncrypt answers to Illuminate\Contracts\Encryption\EncryptException.
var ErrEncrypt = errors.New("encryption: could not encrypt the data")

// ErrDecrypt answers to Illuminate\Contracts\Encryption\DecryptException.
//
// Every failure below wraps it, so a caller answers "this value is not ours"
// once rather than switching on four reasons it is not. The reasons are still
// in the message, because the one reading it is a developer holding a payload,
// not a request.
var ErrDecrypt = errors.New("encryption: could not decrypt the data")

// errInvalidPayload is DecryptException('The payload is invalid.') -- the value
// is not a payload this encrypter wrote, and no key was tried.
var errInvalidPayload = fmt.Errorf("%w: the payload is invalid", ErrDecrypt)

// Encrypter encrypts and decrypts values with the application key. It answers
// to Illuminate\Encryption\Encrypter, and satisfies both interfaces that class
// declares: Contracts\Encryption\Encrypter and Contracts\Encryption\StringEncrypter.
//
// The payload it writes is the one Illuminate writes: base64 of a JSON object
// with iv, value, mac and tag, in that order, each of them base64 itself. The
// mac is empty, because with an AEAD cipher the tag is the MAC -- Illuminate
// leaves the field in place and so does this, since a reader that has both
// formats in a column needs the field to be there to recognise them.
//
// It is safe for concurrent use. Nothing mutates after construction except
// PreviousKeys, which is a boot-time call.
type Encrypter struct {
	key          []byte
	previousKeys [][]byte
	cipher       Cipher
}

// NewEncrypter returns an Encrypter over the given key. It answers to
// Encrypter::__construct($key, $cipher).
//
// The cipher has no default here, where the PHP defaults to 'aes-128-cbc'. Go
// has no default arguments, and the PHP default is a cipher this package does
// not carry -- see [Cipher]. Passing [AES256GCM] is the only call that works,
// and requiring it keeps the payload format a thing the caller chose rather
// than a thing it inherited.
//
// The key is copied, so a caller that zeroes or reuses its buffer does not
// silently change what the application encrypts with.
func NewEncrypter(key []byte, cipher Cipher) (*Encrypter, error) {
	if !Supported(key, cipher) {
		return nil, fmt.Errorf("%w: got a %d-byte key for cipher %q", ErrUnsupportedCipher, len(key), cipher)
	}
	return &Encrypter{
		key:    append([]byte(nil), key...),
		cipher: cipher,
	}, nil
}

// payloadOut is the JSON object an encrypted value is. The field order is the
// order of compact('iv', 'value', 'mac', 'tag') in Encrypter::encrypt, and Go
// marshals struct fields in declaration order, so the bytes match Illuminate's
// byte for byte.
type payloadOut struct {
	IV    string `json:"iv"`
	Value string `json:"value"`
	MAC   string `json:"mac"`
	Tag   string `json:"tag"`
}

// payloadIn is the same object being read back. The fields are pointers so that
// a missing key and an empty string are distinguishable, which is the
// distinction Encrypter::validPayload draws with isset: mac is legitimately ""
// under an AEAD cipher, but a payload with no mac field at all is not one of
// ours. A field holding a non-string makes Unmarshal fail, which is the same
// answer the is_string check gives.
type payloadIn struct {
	IV    *string `json:"iv"`
	Value *string `json:"value"`
	MAC   *string `json:"mac"`
	Tag   *string `json:"tag"`
}

// Encrypt encrypts a value of any type. It answers to Encrypter::encrypt($value,
// $serialize = true) -- the branch where $serialize is true.
//
// It is a function and not a method because it is generic and Go methods cannot
// be. The type parameter is what replaces PHP's `mixed`: the PHP hands back
// whatever it unserialized and the caller has to know what that was, while here
// the caller says so and the compiler holds it to it.
//
// Serialization is encoding/json where Illuminate uses PHP's serialize(). The
// two are not wire-compatible, and could not be: serialize() writes PHP class
// names and property visibility, none of which exists here. A value written by
// [Encrypt] is read back by [Decrypt]; a value written by PHP's encrypt() is not.
// [EncryptString] is the pair that is wire-compatible, in both directions.
func Encrypt[T any](e *Encrypter, value T) (string, error) {
	serialized, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrEncrypt, err)
	}
	return e.encrypt(serialized)
}

// EncryptString encrypts a string without serializing it. It answers to
// Encrypter::encryptString($value), which is encrypt($value, false).
//
// This is the payload an Illuminate application reading the same key can
// decrypt with decryptString, and the one this package can decrypt from theirs.
// An empty string is a legal input and produces a real payload: GCM over no
// plaintext is a nonce and a tag, and [DecryptString] returns "" from it.
func (e *Encrypter) EncryptString(value string) (string, error) {
	return e.encrypt([]byte(value))
}

// encrypt is the body both public forms share: everything in Encrypter::encrypt
// after the serialize decision.
func (e *Encrypter) encrypt(plaintext []byte) (string, error) {
	aead, err := e.aead(e.key)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrEncrypt, err)
	}

	iv := make([]byte, aead.NonceSize())
	_, _ = rand.Read(iv)

	// Seal returns ciphertext and tag as one slice; Illuminate carries them in
	// two fields, because openssl_encrypt hands them back separately.
	sealed := aead.Seal(nil, iv, plaintext, nil)
	split := len(sealed) - aead.Overhead()

	body, err := json.Marshal(payloadOut{
		IV:    base64.StdEncoding.EncodeToString(iv),
		Value: base64.StdEncoding.EncodeToString(sealed[:split]),
		// Empty for an AEAD cipher: the tag is the MAC. Encrypter::encrypt
		// writes '' here for the same reason.
		MAC: "",
		Tag: base64.StdEncoding.EncodeToString(sealed[split:]),
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrEncrypt, err)
	}
	return base64.StdEncoding.EncodeToString(body), nil
}

// Decrypt decrypts a value written by [Encrypt] back into T. It answers to
// Encrypter::decrypt($payload, $unserialize = true) -- the branch where
// $unserialize is true.
//
// It is a function and not a method for the same reason [Encrypt] is.
func Decrypt[T any](e *Encrypter, payload string) (T, error) {
	var zero T
	plaintext, err := e.decrypt(payload)
	if err != nil {
		return zero, err
	}
	var value T
	if err := json.Unmarshal(plaintext, &value); err != nil {
		// The key was right -- the payload opened. What is inside is not what
		// this call expected, which is a different mistake from a bad key and
		// says so, because the two are fixed in different places.
		return zero, fmt.Errorf("%w: the payload decrypted but does not hold this type; was it written by EncryptString?", ErrDecrypt)
	}
	return value, nil
}

// DecryptString decrypts a string without unserializing it. It answers to
// Encrypter::decryptString($payload), which is decrypt($payload, false).
func (e *Encrypter) DecryptString(payload string) (string, error) {
	plaintext, err := e.decrypt(payload)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// decrypt is the body both public forms share: everything in Encrypter::decrypt
// before the unserialize decision.
func (e *Encrypter) decrypt(payload string) ([]byte, error) {
	p, err := e.getJSONPayload(payload)
	if err != nil {
		return nil, err
	}

	// Already validated by validPayload, which is why the error is dropped.
	iv, _ := base64.StdEncoding.DecodeString(*p.IV)

	var tag []byte
	if p.Tag != nil && *p.Tag != "" {
		if tag, err = base64.StdEncoding.DecodeString(*p.Tag); err != nil {
			return nil, errInvalidPayload
		}
	}
	if err := e.ensureTagIsValid(tag); err != nil {
		return nil, err
	}

	value, err := base64.StdEncoding.DecodeString(*p.Value)
	if err != nil {
		return nil, ErrDecrypt
	}
	sealed := append(value, tag...)

	// Every key, current first, then the retired ones. This is the whole of key
	// rotation: a deploy sets the new key and moves the old one to
	// PreviousKeys, and everything already encrypted keeps opening. Nothing is
	// ever encrypted with a previous key.
	for _, key := range e.GetAllKeys() {
		aead, err := e.aead(key)
		if err != nil || len(iv) != aead.NonceSize() {
			continue
		}
		// Open verifies the tag before returning any plaintext, so a forged
		// payload fails here rather than downstream. This is the check
		// Encrypter::validMacForKey has to do by hand for the CBC ciphers.
		if plaintext, err := aead.Open(nil, iv, sealed, nil); err == nil {
			return plaintext, nil
		}
	}
	return nil, ErrDecrypt
}

// aead builds the AEAD for a key. Split out because decrypt builds one per key
// it tries.
func (e *Encrypter) aead(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// getJSONPayload reads the payload and refuses anything that is not one. It
// answers to the protected Encrypter::getJsonPayload.
func (e *Encrypter) getJSONPayload(payload string) (*payloadIn, error) {
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, errInvalidPayload
	}
	var p payloadIn
	if err := json.Unmarshal(decoded, &p); err != nil {
		return nil, errInvalidPayload
	}
	if !e.validPayload(&p) {
		return nil, errInvalidPayload
	}
	return &p, nil
}

// validPayload reports whether the decoded object has the shape of a payload.
// It answers to the protected Encrypter::validPayload.
//
// The iv length check is the load-bearing one, and it is why this runs before
// any key is tried: it separates "not one of ours" from "ours, and it failed",
// and only the second is worth reporting as a decryption failure.
func (e *Encrypter) validPayload(p *payloadIn) bool {
	if p.IV == nil || p.Value == nil || p.MAC == nil {
		return false
	}
	iv, err := base64.StdEncoding.DecodeString(*p.IV)
	if err != nil {
		return false
	}
	return len(iv) == ivLength
}

// ensureTagIsValid refuses a tag of the wrong length. It answers to the
// protected Encrypter::ensureTagIsValid.
//
// The PHP has a second branch, for a payload that carries a tag under a cipher
// that cannot use one. No cipher here is in that state -- [AES256GCM] is the
// only one, and it is AEAD -- so the branch has nothing that could reach it.
func (e *Encrypter) ensureTagIsValid(tag []byte) error {
	if len(tag) != tagLength {
		return ErrDecrypt
	}
	return nil
}

// AppearsEncrypted reports whether a value looks like something this encrypter
// wrote. It answers to the static Encrypter::appearsEncrypted($value).
//
// It is a guess and says so by returning a bool rather than an error: it reads
// the envelope, never a key. It is what a migration re-encrypting a column uses
// to skip the rows it already did. A true here does not promise [DecryptString]
// will succeed -- only that it is worth calling.
func AppearsEncrypted(value string) bool {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return false
	}
	var p payloadIn
	if err := json.Unmarshal(decoded, &p); err != nil {
		return false
	}
	return p.IV != nil && p.Value != nil && p.MAC != nil
}

// GetKey returns the key the encrypter encrypts with. It answers to
// Encrypter::getKey().
//
// The copy is not politeness. PHP hands back a string, which is a value; a
// []byte is a window onto the encrypter's own memory, and a caller that
// appended to it would be rewriting the application key from outside.
func (e *Encrypter) GetKey() []byte {
	return append([]byte(nil), e.key...)
}

// GetAllKeys returns the current key followed by every previous key. It answers
// to Encrypter::getAllKeys().
//
// The order is the order decryption tries them, and the current key is first
// because it is the one that will work.
func (e *Encrypter) GetAllKeys() [][]byte {
	keys := make([][]byte, 0, 1+len(e.previousKeys))
	keys = append(keys, e.GetKey())
	return append(keys, e.GetPreviousKeys()...)
}

// GetPreviousKeys returns the retired keys, without the current one. It answers
// to Encrypter::getPreviousKeys().
func (e *Encrypter) GetPreviousKeys() [][]byte {
	keys := make([][]byte, 0, len(e.previousKeys))
	for _, key := range e.previousKeys {
		keys = append(keys, append([]byte(nil), key...))
	}
	return keys
}

// PreviousKeys sets the retired keys that decryption falls back to, and returns
// the encrypter so the call chains. It answers to
// Encrypter::previousKeys(array $keys).
//
// Every key is checked against the cipher first, and none is stored if any
// fails -- the PHP throws mid-loop, which leaves the encrypter untouched, and
// so does this. A key that is the wrong length would not be used at
// decryption time anyway; the point of refusing it here is that a typo in
// APP_PREVIOUS_KEYS should stop a deploy, not quietly stop old payloads from
// opening.
func (e *Encrypter) PreviousKeys(keys [][]byte) (*Encrypter, error) {
	for i, key := range keys {
		if !Supported(key, e.cipher) {
			return nil, fmt.Errorf("%w: previous key %d is %d bytes", ErrUnsupportedCipher, i+1, len(key))
		}
	}
	previous := make([][]byte, 0, len(keys))
	for _, key := range keys {
		previous = append(previous, append([]byte(nil), key...))
	}
	e.previousKeys = previous
	return e, nil
}
