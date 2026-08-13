package casts_test

import (
	"strings"
	"testing"

	"github.com/arandu-io/hesape/collections"
	"github.com/arandu-io/hesape/database/eloquent/casts"
	"github.com/arandu-io/hesape/encryption"
)

func TestAsCollectionRoundTripsAJsonArrayColumn(t *testing.T) {
	cast, err := casts.AsCollection{}.CastUsing(nil)
	if err != nil {
		t.Fatalf("building the cast: %v", err)
	}

	written, err := cast.Set(nil, "tags", collections.Collect([]any{"go", "sql"}), map[string]any{})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	stored, ok := written["tags"].(string)
	if !ok || stored != `["go","sql"]` {
		t.Fatalf("the column holds %#v, want the JSON array", written["tags"])
	}

	read, err := cast.Get(nil, "tags", stored, map[string]any{"tags": stored})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	items, ok := read.(collections.Collection[any])
	if !ok {
		t.Fatalf("the column came back as %T, want a collection", read)
	}
	if items.Count() != 2 || items.All()[0] != "go" {
		t.Errorf("the column came back as %v", items.All())
	}
}

func TestAsCollectionOfRunsTheMapperOnTheWayOut(t *testing.T) {
	cast, err := casts.AsCollection{}.
		Of(func(item any) (any, error) { return strings.ToUpper(item.(string)), nil }).
		CastUsing(nil)
	if err != nil {
		t.Fatalf("building the cast: %v", err)
	}

	read, err := cast.Get(nil, "tags", nil, map[string]any{"tags": `["go"]`})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got := read.(collections.Collection[any]).All()[0]; got != "GO" {
		t.Errorf("the mapper gave %v, want GO", got)
	}
}

func TestAnAbsentColumnCastsToNothing(t *testing.T) {
	cast, err := casts.AsCollection{}.CastUsing(nil)
	if err != nil {
		t.Fatalf("building the cast: %v", err)
	}

	read, err := cast.Get(nil, "tags", nil, map[string]any{})
	if err != nil {
		t.Fatalf("reading an absent column: %v", err)
	}
	if read != nil {
		t.Errorf("an absent column came back as %#v, want nil", read)
	}
}

func TestAsArrayObjectSerializesAsThePlainMap(t *testing.T) {
	cast, err := casts.AsArrayObject{}.CastUsing(nil)
	if err != nil {
		t.Fatalf("building the cast: %v", err)
	}

	read, err := cast.Get(nil, "options", nil, map[string]any{"options": `{"email":true}`})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	bag, ok := read.(*casts.ArrayObject[any])
	if !ok {
		t.Fatalf("the column came back as %T, want an ArrayObject", read)
	}
	if value, found := bag.OffsetGet("email"); !found || value != true {
		t.Errorf("the ArrayObject holds %#v", bag.GetArrayCopy())
	}

	serializer, ok := cast.(casts.SerializesCastableAttributes)
	if !ok {
		t.Fatal("the ArrayObject cast does not serialize")
	}
	serialized, err := serializer.Serialize(nil, "options", bag, map[string]any{})
	if err != nil {
		t.Fatalf("serializing: %v", err)
	}
	if _, ok := serialized.(map[string]any); !ok {
		t.Errorf("the serialized column is %T, want the plain map", serialized)
	}
}

func TestAsBinaryRoundTripsAUuid(t *testing.T) {
	cast, err := casts.AsBinary{}.UUID().CastUsing(nil)
	if err != nil {
		t.Fatalf("building the cast: %v", err)
	}

	const id = "0196f0d5-2d4c-7000-8000-000000000001"
	written, err := cast.Set(nil, "id", id, map[string]any{})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	raw, ok := written["id"].([]byte)
	if !ok || len(raw) != 16 {
		t.Fatalf("the column holds %#v, want sixteen bytes", written["id"])
	}

	read, err := cast.Get(nil, "id", nil, map[string]any{"id": raw})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if read != id {
		t.Errorf("the key came back as %v, want %s", read, id)
	}
}

func TestAsBinaryRefusesAFormatItDoesNotHave(t *testing.T) {
	if _, err := (casts.AsBinary{}).CastUsing(nil); err == nil {
		t.Error("a cast with no format was built")
	}
	if _, err := (casts.AsBinary{}).Of("snowflake").CastUsing(nil); err == nil {
		t.Error("a format the codec does not have was accepted")
	}
}

func TestAsEncryptedCollectionStoresCiphertext(t *testing.T) {
	encrypter, err := encryption.NewEncrypter(make([]byte, 32), encryption.AES256GCM)
	if err != nil {
		t.Fatalf("building an encrypter: %v", err)
	}

	cast, err := casts.AsEncryptedCollection{Encrypter: encrypter}.CastUsing(nil)
	if err != nil {
		t.Fatalf("building the cast: %v", err)
	}

	written, err := cast.Set(nil, "secrets", collections.Collect([]any{"token"}), map[string]any{})
	if err != nil {
		t.Fatalf("writing: %v", err)
	}
	stored := written["secrets"]
	if text, ok := stored.(string); ok && strings.Contains(text, "token") {
		t.Fatal("the value was stored in the clear")
	}

	read, err := cast.Get(nil, "secrets", nil, map[string]any{"secrets": stored})
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	items, ok := read.(collections.Collection[any])
	if !ok || items.Count() != 1 || items.All()[0] != "token" {
		t.Errorf("the column came back as %#v", read)
	}
}

func TestAnEncryptedCastNeedsAnEncrypter(t *testing.T) {
	if _, err := (casts.AsEncryptedCollection{}).CastUsing(nil); err == nil {
		t.Error("an encrypted cast was built with no encrypter")
	}
}

func TestJsonEncodeAndDecodeCanBeReplacedAndPutBack(t *testing.T) {
	defer casts.EncodeUsing(nil)

	casts.EncodeUsing(func(value any) ([]byte, error) { return []byte(`"replaced"`), nil })

	encoded, err := casts.Encode(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if string(encoded) != `"replaced"` {
		t.Errorf("the replacement encoder was not used, got %s", encoded)
	}

	casts.EncodeUsing(nil)
	if encoded, err = casts.Encode([]any{1}); err != nil || string(encoded) != "[1]" {
		t.Errorf("encoding after the reset gave (%s, %v)", encoded, err)
	}

	decoded, err := casts.Decode(nil)
	if err != nil || decoded != nil {
		t.Errorf("an empty column decoded to (%v, %v), want nil", decoded, err)
	}
}

func TestAttributeDefaultsObjectCachingOnAsThePhpDoes(t *testing.T) {
	attribute := casts.Get(func(value any, _ map[string]any) (any, error) { return value, nil })

	if !attribute.HasGet() || attribute.HasSet() {
		t.Error("an accessor-only Attribute reported the wrong halves")
	}
	if !attribute.CachesObjects() {
		t.Error("object caching is off, and the PHP default is on")
	}

	attribute.WithoutObjectCaching()
	if attribute.CachesObjects() {
		t.Error("withoutObjectCaching did not turn it off")
	}

	var literal casts.Attribute
	if !literal.CachesObjects() {
		t.Error("an Attribute built as a literal lost the PHP default")
	}
}
