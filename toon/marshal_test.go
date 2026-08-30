package toon_test

import (
	"testing"

	"github.com/arandu-io/hesape/toon"
)

// TestMarshalEncodesAnObjectInEncounterOrder is the object example from the
// TOON 4.1.1 specification, Appendix A. Struct field order is the encounter
// order a Go caller supplies after encoding/json applies its field rules.
func TestMarshalEncodesAnObjectInEncounterOrder(t *testing.T) {
	t.Parallel()

	value := struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Active bool   `json:"active"`
	}{ID: 123, Name: "Ada", Active: true}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "id: 123\nname: Ada\nactive: true"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

// TestMarshalEncodesPrimitiveArraysInline is the primitive-array example from
// TOON 4.1.1, Appendix A. The declared count is part of the representation.
func TestMarshalEncodesPrimitiveArraysInline(t *testing.T) {
	t.Parallel()

	value := struct {
		Tags []string `json:"tags"`
	}{Tags: []string{"admin", "ops", "dev"}}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if want := "tags[3]: admin,ops,dev"; string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}

// TestMarshalCollapsesUniformObjectsIntoATable is arrays-tabular fixture zero
// from the TOON 4.1.1 conformance corpus. Shape detection selects the form; a
// caller does not opt into the table independently from the value.
func TestMarshalCollapsesUniformObjectsIntoATable(t *testing.T) {
	t.Parallel()

	type item struct {
		SKU   string  `json:"sku"`
		Qty   int     `json:"qty"`
		Price float64 `json:"price"`
	}
	value := struct {
		Items []item `json:"items"`
	}{Items: []item{{SKU: "A1", Qty: 2, Price: 9.99}, {SKU: "B2", Qty: 1, Price: 14.5}}}

	got, err := toon.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := "items[2]{sku,qty,price}:\n  A1,2,9.99\n  B2,1,14.5"
	if string(got) != want {
		t.Errorf("Marshal = %q, want %q", got, want)
	}
}
