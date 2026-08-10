package query

// Expression answers Illuminate\Database\Query\Expression.
//
// A value the grammar must not wrap in identifier quotes and must not turn into
// a placeholder. Everything the query builder accepts as a column or a value
// also accepts an Expression, exactly as in PHP.
type Expression struct {
	value any
}

// Raw answers `new Expression($value)` and Illuminate's `DB::raw()` helper.
func Raw(value any) Expression {
	return Expression{value: value}
}

// GetValue answers Expression::getValue.
//
// The PHP takes the grammar so a grammar-aware expression can render itself; the
// value carried here is already final, so the grammar is accepted and ignored to
// keep the signature recognisable.
func (e Expression) GetValue(grammar Grammar) any {
	return e.value
}

// Value returns the wrapped value without a grammar. It is the mechanical
// convenience the PHP does not need because `getValue` is always reachable.
func (e Expression) Value() any {
	return e.value
}

// String renders the expression the way the grammar concatenates it.
func (e Expression) String() string {
	return stringify(e.value)
}

// IsExpression answers Grammar::isExpression.
func IsExpression(value any) bool {
	switch value.(type) {
	case Expression, *Expression:
		return true
	}
	return false
}
