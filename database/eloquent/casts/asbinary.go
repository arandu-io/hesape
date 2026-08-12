package casts

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// AsBinary answers Illuminate\Database\Eloquent\Casts\AsBinary: an identifier
// stored as its sixteen raw bytes rather than as text.
//
// It is worth the trouble on a key column: a UUID as char(36) is 36 bytes and
// compares character by character, and the same value as binary(16) is 16 and
// compares as two words.
//
// The PHP delegates the two directions to Illuminate\Support\BinaryCodec. There
// is no such helper here, and one that only this cast used would be a package
// with a single caller, so the two formats are decoded below.
type AsBinary struct {
	// Format answers the single cast argument: "uuid" or "ulid".
	Format string
}

// Formats answers BinaryCodec::formats: the formats the cast accepts.
func (AsBinary) Formats() []string { return []string{"uuid", "ulid"} }

// UUID answers AsBinary::uuid. The PHP spells the static uuid.
func (a AsBinary) UUID() AsBinary { return AsBinary{Format: "uuid"} }

// ULID answers AsBinary::ulid. The PHP spells the static ulid.
func (a AsBinary) ULID() AsBinary { return AsBinary{Format: "ulid"} }

// Of answers AsBinary::of: the cast configured for one format.
func (a AsBinary) Of(format string) AsBinary { return AsBinary{Format: format} }

// CastUsing answers AsBinary::castUsing. The PHP throws
// InvalidArgumentException for a missing or unknown format; this returns it.
func (a AsBinary) CastUsing(arguments []string) (CastsAttributes, error) {
	format := a.Format
	if format == "" && len(arguments) > 0 {
		format = arguments[0]
	}
	if format == "" {
		return nil, fmt.Errorf("casts: the binary codec format is required")
	}
	if format != "uuid" && format != "ulid" {
		return nil, fmt.Errorf(
			"casts: unsupported binary codec format [%s]. allowed formats are: %s",
			format, strings.Join(AsBinary{}.Formats(), ", "),
		)
	}
	return binaryCast{format: format}, nil
}

type binaryCast struct {
	format string
}

// Get answers the anonymous caster's get: the raw bytes back as text.
func (b binaryCast) Get(model any, key string, value any, attributes map[string]any) (any, error) {
	stored, ok := attributes[key]
	if !ok || stored == nil {
		return nil, nil
	}

	raw, ok := stored.([]byte)
	if !ok {
		text, isText := asText(stored)
		if !isText {
			return nil, nil
		}
		raw = []byte(text)
	}
	if len(raw) != 16 {
		return nil, fmt.Errorf("casts: %s: a binary %s is 16 bytes, got %d", key, b.format, len(raw))
	}

	if b.format == "uuid" {
		return encodeUUID(raw), nil
	}
	return encodeULID(raw), nil
}

// Set answers the anonymous caster's set: the text as its raw bytes.
func (b binaryCast) Set(model any, key string, value any, attributes map[string]any) (map[string]any, error) {
	text, ok := asText(value)
	if !ok {
		return map[string]any{key: nil}, nil
	}

	if b.format == "uuid" {
		raw, err := decodeUUID(text)
		if err != nil {
			return nil, fmt.Errorf("casts: %s: %w", key, err)
		}
		return map[string]any{key: raw}, nil
	}

	raw, err := decodeULID(text)
	if err != nil {
		return nil, fmt.Errorf("casts: %s: %w", key, err)
	}
	return map[string]any{key: raw}, nil
}

// encodeUUID writes sixteen bytes in the 8-4-4-4-12 hyphenated form.
func encodeUUID(raw []byte) string {
	var out [36]byte
	hex.Encode(out[0:8], raw[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], raw[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], raw[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], raw[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], raw[10:16])
	return string(out[:])
}

// decodeUUID reads the hyphenated form back into sixteen bytes.
func decodeUUID(text string) ([]byte, error) {
	stripped := strings.ReplaceAll(text, "-", "")
	if len(stripped) != 32 {
		return nil, fmt.Errorf("%q is not a uuid", text)
	}
	raw, err := hex.DecodeString(stripped)
	if err != nil {
		return nil, fmt.Errorf("%q is not a uuid", text)
	}
	return raw, nil
}

// crockford is base32 without I, L, O and U -- the four that are read as
// something else. It is str's alphabet, repeated rather than exported from
// there, because a ULID's spelling is not str's contract.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// encodeULID writes sixteen bytes as the 26 Crockford base32 characters a ULID
// is. 128 bits do not divide into 5, so the first character carries the two
// leading bits and the remaining 25 carry 125.
func encodeULID(raw []byte) string {
	out := make([]byte, 26)
	acc, bits, pos := uint64(0), 0, 0

	// The first character is the top two bits, left padded to five.
	out[pos] = crockford[raw[0]>>5]
	pos++
	acc, bits = uint64(raw[0]&0x1f), 5

	for _, by := range raw[1:] {
		acc = acc<<8 | uint64(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[pos] = crockford[(acc>>bits)&31]
			pos++
		}
	}
	return string(out)
}

// decodeULID reads the 26 characters back into sixteen bytes.
func decodeULID(text string) ([]byte, error) {
	if len(text) != 26 {
		return nil, fmt.Errorf("%q is not a ulid", text)
	}

	raw := make([]byte, 0, 16)
	acc, bits := uint64(0), 0
	for i, char := range strings.ToUpper(text) {
		index := strings.IndexRune(crockford, char)
		if index < 0 {
			return nil, fmt.Errorf("%q is not a ulid", text)
		}
		if i == 0 {
			if index > 7 {
				return nil, fmt.Errorf("%q is not a ulid", text)
			}
			acc, bits = uint64(index), 3
			continue
		}
		acc = acc<<5 | uint64(index)
		bits += 5
		for bits >= 8 {
			bits -= 8
			raw = append(raw, byte((acc>>bits)&0xff))
		}
	}
	if len(raw) != 16 {
		return nil, fmt.Errorf("%q is not a ulid", text)
	}
	return raw, nil
}
