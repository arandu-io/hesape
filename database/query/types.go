package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Record is one row as the connection hands it back: column name to value.
// Illuminate returns a stdClass per row; a Go map is the same thing without the
// dynamic property access PHP needs.
type Record = map[string]any

// Where is one entry of Illuminate\Database\Query\Builder::$wheres.
//
// PHP builds an untyped array per clause, with a different key set per Type.
// The union of those keys is spelled out here so the grammar can read them.
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

// Having is one entry of Illuminate\Database\Query\Builder::$havings.
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

// Order is one entry of Illuminate\Database\Query\Builder::$orders.
type Order struct {
	Column    any
	Direction string
	SQL       any
}

// Union is one entry of Illuminate\Database\Query\Builder::$unions.
type Union struct {
	Query *Builder
	All   bool
}

// Aggregate is Illuminate\Database\Query\Builder::$aggregate: the function name
// and the columns it is applied to.
type Aggregate struct {
	Function string
	Columns  []any
}

// IndexHint answers Illuminate\Database\Query\IndexHint.
type IndexHint struct {
	Type  string
	Index string
}

// NewIndexHint answers IndexHint::__construct.
func NewIndexHint(typ, index string) *IndexHint {
	return &IndexHint{Type: typ, Index: index}
}

// GroupLimit is Illuminate\Database\Query\Builder::$groupLimit.
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
