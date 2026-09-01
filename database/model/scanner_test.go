package model

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// roles is the array column a project could not declare: a JSON column read
// straight into the slice, instead of a string column beside it and a pair of
// hand-written encode and decode functions.
type roles []string

// Value writes the slice as the JSON the column holds.
func (r roles) Value() (driver.Value, error) {
	encoded, err := json.Marshal([]string(r))
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

// Scan reads the column back. It refuses a nil deliberately: a NULL must never
// reach here, because the field's zero is the answer for one.
func (r *roles) Scan(src any) error {
	var encoded []byte
	switch raw := src.(type) {
	case string:
		encoded = []byte(raw)
	case []byte:
		encoded = raw
	default:
		return fmt.Errorf("roles: cannot read %T from the database", src)
	}
	return json.Unmarshal(encoded, (*[]string)(r))
}

// address is the same question asked about a struct rather than a slice.
type address struct {
	City string `json:"city"`
}

// Value writes the struct as the JSON the column holds.
func (a address) Value() (driver.Value, error) {
	encoded, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}

// Scan reads the column back into the struct.
func (a *address) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("address: cannot read %T from the database", src)
	}
	return json.Unmarshal([]byte(text), a)
}

// status is shaped like a generated enum: a string type with a closed set, a
// Value that refuses to write one outside it and a Scan that refuses to read
// one. Hydrating it used to go through the string branch, which accepted
// whatever the column held.
type status string

const (
	statusOpen status = "open"
	statusPaid status = "paid"
)

// Valid reports whether the value is one of the known ones.
func (s status) Valid() bool { return s == statusOpen || s == statusPaid }

// Value writes the value, refusing one outside the set.
func (s status) Value() (driver.Value, error) {
	if !s.Valid() {
		return nil, fmt.Errorf("status: refusing to write %q", string(s))
	}
	return string(s), nil
}

// Scan reads the value, refusing one outside the set.
func (s *status) Scan(src any) error {
	var text string
	switch raw := src.(type) {
	case string:
		text = raw
	case []byte:
		text = string(raw)
	default:
		return fmt.Errorf("status: cannot read %T from the database", src)
	}
	if parsed := status(text); parsed.Valid() {
		*s = parsed
		return nil
	}
	return fmt.Errorf("status: %q is not one of open, paid", text)
}

var (
	_ driver.Valuer = roles(nil)
	_ sql.Scanner   = (*roles)(nil)
	_ driver.Valuer = address{}
	_ sql.Scanner   = (*address)(nil)
	_ driver.Valuer = statusOpen
	_ sql.Scanner   = (*status)(nil)
)

// document is an entity whose columns are the three kinds of field that had no
// way into a struct: a slice, a struct, and a named string that validates.
type document struct {
	ID      int64   `db:"id"`
	Roles   roles   `db:"roles"`
	Address address `db:"address"`
	Status  status  `db:"status"`
	Backup  *roles  `db:"backup"`
}

func newDocumentModel() *Model[document] {
	conn := newTestConnection()
	return NewModel[document]("documents", conn, newTestGrammar(), &testProcessor{conn: conn})
}

// The whole of the gap. A field whose type reads itself out of a column never
// got the chance: assign walked assignable, pointer, time and the primitive
// kinds, and a slice of strings or a struct fell off the end as "cannot put a
// []uint8 into a model.roles field". The cost was a mirror column and a pair of
// hand-written encode and decode functions for every JSON field in an
// application.
func TestAFieldThatReadsItselfOutOfAColumnIsAskedTo(t *testing.T) {
	model := newDocumentModel()

	err := model.SetRawAttributes(map[string]any{
		"id":      int64(1),
		"roles":   []byte(`["admin","auditor"]`),
		"address": `{"city":"Asuncion"}`,
		"status":  "paid",
	}, true)
	if err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	if got := strings.Join(model.Entity.Roles, ","); got != "admin,auditor" {
		t.Errorf("Entity.Roles = %v, want [admin auditor]", model.Entity.Roles)
	}
	if model.Entity.Address.City != "Asuncion" {
		t.Errorf("Entity.Address.City = %q, want Asuncion", model.Entity.Address.City)
	}
	if model.Entity.Status != statusPaid {
		t.Errorf("Entity.Status = %q, want paid", model.Entity.Status)
	}
}

// A pointer to such a type is allocated and then asked, which is the same rule
// the primitive kinds already had.
func TestAPointerToSuchAFieldIsAllocatedAndThenAsked(t *testing.T) {
	model := newDocumentModel()

	if err := model.SetRawAttributes(map[string]any{"backup": `["archivist"]`}, true); err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	if model.Entity.Backup == nil {
		t.Fatal("Entity.Backup is nil: a pointer field is allocated before it is written")
	}
	if got := strings.Join(*model.Entity.Backup, ","); got != "archivist" {
		t.Errorf("Entity.Backup = %v, want [archivist]", *model.Entity.Backup)
	}
}

// An enum hydrated through the string branch accepted whatever the column held,
// three layers away from the row that has it. Now the type answers about its
// own column, and the answer is the row it went wrong on.
func TestAnEnumRefusesAValueOutsideItsSet(t *testing.T) {
	model := newDocumentModel()

	err := model.SetRawAttributes(map[string]any{"status": "written-off"}, true)
	if err == nil {
		t.Fatalf("SetRawAttributes accepted %q, and the column holds a closed set", "written-off")
	}
	if !strings.Contains(err.Error(), "written-off") {
		t.Errorf("the error is %q, and it has to name the value that was in the column", err)
	}
}

// The known values keep working, from either shape a driver hands back. That is
// the path this change moved, and moving it would be no better than the gap if
// it broke.
func TestAnEnumStillHydratesFromWhatADriverHandsBack(t *testing.T) {
	for _, c := range []struct {
		name  string
		value any
	}{
		{"a string", "open"},
		{"a byte slice", []byte("open")},
	} {
		t.Run(c.name, func(t *testing.T) {
			model := newDocumentModel()

			if err := model.SetRawAttributes(map[string]any{"status": c.value}, true); err != nil {
				t.Fatalf("SetRawAttributes: %v", err)
			}
			if model.Entity.Status != statusOpen {
				t.Errorf("Entity.Status = %q, want open", model.Entity.Status)
			}
		})
	}
}

// A NULL is the field's zero and is not routed to the type. A Scan written
// about the values a column holds does not have to carry a branch for the
// absence of one, and every one of these three would refuse a nil.
func TestANullIsTheZeroRatherThanSomethingTheTypeHasToRefuse(t *testing.T) {
	model := newDocumentModel()

	err := model.SetRawAttributes(map[string]any{
		"roles":   nil,
		"address": nil,
		"status":  nil,
		"backup":  nil,
	}, true)
	if err != nil {
		t.Fatalf("SetRawAttributes: %v", err)
	}

	if model.Entity.Roles != nil {
		t.Errorf("Entity.Roles = %v, want nothing", model.Entity.Roles)
	}
	if model.Entity.Address.City != "" {
		t.Errorf("Entity.Address.City = %q, want empty", model.Entity.Address.City)
	}
	if model.Entity.Status != "" {
		t.Errorf("Entity.Status = %q, want empty", model.Entity.Status)
	}
	if model.Entity.Backup != nil {
		t.Errorf("Entity.Backup = %v, want nil", model.Entity.Backup)
	}
}

// A value already of the field's own type is assigned rather than encoded and
// read back, which is what a model built in memory and saved goes through.
func TestAValueAlreadyOfTheFieldsTypeIsAssignedDirectly(t *testing.T) {
	model := newDocumentModel()

	if err := model.SetAttribute("roles", roles{"admin"}); err != nil {
		t.Fatalf("SetAttribute: %v", err)
	}

	if got := strings.Join(model.Entity.Roles, ","); got != "admin" {
		t.Errorf("Entity.Roles = %v, want [admin]", model.Entity.Roles)
	}
}

// The failure names the column and the type, because "cannot read" without
// either is a message that sends somebody looking through the whole row.
func TestTheFailureNamesTheColumnAndTheType(t *testing.T) {
	model := newDocumentModel()

	err := model.SetRawAttributes(map[string]any{"roles": int64(7)}, true)
	if err == nil {
		t.Fatal("SetRawAttributes accepted an integer for a JSON column")
	}
	for _, want := range []string{"documents.roles", "model.roles"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error is %q, and it has to name %q", err, want)
		}
	}
}
