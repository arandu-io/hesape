package concerns_test

import (
	"math"
	"testing"
	"time"

	"github.com/arandu-io/hesape/database/eloquent/casts"
	"github.com/arandu-io/hesape/database/eloquent/concerns"
	"github.com/arandu-io/hesape/encryption"
)

func TestMergeCastsDeclaresAndThenAdds(t *testing.T) {
	var attributes concerns.HasAttributes

	attributes.MergeCasts(map[string]any{"published": "bool"})
	attributes.MergeCasts(map[string]any{"price": "decimal:2"})

	if !attributes.HasCast("published") {
		t.Error("the first declaration was lost by the second")
	}
	if !attributes.HasCast("price", "decimal") {
		t.Errorf("the cast type of price is %q, want decimal", attributes.GetCastType("price"))
	}
	if attributes.HasCast("published", "int") {
		t.Error("a cast matched a type it is not")
	}
	if attributes.HasCast("missing") {
		t.Error("a column with no cast reported one")
	}
}

func TestGetCastTypeFoldsTheParameterisedForms(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.MergeCasts(map[string]any{
		"born":      "datetime:2006-01-02",
		"retired":   "immutable_datetime:2006-01-02",
		"price":     "decimal:4",
		"published": " Bool ",
		"options":   "array",
	})

	for column, want := range map[string]string{
		"born":      "custom_datetime",
		"retired":   "immutable_custom_datetime",
		"price":     "decimal",
		"published": "bool",
		"options":   "array",
	} {
		if got := attributes.GetCastType(column); got != want {
			t.Errorf("the cast type of %s is %q, want %q", column, got, want)
		}
	}

	if !attributes.IsJsonCastable("options") {
		t.Error("an array cast is not JSON castable")
	}
	if attributes.IsEncryptedCastable("options") {
		t.Error("a plain array cast reported as encrypted")
	}
}

func TestTheMutatorRegistryAnswersEveryLookup(t *testing.T) {
	var attributes concerns.HasAttributes

	if attributes.HasAttributeMutator("title") {
		t.Error("a key with nothing registered has a mutator")
	}

	attributes.SetAttributeMutator("title", casts.Get(
		func(value any, _ map[string]any) (any, error) { return "read:" + value.(string), nil },
	))
	attributes.SetAttributeMutator("slug", casts.Set(
		func(value any, _ map[string]any) (map[string]any, error) {
			return map[string]any{"slug": "written:" + value.(string)}, nil
		},
	))

	if !attributes.HasAttributeMutator("title") || !attributes.HasAttributeGetMutator("title") {
		t.Error("the accessor was not found")
	}
	if attributes.HasAttributeSetMutator("title") {
		t.Error("an accessor-only attribute reported a mutator")
	}
	if !attributes.HasSetMutator("slug") || attributes.HasGetMutator("slug") {
		t.Error("the mutator-only attribute was read wrong")
	}
	if !attributes.HasAnyGetMutator("title") {
		t.Error("hasAnyGetMutator missed the accessor")
	}

	if got := attributes.GetMutatedAttributes(); len(got) != 1 || got[0] != "title" {
		t.Errorf("the mutated attributes are %v, want [title]", got)
	}

	read, err := attributes.MutateAttribute("title", "hello")
	if err != nil {
		t.Fatalf("reading through the accessor: %v", err)
	}
	if read != "read:hello" {
		t.Errorf("the accessor answered %v, want read:hello", read)
	}

	if err := attributes.SetMutatedAttributeValue("slug", "hello"); err != nil {
		t.Fatalf("writing through the mutator: %v", err)
	}
	if got := attributes.GetAttribute("slug"); got != "written:hello" {
		t.Errorf("the mutator wrote %v, want written:hello", got)
	}
}

func TestGetMutatedAttributesIsRebuiltWhenTheRegistryChanges(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.SetAttributeMutator("title", casts.Get(
		func(value any, _ map[string]any) (any, error) { return value, nil },
	))

	if len(attributes.GetMutatedAttributes()) != 1 {
		t.Fatal("the first list was not built")
	}

	attributes.SetAttributeMutator("title", nil)

	if got := attributes.GetMutatedAttributes(); len(got) != 0 {
		t.Errorf("the list is %v after the accessor was removed, want empty", got)
	}
}

func TestFromDateTimeAndBack(t *testing.T) {
	var attributes concerns.HasAttributes

	moment := time.Date(2026, 8, 13, 9, 30, 0, 0, time.UTC)
	stored, err := attributes.FromDateTime(moment)
	if err != nil {
		t.Fatalf("storing a date: %v", err)
	}
	if stored != "2026-08-13 09:30:00" {
		t.Errorf("the stored date is %q, want 2026-08-13 09:30:00", stored)
	}

	attributes.SetDateFormat("2006-01-02")
	stored, err = attributes.FromDateTime(stored)
	if err != nil {
		t.Fatalf("reading the stored date back: %v", err)
	}
	if stored != "2026-08-13" {
		t.Errorf("the reformatted date is %q, want 2026-08-13", stored)
	}

	if empty, err := attributes.FromDateTime(nil); err != nil || empty != "" {
		t.Errorf("an empty value gave (%q, %v), want an empty string and no error", empty, err)
	}
	if _, err := attributes.FromDateTime("not a date"); err == nil {
		t.Error("a value that is not a date was accepted")
	}
}

func TestFromDateTimeReadsATimestamp(t *testing.T) {
	var attributes concerns.HasAttributes

	stored, err := attributes.FromDateTime(int64(1786000000))
	if err != nil {
		t.Fatalf("reading a unix timestamp: %v", err)
	}
	if want := time.Unix(1786000000, 0).Format(concerns.DefaultDateFormat); stored != want {
		t.Errorf("the stored date is %q, want %q", stored, want)
	}
}

func TestFromJsonAndAsJson(t *testing.T) {
	var attributes concerns.HasAttributes

	encoded, err := attributes.AsJson(map[string]any{"email": true})
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	decoded, err := attributes.FromJson(encoded)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	bag, ok := decoded.(map[string]any)
	if !ok || bag["email"] != true {
		t.Errorf("the column round-tripped to %#v", decoded)
	}

	for _, empty := range []any{nil, "", []byte(nil)} {
		if decoded, err := attributes.FromJson(empty); err != nil || decoded != nil {
			t.Errorf("an empty column gave (%v, %v), want nil and no error", decoded, err)
		}
	}
}

func TestFromFloatReadsTheThreeNames(t *testing.T) {
	var attributes concerns.HasAttributes

	if got, err := attributes.FromFloat("Infinity"); err != nil || !math.IsInf(got, 1) {
		t.Errorf("Infinity gave (%v, %v)", got, err)
	}
	if got, err := attributes.FromFloat("-Infinity"); err != nil || !math.IsInf(got, -1) {
		t.Errorf("-Infinity gave (%v, %v)", got, err)
	}
	if got, err := attributes.FromFloat("NaN"); err != nil || !math.IsNaN(got) {
		t.Errorf("NaN gave (%v, %v)", got, err)
	}
	if got, err := attributes.FromFloat("1.5"); err != nil || got != 1.5 {
		t.Errorf("1.5 gave (%v, %v)", got, err)
	}
	if _, err := attributes.FromFloat("a lot"); err == nil {
		t.Error("a value that is not a number was accepted")
	}
}

func TestFillJsonAttributeWritesOnePath(t *testing.T) {
	var attributes concerns.HasAttributes
	attributes.MergeCasts(map[string]any{"options": "array"})
	attributes.SetRawAttributes(map[string]any{"options": `{"notifications":{"sms":false}}`}, true)

	if err := attributes.FillJsonAttribute("options->notifications->email", true); err != nil {
		t.Fatalf("filling a JSON path: %v", err)
	}

	decoded, err := attributes.FromJson(attributes.GetAttribute("options"))
	if err != nil {
		t.Fatalf("reading the column back: %v", err)
	}
	notifications, ok := decoded.(map[string]any)["notifications"].(map[string]any)
	if !ok {
		t.Fatalf("the column holds %#v", decoded)
	}
	if notifications["email"] != true {
		t.Error("the written path is not in the column")
	}
	if notifications["sms"] != false {
		t.Error("filling one path dropped a sibling")
	}
}

func TestFillJsonAttributeNeedsAPath(t *testing.T) {
	var attributes concerns.HasAttributes
	if err := attributes.FillJsonAttribute("options", true); err == nil {
		t.Error("a key with no -> segment was accepted")
	}
}

func TestEncryptedAttributesRoundTripThroughTheCurrentEncrypter(t *testing.T) {
	encrypter, err := encryption.NewEncrypter(make([]byte, 32), encryption.AES256GCM)
	if err != nil {
		t.Fatalf("building an encrypter: %v", err)
	}

	concerns.EncryptUsing(encrypter)
	defer concerns.EncryptUsing(nil)

	var attributes concerns.HasAttributes
	stored, err := attributes.CastAttributeAsEncryptedString("secret", "hello")
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}
	if stored == "hello" {
		t.Fatal("the value was stored in the clear")
	}

	plain, err := attributes.FromEncryptedString(stored)
	if err != nil {
		t.Fatalf("decrypting: %v", err)
	}
	if plain != "hello" {
		t.Errorf("the value came back as %q, want hello", plain)
	}
}

func TestWithoutAnEncrypterAnEncryptedAttributeIsAnError(t *testing.T) {
	concerns.EncryptUsing(nil)

	var attributes concerns.HasAttributes
	if _, err := attributes.FromEncryptedString("whatever"); err == nil {
		t.Error("a value came back with no encrypter set")
	}
}

func TestResolveRouteBindingQueryRefusesAKeyThatCannotBeOne(t *testing.T) {
	ids := concerns.HasUUIDs()

	if err := ids.ResolveRouteBindingQuery("7", "", "id", "id"); err == nil {
		t.Error("an integer was accepted as the route key of a model keyed by UUID")
	}
	if err := ids.ResolveRouteBindingQuery("0196f0d5-2d4c-7000-8000-000000000000", "", "id", "id"); err != nil {
		t.Errorf("a valid UUID was refused: %v", err)
	}
	if err := ids.ResolveRouteBindingQuery("anything", "slug", "id", "id"); err != nil {
		t.Errorf("a column that holds no generated key was checked anyway: %v", err)
	}

	var plain concerns.HasUniqueIDs
	if err := plain.ResolveRouteBindingQuery("7", "", "id", "id"); err != nil {
		t.Errorf("a model that generates no keys was checked anyway: %v", err)
	}
}

func TestGetDatesIsTheTimestampPairOrNothing(t *testing.T) {
	var timestamps concerns.HasTimestamps

	dates := timestamps.GetDates()
	if len(dates) != 2 || dates[0] != concerns.CreatedAt || dates[1] != concerns.UpdatedAt {
		t.Errorf("the dates are %v, want the timestamp pair", dates)
	}

	timestamps.SetTimestamps(false)
	if got := timestamps.GetDates(); len(got) != 0 {
		t.Errorf("a model without timestamps has dates %v", got)
	}
}
