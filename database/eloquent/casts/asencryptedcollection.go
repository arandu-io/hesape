package casts

import (
	"fmt"

	"github.com/arandu-io/hesape/collections"
	"github.com/arandu-io/hesape/encryption"
)

// AsEncryptedCollection casts a JSON array column encrypted at rest.
//
// The encrypter is a field: the application that built it hands it to the cast,
// where a reader can see the wiring.
type AsEncryptedCollection struct {
	// Encrypter performs the encryption and decryption the cast does on the
	// way in and out of the database.
	Encrypter *encryption.Encrypter

	// Mapper is the function each decoded item is passed through before it
	// joins the collection, the same role it plays on AsCollection.
	Mapper func(item any) (any, error)
}

// Of returns the cast configured with mapper, keeping the same Encrypter.
func (a AsEncryptedCollection) Of(mapper func(item any) (any, error)) AsEncryptedCollection {
	return AsEncryptedCollection{Encrypter: a.Encrypter, Mapper: mapper}
}

// CastUsing returns the caster configured with Encrypter and Mapper, or an
// error if Encrypter is nil.
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

// Get decrypts the stored payload, decodes it as JSON, and maps each item
// through mapper when one is set.
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

// Set encodes value as JSON, encrypts it, and returns the column holding the
// ciphertext. A nil value writes no column at all.
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
type AsEncryptedArrayObject struct {
	// Encrypter performs the encryption and decryption the cast does on the
	// way in and out of the database.
	Encrypter *encryption.Encrypter
}

// CastUsing returns the caster configured with Encrypter, or an error if
// Encrypter is nil.
func (a AsEncryptedArrayObject) CastUsing(arguments []string) (CastsAttributes, error) {
	if a.Encrypter == nil {
		return nil, fmt.Errorf("casts: AsEncryptedArrayObject needs an encrypter")
	}
	return encryptedArrayObjectCast{encrypter: a.Encrypter}, nil
}

type encryptedArrayObjectCast struct {
	encrypter *encryption.Encrypter
}

// Get decrypts the stored payload and returns it decoded as an *ArrayObject,
// or nil if the column is absent or does not decode to a JSON object.
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

// Set encodes value as JSON, encrypts it, and returns the column holding the
// ciphertext. A nil value writes no column at all.
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

// Serialize returns value's plain map form when value is an *ArrayObject, so
// the serialised row holds the map rather than the ArrayObject itself.
func (encryptedArrayObjectCast) Serialize(model any, key string, value any, attributes map[string]any) (any, error) {
	if bag, ok := value.(*ArrayObject[any]); ok {
		return bag.GetArrayCopy(), nil
	}
	return value, nil
}
