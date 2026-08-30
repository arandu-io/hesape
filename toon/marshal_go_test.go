package toon_test

import (
	"encoding"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/toon"
)

type orderedMarshaler struct{}

func (orderedMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"z":1,"a":2}`), nil
}

type pointerMarshaler struct {
	Invalid string
}

func (*pointerMarshaler) MarshalJSON() ([]byte, error) { return []byte(`"safe"`), nil }

type safeTextMarshaler struct {
	Invalid string
}

func (safeTextMarshaler) MarshalText() ([]byte, error) { return []byte("safe"), nil }

type invalidTextMarshaler struct{}

func (invalidTextMarshaler) MarshalText() ([]byte, error) { return []byte{0xff}, nil }

type nilTextMarshaler struct{}

func (*nilTextMarshaler) MarshalText() ([]byte, error) {
	return nil, errors.New("nil text marshaler was called")
}

type changingTextMarshaler struct {
	calls *int
}

func (m changingTextMarshaler) MarshalText() ([]byte, error) {
	*m.calls++
	if *m.calls == 1 {
		return []byte{0xff}, nil
	}
	return []byte("safe"), nil
}

type stringKeyWithHooks string

func (stringKeyWithHooks) MarshalJSON() ([]byte, error) {
	return nil, errors.New("MarshalJSON was called for a map key")
}

func (stringKeyWithHooks) MarshalText() ([]byte, error) {
	return nil, errors.New("MarshalText was called for a string map key")
}

type embeddedFields struct {
	Value string `json:"value"`
}

type outerWithPromotedFields struct {
	embeddedFields
}

type rawNumber string

func (n rawNumber) MarshalJSON() ([]byte, error) { return []byte(n), nil }

type loneSurrogate struct{}

func (loneSurrogate) MarshalJSON() ([]byte, error) { return []byte(`"\ud800"`), nil }

func TestPublicConstantsPinTheWireContract(t *testing.T) {
	if toon.SpecVersion != "4.1" {
		t.Errorf("SpecVersion = %q, want 4.1", toon.SpecVersion)
	}
	if toon.MediaType != "text/toon" {
		t.Errorf("MediaType = %q, want text/toon", toon.MediaType)
	}
}

func TestMarshalAppliesJSONFieldTagsAndOmissions(t *testing.T) {
	value := struct {
		Visible string `json:"visible"`
		Empty   string `json:"empty,omitempty"`
		Secret  string `json:"-"`
		Count   int    `json:"count"`
	}{Visible: "yes", Secret: string([]byte{0xff}), Count: 2}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "visible: yes\ncount: 2"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalHonorsJSONMarshalerAndItsObjectOrder(t *testing.T) {
	got, err := toon.Marshal(orderedMarshaler{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "z: 1\na: 2"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalHonorsPointerJSONMarshalerOnAddressableValues(t *testing.T) {
	value := []pointerMarshaler{{Invalid: string([]byte{0xff})}}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "[1]: safe"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalHonorsTextMarshalerBeforeInspectingItsFields(t *testing.T) {
	got, err := toon.Marshal(safeTextMarshaler{Invalid: string([]byte{0xff})})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "safe"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalDoesNotCallTextMarshalerOnANilPointer(t *testing.T) {
	value := struct {
		Replacement json.RawMessage   `json:"replacement"`
		Optional    *nilTextMarshaler `json:"optional"`
	}{Replacement: json.RawMessage(`"\ufffd"`)}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "replacement: �\noptional: null"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalTreatsANilTextMarshalerInterfaceAsNull(t *testing.T) {
	value := struct {
		Replacement json.RawMessage        `json:"replacement"`
		Optional    encoding.TextMarshaler `json:"optional"`
	}{Replacement: json.RawMessage(`"\ufffd"`)}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "replacement: �\noptional: null"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalSortsGoMapKeysDeterministically(t *testing.T) {
	for range 20 {
		got, err := toon.Marshal(map[string]any{"z": 1, "a": 2})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if want := "a: 2\nz: 1"; string(got) != want {
			t.Fatalf("Marshal = %q, want %q", got, want)
		}
	}
}

func TestMarshalCanonicalizesJSONNumberLexemes(t *testing.T) {
	tests := []struct {
		input rawNumber
		want  string
	}{
		{input: "-0.00", want: "0"},
		{input: "1.5000", want: "1.5"},
		{input: "1e6", want: "1000000"},
		{input: "1e-6", want: "0.000001"},
		{input: "1e-7", want: "1e-7"},
		{input: "1E21", want: "1e+21"},
	}

	for _, test := range tests {
		t.Run(string(test.input), func(t *testing.T) {
			got, err := toon.Marshal(test.input)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != test.want {
				t.Errorf("Marshal = %q, want %q", got, test.want)
			}
		})
	}
}

func TestMarshalRefusesMalformedUTF8(t *testing.T) {
	_, err := toon.Marshal(string([]byte{0xff}))
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
	}
}

func TestMarshalRefusesMalformedUTF8InAliasedSliceViews(t *testing.T) {
	values := []string{"valid", string([]byte{0xff})}
	input := []any{values[:1], values[:2]}

	_, err := toon.Marshal(input)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
	}
}

func TestMarshalRefusesMalformedUTF8FromTextMarshaler(t *testing.T) {
	for name, value := range map[string]any{
		"value": invalidTextMarshaler{},
		"map key": map[invalidTextMarshaler]int{
			{}: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := toon.Marshal(value)
			if err == nil || !strings.Contains(err.Error(), "UTF-8") {
				t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
			}
		})
	}
}

func TestMarshalCallsTextMarshalerOnlyOnceWhenItsOutputIsInvalid(t *testing.T) {
	calls := 0
	_, err := toon.Marshal(changingTextMarshaler{calls: &calls})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
	}
	if calls != 1 {
		t.Errorf("MarshalText called %d times, want exactly once", calls)
	}
}

func TestMarshalTreatsNamedStringMapKeysAsStringsBeforeHooks(t *testing.T) {
	value := struct {
		Replacement json.RawMessage            `json:"replacement"`
		Values      map[stringKeyWithHooks]int `json:"values"`
	}{
		Replacement: json.RawMessage(`"\ufffd"`),
		Values:      map[stringKeyWithHooks]int{"safe": 1},
	}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "replacement: �\nvalues:\n  safe: 1"
	if string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

func TestMarshalRefusesMalformedUTF8InNamedStringMapKeysDespiteHooks(t *testing.T) {
	key := stringKeyWithHooks(string([]byte{0xff}))

	_, err := toon.Marshal(map[stringKeyWithHooks]int{key: 1})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
	}
}

func TestMarshalRefusesMalformedUTF8InPromotedFields(t *testing.T) {
	value := outerWithPromotedFields{embeddedFields{Value: string([]byte{0xff})}}

	_, err := toon.Marshal(value)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
	}
}

func TestMarshalRefusesMalformedUTF8InALiteralDashField(t *testing.T) {
	value := struct {
		Dash string `json:"-,"`
	}{Dash: string([]byte{0xff})}

	_, err := toon.Marshal(value)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("Marshal error = %v, want an invalid UTF-8 error", err)
	}
}

func TestMarshalRefusesAnUnpairedSurrogateFromJSONMarshaler(t *testing.T) {
	_, err := toon.Marshal(loneSurrogate{})
	if err == nil || !strings.Contains(err.Error(), "surrogate") {
		t.Fatalf("Marshal error = %v, want an unpaired surrogate error", err)
	}
}

func TestMarshalRefusesCyclesAndUnsupportedValues(t *testing.T) {
	type cyclic struct {
		Self *cyclic `json:"self"`
	}
	cycle := &cyclic{}
	cycle.Self = cycle

	for name, value := range map[string]any{
		"cycle":    cycle,
		"channel":  make(chan int),
		"NaN":      math.NaN(),
		"infinity": math.Inf(1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := toon.Marshal(value); err == nil {
				t.Fatalf("Marshal accepted %s outside the JSON data model", name)
			}
		})
	}
}

func TestMarshalRefusesDuplicateJSONObjectKeys(t *testing.T) {
	_, err := toon.Marshal(json.RawMessage(`{"a":1,"a":2}`))
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Marshal error = %v, want a duplicate key error", err)
	}
}

func TestMarshalRefusesExcessiveNesting(t *testing.T) {
	var value any = "leaf"
	for range 1001 {
		value = []any{value}
	}

	_, err := toon.Marshal(value)
	if err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("Marshal error = %v, want a nesting limit error", err)
	}
}
