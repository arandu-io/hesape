package casts

import (
	"encoding/json"
	"sync"
)

// Json answers Illuminate\Database\Eloquent\Casts\Json: the one place a cast
// encodes and decodes a JSON column, and the seam an application uses to
// replace that with its own encoder.
//
// The PHP class holds two static properties and four static methods. Go has no
// static, so the properties are package variables behind a mutex and the
// methods are package functions -- which is what a static method is once the
// class is not a namespace (ADR 0044, mechanical change). The type is kept so
// the name Json still appears where a reader looks for it.
type Json struct{}

var jsonCoder = struct {
	sync.RWMutex
	encode func(value any) ([]byte, error)
	decode func(data []byte, out *any) error
}{}

// Encode answers Json::encode.
//
// The PHP takes json_encode's flags as a second argument and returns false on
// failure. Go returns bytes and an error, and the flags have no counterpart:
// encoding/json has no JSON_PRETTY_PRINT to pass through, and pretty printing a
// column would change what is stored.
func Encode(value any) ([]byte, error) {
	jsonCoder.RLock()
	encode := jsonCoder.encode
	jsonCoder.RUnlock()

	if encode != nil {
		return encode(value)
	}
	return json.Marshal(value)
}

// Decode answers Json::decode.
//
// The PHP's $associative argument chooses between an array and a stdClass. Go
// has neither: a JSON object decodes to map[string]any and an array to []any,
// which is the associative branch, and the object branch has nothing to decode
// into. An empty input decodes to nil rather than an error, because a null
// column and an empty JSON column mean the same thing to a cast.
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

// EncodeUsing answers Json::encodeUsing. A nil encoder restores encoding/json,
// which is the PHP's null.
func EncodeUsing(encoder func(value any) ([]byte, error)) {
	jsonCoder.Lock()
	defer jsonCoder.Unlock()
	jsonCoder.encode = encoder
}

// DecodeUsing answers Json::decodeUsing. A nil decoder restores encoding/json.
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
