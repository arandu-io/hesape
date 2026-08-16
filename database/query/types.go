package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Record is one row as the connection hands it back: column name to value.
type Record = map[string]any

// Where is one where clause of a Builder.
//
// Each Type reads a different subset of the fields; the union of them is spelled
// out here so the grammar can read them.
type Where struct {
	Type          string
	Column        any
	Columns       []any
	First         any
	Second        any
	Operator      string
	Value         any
	Values        []any
	Boolean       string
	Not           bool
	SQL           any
	Query         *Builder
	CaseSensitive bool
	Options       map[string]any
}

// Having is one having clause of a Builder.
type Having struct {
	Type     string
	Column   any
	Operator string
	Value    any
	Values   []any
	Boolean  string
	Not      bool
	SQL      string
	Query    *Builder
}

// Order is one order-by clause of a Builder.
type Order struct {
	Column    any
	Direction string
	SQL       any
}

// Union is one query unioned onto a Builder.
type Union struct {
	Query *Builder
	All   bool
}

// Aggregate is the aggregate a Builder is compiled as: the function name and
// the columns it is applied to.
type Aggregate struct {
	Function string
	Columns  []any
}

// IndexHint is the index a query asks the planner to use, force or ignore: what
// Builder.UseIndex, ForceIndex and IgnoreIndex record, and what the grammar
// compiles into the from clause.
//
// Type is one of "hint", "force" and "ignore", and Index is the index name. A
// builder holds at most one, so setting a second replaces the first.
//
// A hint changes the plan and never the rows, which is why a grammar is free to
// compile it to nothing: the base Grammar does, SQLite honours only "force",
// and an index name that is not a bare identifier is dropped rather than
// interpolated, because an identifier cannot be bound.
type IndexHint struct {
	Type  string
	Index string
}

// NewIndexHint builds an IndexHint of the given type and index name.
func NewIndexHint(typ, index string) *IndexHint {
	return &IndexHint{Type: typ, Index: index}
}

// GroupLimit is the per-group row limit a Builder was given.
type GroupLimit struct {
	Value  int
	Column string
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case Expression:
		return stringify(t.value)
	case *Expression:
		return stringify(t.value)
	case fmt.Stringer:
		return t.String()
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(v)
	}
}

func lower(s string) string { return strings.ToLower(s) }

func intPtr(v int) *int { return &v }
