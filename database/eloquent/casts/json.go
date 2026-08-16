package casts

import (
	"encoding/json"
	"sync"
)

// Json is the one place a cast encodes and decodes a JSON column, and the seam
// an application uses to replace that with its own encoder.
//
// The encoders are package variables behind a mutex and the operations are
// package functions, because none of them is per cast. The type is kept so the
// name is where a reader looks for it.
type Json struct{}

var jsonCoder = struct {
	sync.RWMutex
	encode func(value any) ([]byte, error)
	decode func(data []byte, out *any) error
}{}

// Encode returns value marshaled to JSON, through the encoder EncodeUsing
// last set, or through encoding/json if none was set.
//
// Encode takes no formatting flags: encoding/json has nothing worth passing
// through, and pretty printing a stored column would change what is stored.
func Encode(value any) ([]byte, error) {
	jsonCoder.RLock()
	encode := jsonCoder.encode
	jsonCoder.RUnlock()

	if encode != nil {
		return encode(value)
	}
	return json.Marshal(value)
}

// Decode returns data decoded from JSON, through the decoder DecodeUsing
// last set, or through encoding/json if none was set. A JSON object decodes
// to map[string]any and a JSON array to []any.
//
// An empty input decodes to nil rather than an error, because a null column
// and an empty JSON column mean the same thing to a cast.
func Decode(data []byte) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}

	jsonCoder.RLock()
	decode := jsonCoder.decode
	jsonCoder.RUnlock()

	var out any
	if decode != nil {
		if err := decode(data, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EncodeUsing replaces the encoder Encode calls; a nil encoder restores
// encoding/json.
func EncodeUsing(encoder func(value any) ([]byte, error)) {
	jsonCoder.Lock()
	defer jsonCoder.Unlock()
	jsonCoder.encode = encoder
}

// DecodeUsing replaces the decoder Decode calls; a nil decoder restores
// encoding/json.
func DecodeUsing(decoder func(data []byte, out *any) error) {
	jsonCoder.Lock()
	defer jsonCoder.Unlock()
	jsonCoder.decode = decoder
}

// encodeToString is what every cast that writes a JSON column uses: the column
// is text, so the bytes become a string exactly once and in one place.
func encodeToString(value any) (string, error) {
	out, err := Encode(value)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// decodeAttribute reads a stored value as JSON. A column comes back as a
// string from one driver and as []byte from another, and a cast should not have
// to know which.
func decodeAttribute(value any) (any, error) {
	switch stored := value.(type) {
	case nil:
		return nil, nil
	case string:
		return Decode([]byte(stored))
	case []byte:
		return Decode(stored)
	default:
		return stored, nil
	}
}
