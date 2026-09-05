package enums

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
)

// Priority is the closed set of values the column may hold.
//
// Go has no enum keyword, so this is the shape that gets the guarantee anyway:
// a named type, constants that are the only valid values, and a Scan/Value pair
// so a value the application does not know about is an error at the read rather
// than a zero value that silently behaves like the first case.
//
// That last part is the whole reason the file is this long: nothing refuses an
// unknown value unless the type does, and a plain `type Priority int`
// accepts anything the database hands it.
type Priority int

// The values. The numbers are explicit and never iota: iota renumbers everything
// below an insertion, and the numbers are already in the database.
const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 2
	PriorityHigh   Priority = 3
)

// PriorityValues lists them in declaration order.
//
// It feeds a <select> in a kyse view and it is what a test asserts against, so
// adding a value without deciding what the form and the migration do about it
// shows up as a failure.
func PriorityValues() []Priority {
	return []Priority{
		PriorityLow,
		PriorityNormal,
		PriorityHigh,
	}
}

// Valid reports whether v is one of the values.
func (v Priority) Valid() bool {
	switch v {
	case PriorityLow, PriorityNormal, PriorityHigh:
		return true
	}
	return false
}

// String is the name of the value, because the stored one is a number and a
// number in a log is a lookup somebody has to do at three in the morning.
func (v Priority) String() string {
	switch v {
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	}
	return fmt.Sprintf("Priority(%d)", int(v))
}

// Label is what a form shows.
//
// Separate from String on purpose: the stored value is a contract with the
// database and with every consumer of an event, and the label is a contract
// with nobody. Changing a label must not be able to change a row.
func (v Priority) Label() string {
	switch v {
	case PriorityLow:
		return "Low"
	case PriorityNormal:
		return "Normal"
	case PriorityHigh:
		return "High"
	}
	return v.String()
}

// ParsePriority turns request input into the type, or says why it cannot.
//
// app/Http/Requests calls it, so a value outside the set becomes a field error
// the form can show rather than a row.
func ParsePriority(s string) (Priority, error) {
	for _, v := range PriorityValues() {
		if v.String() == s {
			return v, nil
		}
	}
	return PriorityLow, fmt.Errorf("priority: %q is not one of low, normal, high", s)
}

// Compile-time proof that the repository can read and write it directly.
var (
	_ driver.Valuer = Priority(0)
	_ sql.Scanner   = (*Priority)(nil)
)

// Value implements driver.Valuer. It refuses to write a value outside the set,
// which is what keeps a zero-valued struct from putting a zero in the column.
func (v Priority) Value() (driver.Value, error) {
	if !v.Valid() {
		return nil, fmt.Errorf("priority: refusing to write %q", v.String())
	}
	return int64(v), nil
}

// Scan implements sql.Scanner.
//
// A value the application does not know about is an error here, at the row that
// has it, rather than a silent fallback three layers up. That is usually a
// migration that added a value the binary being rolled out does not have yet,
// which is why a migration must stay compatible with the previous binary.
func (v *Priority) Scan(src any) error {
	var n int64
	switch raw := src.(type) {
	case int64:
		n = raw
	default:
		return fmt.Errorf("priority: cannot read %T from the database", src)
	}
	parsed := Priority(n)
	if !parsed.Valid() {
		return fmt.Errorf("priority: %d is not one of the known values", n)
	}
	*v = parsed
	return nil
}

// arandu:begin custom
// Anything the set does not say: transitions (which status may follow which),
// a grouping predicate, a colour for the badge in the view.
// arandu:end custom
