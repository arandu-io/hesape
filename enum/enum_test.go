package enum_test

import (
	"errors"
	"testing"

	"github.com/arandu-io/hesape/enum"
	"github.com/arandu-io/hesape/internal/enums"
)

// The contract is satisfied by what the generator emits, without a line being
// added to the generated file. A type that stopped satisfying it would fail to
// compile here, which is the point of asserting it at package scope.
var (
	_ enum.Enum = enums.InvoiceStatusDraft
	_ enum.Enum = enums.PriorityLow
)

func TestOptionsReadTheCasesOffTheType(t *testing.T) {
	got := enum.Options(enums.InvoiceStatusValues()...)

	want := []enum.Option{
		{Value: "draft", Label: "Draft"},
		{Value: "sent", Label: "Sent"},
		{Value: "paid", Label: "Paid"},
		{Value: "void", Label: "Void"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d options, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("option %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestOptionsOfAnIntegerBackedSetCarryTheShownSpelling(t *testing.T) {
	got := enum.Options(enums.PriorityValues()...)

	if len(got) != 3 {
		t.Fatalf("got %d options, want 3: %v", len(got), got)
	}
	// The value is what the generated parser reads back, and the parser
	// compares String -- so a <select> that sent the number would round-trip
	// into an error.
	if got[1].Value != "normal" || got[1].Label != "Normal" {
		t.Errorf("option 1 = %+v, want {normal Normal}", got[1])
	}
}

func TestNames(t *testing.T) {
	got := enum.Names(enums.InvoiceStatusValues()...)
	want := []string{"draft", "sent", "paid", "void"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("name %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFrom(t *testing.T) {
	status := enums.InvoiceStatusPaid

	for _, c := range []struct {
		name  string
		value any
		want  bool
	}{
		{"a value", status, true},
		{"a pointer", &status, true},
		{"a value outside the set is still of the type", enums.InvoiceStatus("nope"), true},
		{"a plain string is not an enum", "draft", false},
		{"nil", nil, false},
		{"a nil pointer", (*enums.InvoiceStatus)(nil), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := enum.From(c.value)
			if ok != c.want {
				t.Fatalf("From(%v) ok = %v, want %v", c.value, ok, c.want)
			}
			if ok && got == nil {
				t.Fatal("From reported an enum and returned nil")
			}
		})
	}
}

func TestFromReadsMembershipOffTheValue(t *testing.T) {
	got, ok := enum.From(enums.InvoiceStatus("deleted"))
	if !ok {
		t.Fatal("a value of the type is an enum whatever it holds")
	}
	if got.Valid() {
		t.Error(`"deleted" is not a case and Valid said it was`)
	}
}

func TestCast(t *testing.T) {
	for _, c := range []struct {
		name       string
		proto      enum.Enum
		text       string
		castable   bool
		wantMember bool
	}{
		{"a case of a text-backed set", enums.InvoiceStatusDraft, "sent", true, true},
		{"a stranger is castable and not a member", enums.InvoiceStatusDraft, "deleted", true, false},
		{"the stored number of an integer-backed set", enums.PriorityLow, "2", true, true},
		{"a number outside the set", enums.PriorityLow, "9", true, false},
		{"the shown spelling of an integer-backed set is not the stored one", enums.PriorityLow, "normal", false, false},
		{"no prototype", nil, "draft", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, ok := enum.Cast(c.proto, c.text)
			if ok != c.castable {
				t.Fatalf("Cast ok = %v, want %v", ok, c.castable)
			}
			if !ok {
				return
			}
			if got.Valid() != c.wantMember {
				t.Errorf("Valid() = %v, want %v", got.Valid(), c.wantMember)
			}
		})
	}
}

func TestHolds(t *testing.T) {
	if !enum.Holds(enums.InvoiceStatusDraft, "void") {
		t.Error(`"void" is a case and Holds said it was not`)
	}
	if enum.Holds(enums.InvoiceStatusDraft, "deleted") {
		t.Error(`"deleted" is not a case and Holds said it was`)
	}
	if !enum.Holds(enums.PriorityLow, "3") {
		t.Error("3 is a stored case of Priority and Holds said it was not")
	}
}

func TestUnknownFindsTheListThatDisagreesWithTheType(t *testing.T) {
	unknown, comparable := enum.Unknown(enums.InvoiceStatusDraft, []string{"draft", "sent", "paid", "void"})
	if !comparable {
		t.Fatal("a text-backed set is comparable")
	}
	if len(unknown) != 0 {
		t.Errorf("a list that matches the type reported %v", unknown)
	}

	unknown, comparable = enum.Unknown(enums.InvoiceStatusDraft, []string{"draft", "cancelled"})
	if !comparable {
		t.Fatal("a text-backed set is comparable")
	}
	if len(unknown) != 1 || unknown[0] != "cancelled" {
		t.Errorf("unknown = %v, want [cancelled]", unknown)
	}
}

func TestUnknownSaysWhatItCannotAnswer(t *testing.T) {
	// The list is written in shown spellings and the type stores numbers.
	// Answering "they all match" or "none of them match" would both be wrong.
	if _, comparable := enum.Unknown(enums.PriorityLow, []string{"low", "normal"}); comparable {
		t.Error("an integer-backed set cannot be compared against a list of names")
	}
	if _, comparable := enum.Unknown(nil, []string{"low"}); comparable {
		t.Error("no prototype is not comparable")
	}
}

func TestMarshal(t *testing.T) {
	got, err := enum.Marshal(enums.InvoiceStatusPaid)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `"paid"` {
		t.Errorf("Marshal = %s, want %q", got, `"paid"`)
	}

	got, err = enum.Marshal(enums.PriorityHigh)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != "3" {
		t.Errorf("Marshal = %s, want 3", got)
	}
}

func TestMarshalRefusesAValueOutsideTheSet(t *testing.T) {
	_, err := enum.Marshal(enums.InvoiceStatus("deleted"))
	if !errors.Is(err, enum.ErrNotACase) {
		t.Fatalf("Marshal error = %v, want ErrNotACase", err)
	}
}

func TestUnmarshal(t *testing.T) {
	got, err := enum.Unmarshal(enums.InvoiceStatusDraft, []byte(`"void"`))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != enums.InvoiceStatusVoid {
		t.Errorf("Unmarshal = %v, want void", got)
	}

	got, err = enum.Unmarshal(enums.PriorityLow, []byte("2"))
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != enums.PriorityNormal {
		t.Errorf("Unmarshal = %v, want normal", got)
	}
}

func TestUnmarshalRefusesACaseTheTypeDoesNotKnow(t *testing.T) {
	for _, c := range []struct {
		name  string
		proto enum.Enum
		data  string
	}{
		{"a stranger", enums.InvoiceStatusDraft, `"deleted"`},
		{"a number outside the set", enums.PriorityLow, "9"},
		{"a fraction", enums.PriorityLow, "1.5"},
		{"a name where the type stores numbers", enums.PriorityLow, `"low"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := enum.Unmarshal(c.proto, []byte(c.data)); !errors.Is(err, enum.ErrNotACase) {
				t.Fatalf("Unmarshal error = %v, want ErrNotACase", err)
			}
		})
	}
}
