package casts

import (
	"fmt"

	"github.com/arandu-io/hesape/collections"
	"github.com/arandu-io/hesape/encryption"
)

// AsEncryptedCollection answers
// Illuminate\Database\Eloquent\Casts\AsEncryptedCollection: a JSON array
// column encrypted at rest.
//
// The PHP reaches the encrypter through the Crypt facade. There is no facade
// and no container here (ADR 0001, ADR 0002), so the encrypter is a field: the
// application that built it hands it to the cast, which is the same wiring
// written where a reader can see it.
type AsEncryptedCollection struct {
	// Encrypter is what Crypt::encryptString and Crypt::decryptString reach in
	// the PHP.
	Encrypter *encryption.Encrypter

	// Mapper answers the second cast argument, as it does on AsCollection.
	Mapper func(item any) (any, error)
}

// Of answers AsEncryptedCollection::of.
func (a AsEncryptedCollection) Of(mapper func(item any) (any, error)) AsEncryptedCollection {
	return AsEncryptedCollection{Encrypter: a.Encrypter, Mapper: mapper}
}

// CastUsing answers AsEncryptedCollection::castUsing.
func (a AsEncryptedCollection) CastUsing(arguments []string) (CastsAttributes, error) {
	if a.Encrypter == nil {
		return nil, fmt.Errorf("casts: AsEncryptedCollection needs an encrypter")
	}
	return encryptedCollectionCast{encrypter: a.Encrypter, mapper: a.Mapper}, nil
}

type encryptedCollectionCast struct {
	encrypter *encryption.Encrypter
	mapper    func(item any) (any, error)
}

// Get answers the anonymous caster's get: decrypt, then decode, then map.
func (c encryptedCollectionCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	stored, ok := attributes[key]
	if !ok || stored == nil {
		return nil, nil
	}
	payload, ok := asText(stored)
	if !ok {
		return nil, nil
	}

	plain, err := c.encrypter.DecryptString(payload)
	if err != nil {
		return nil, fmt.Errorf("casts: decrypting %s: %w", key, err)
	}

	data, err := Decode([]byte(plain))
	if err != nil {
		return nil, fmt.Errorf("casts: decoding %s: %w", key, err)
	}
	items, ok := data.([]any)
	if !ok {
		return nil, nil
	}
	if c.mapper == nil {
		return collections.Collect(items), nil
	}

	mapped := make([]any, 0, len(items))
	for _, item := range items {
		out, err := c.mapper(item)
		if err != nil {
			return nil, fmt.Errorf("casts: mapping %s: %w", key, err)
		}
		mapped = append(mapped, out)
	}
	return collections.Collect(mapped), nil
}

// Set answers the anonymous caster's set: encode, then encrypt. A nil value
// writes nothing, which is the PHP's `return null`.
func (c encryptedCollectionCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	payload, err := c.encrypter.EncryptString(encoded)
	if err != nil {
		return nil, fmt.Errorf("casts: encrypting %s: %w", key, err)
	}
	return map[string]any{key: payload}, nil
}

// AsEncryptedArrayObject casts a text column holding an encrypted JSON object
// into an *ArrayObject, and encrypts it again on the way back to the database.
//
// It is AsEncryptedCollection for an object rather than an array: reading
// decrypts the column and decodes it into a keyed bag, writing encodes the bag
// and encrypts it, and a value the column cannot be read as an object reads as
// nil. Serializing the model gives the plain map, not the ciphertext.
//
// The column is opaque to the database, so nothing can be queried, indexed or
// sorted by what is inside it. That is the trade the cast exists to make.
//
// Answers Illuminate\Database\Eloquent\Casts\AsEncryptedArrayObject.
type AsEncryptedArrayObject struct {
	// Encrypter is what the Crypt facade is in the PHP.
	Encrypter *encryption.Encrypter
}

// CastUsing answers AsEncryptedArrayObject::castUsing.
func (a AsEncryptedArrayObject) CastUsing(arguments []string) (CastsAttributes, error) {
	if a.Encrypter == nil {
		return nil, fmt.Errorf("casts: AsEncryptedArrayObject needs an encrypter")
	}
	return encryptedArrayObjectCast{encrypter: a.Encrypter}, nil
}

type encryptedArrayObjectCast struct {
	encrypter *encryption.Encrypter
}

// Get answers the anonymous caster's get.
func (c encryptedArrayObjectCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	stored, ok := attributes[key]
	if !ok || stored == nil {
		return nil, nil
	}
	payload, ok := asText(stored)
	if !ok {
		return nil, nil
	}

	plain, err := c.encrypter.DecryptString(payload)
	if err != nil {
		return nil, fmt.Errorf("casts: decrypting %s: %w", key, err)
	}

	data, err := Decode([]byte(plain))
	if err != nil {
		return nil, fmt.Errorf("casts: decoding %s: %w", key, err)
	}
	bag, ok := data.(map[string]any)
	if !ok {
		return nil, nil
	}
	return NewArrayObject(bag), nil
}

// Set answers the anonymous caster's set.
func (c encryptedArrayObjectCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := encodeToString(value)
	if err != nil {
		return nil, fmt.Errorf("casts: encoding %s: %w", key, err)
	}
	payload, err := c.encrypter.EncryptString(encoded)
	if err != nil {
		return nil, fmt.Errorf("casts: encrypting %s: %w", key, err)
	}
	return map[string]any{key: payload}, nil
}

// Serialize answers the anonymous caster's serialize: the plain map, as
// AsArrayObject's does.
func (encryptedArrayObjectCast) Serialize(model any, key string, value any, attributes map[string]any) (any, error) {
	if bag, ok := value.(*ArrayObject[any]); ok {
		return bag.GetArrayCopy(), nil
	}
	return value, nil
}
