package query

import (
	"fmt"
	"strings"
)

// Grammar answers Illuminate\Database\Query\Grammars\Grammar together with its
// base, Illuminate\Database\Grammar.
//
// In PHP the grammar is an abstract class the driver grammars extend, and the
// builder holds one. Here it is an interface, declared in the package that
// consumes it, because the concrete grammars live in query/grammars and a class
// in Go would close an import cycle: grammars imports query for *Builder, so
// query cannot import grammars for the type.
//
// The method set is the public surface of the two PHP classes, minus the
// members that only exist to let a subclass reach a protected helper. A driver
// grammar embeds BaseGrammar and overrides what its dialect spells differently,
// which is what extending the abstract class does there.
type Grammar interface {
	// CompileSelect answers Grammar::compileSelect.
	CompileSelect(query *Builder) string

	// CompileInsert answers Grammar::compileInsert.
	CompileInsert(query *Builder, values []map[string]any) string

	// CompileInsertOrIgnore answers Grammar::compileInsertOrIgnore.
	CompileInsertOrIgnore(query *Builder, values []map[string]any) string

	// CompileInsertGetID answers Grammar::compileInsertGetId. The PHP spells the
	// last word Id; Go initialisms are upper case throughout.
	CompileInsertGetID(query *Builder, values map[string]any, sequence string) string

	// CompileUpdate answers Grammar::compileUpdate.
	CompileUpdate(query *Builder, values map[string]any) string

	// CompileUpsert answers Grammar::compileUpsert.
	CompileUpsert(query *Builder, values []map[string]any, uniqueBy []string, update []string) string

	// CompileDelete answers Grammar::compileDelete.
	CompileDelete(query *Builder) string

	// CompileTruncate answers Grammar::compileTruncate. A truncate is more than
	// one statement on some engines, so the result is a statement to bindings
	// map, as the PHP returns an array keyed by SQL.
	CompileTruncate(query *Builder) map[string][]any

	// CompileExists answers Grammar::compileExists.
	CompileExists(query *Builder) string

	// CompileRandom answers Grammar::compileRandom.
	CompileRandom(seed string) string

	// PrepareBindingsForUpdate answers Grammar::prepareBindingsForUpdate.
	PrepareBindingsForUpdate(bindings map[string][]any, values map[string]any) []any

	// PrepareBindingsForDelete answers Grammar::prepareBindingsForDelete.
	PrepareBindingsForDelete(bindings map[string][]any) []any

	// SupportsSavepoints answers Grammar::supportsSavepoints.
	SupportsSavepoints() bool

	// CompileSavepoint answers Grammar::compileSavepoint.
	CompileSavepoint(name string) string

	// CompileSavepointRollBack answers Grammar::compileSavepointRollBack.
	CompileSavepointRollBack(name string) string

	// GetOperators answers Grammar::getOperators.
	GetOperators() []string

	// GetBitwiseOperators answers Grammar::getBitwiseOperators.
	GetBitwiseOperators() []string

	// Wrap answers Illuminate\Database\Grammar::wrap: it quotes an identifier,
	// leaving an Expression alone.
	Wrap(value any) string

	// WrapTable answers Grammar::wrapTable.
	WrapTable(table any) string

	// WrapArray answers Grammar::wrapArray.
	WrapArray(values []any) []string

	// Columnize answers Grammar::columnize.
	Columnize(columns []any) string

	// Parameterize answers Grammar::parameterize.
	Parameterize(values []any) string

	// Parameter answers Grammar::parameter: the placeholder for a value, or the
	// value itself when it is an Expression.
	Parameter(value any) string

	// QuoteString answers Grammar::quoteString.
	QuoteString(value any) string

	// Escape answers Grammar::escape.
	//
	// It carries an error where the PHP throws RuntimeException for a grammar
	// with no connection: escaping without one cannot be done safely, and a
	// grammar that silently returned the unescaped value would be an injection
	// with a reassuring name.
	Escape(value any, binary bool) (string, error)

	// GetDateFormat answers Grammar::getDateFormat, in Go reference-time layout
	// rather than PHP's date() letters.
	GetDateFormat() string

	// GetTablePrefix answers Grammar::getTablePrefix.
	GetTablePrefix() string

	// SetTablePrefix answers Grammar::setTablePrefix.
	SetTablePrefix(prefix string) Grammar
}

// BaseGrammar answers the shared body of Illuminate\Database\Grammar and
// Illuminate\Database\Query\Grammars\Grammar: everything the abstract classes
// implement rather than declare.
//
// A driver grammar embeds it and overrides what its dialect spells differently,
// which is what extending the abstract class does in PHP. It is exported
// because query/grammars has to embed it, and embedding is how Go says
// "extends" for the part that is code rather than contract.
type BaseGrammar struct {
	tablePrefix string
}

// operators is Builder::$operators, the list every grammar accepts. A driver
// grammar that accepts more overrides GetOperators and appends.
var operators = []string{
	"=", "<", ">", "<=", ">=", "<>", "!=", "<=>",
	"like", "like binary", "not like", "ilike",
	"&", "|", "^", "<<", ">>", "&~", "is", "is not",
	"rlike", "not rlike", "regexp", "not regexp",
	"~", "~*", "!~", "!~*", "similar to",
	"not similar to", "not ilike", "~~*", "!~~*",
}

// bitwiseOperators is Builder::$bitwiseOperators.
var bitwiseOperators = []string{"&", "|", "^", "<<", ">>", "&~"}

// GetOperators answers Grammar::getOperators.
func (g *BaseGrammar) GetOperators() []string {
	out := make([]string, len(operators))
	copy(out, operators)
	return out
}

// GetBitwiseOperators answers Grammar::getBitwiseOperators.
func (g *BaseGrammar) GetBitwiseOperators() []string {
	out := make([]string, len(bitwiseOperators))
	copy(out, bitwiseOperators)
	return out
}

// GetTablePrefix answers Grammar::getTablePrefix.
func (g *BaseGrammar) GetTablePrefix() string { return g.tablePrefix }

// SetTablePrefix answers Grammar::setTablePrefix.
func (g *BaseGrammar) SetTablePrefix(prefix string) Grammar {
	g.tablePrefix = prefix
	return nil // a driver grammar overrides this to return itself
}

// Wrap answers Grammar::wrap.
//
// An Expression passes through untouched, which is the whole reason Expression
// exists: it is how a caller says "this is SQL, not a name".
func (g *BaseGrammar) Wrap(value any) string {
	if IsExpression(value) {
		return stringify(value)
	}
	name := stringify(value)
	// "column as alias" is split before quoting, as wrapAliasedValue does.
	if i := aliasIndex(name); i >= 0 {
		return g.Wrap(strings.TrimSpace(name[:i])) + " as " +
			g.wrapValue(strings.TrimSpace(name[i+4:]))
	}
	return g.wrapSegments(name)
}

// WrapTable answers Grammar::wrapTable. The table prefix is applied here and
// nowhere else, which is why a raw table name in a where clause is a bug that
// only shows up on a prefixed connection.
func (g *BaseGrammar) WrapTable(table any) string {
	if IsExpression(table) {
		return stringify(table)
	}
	name := stringify(table)
	if i := aliasIndex(name); i >= 0 {
		return g.WrapTable(strings.TrimSpace(name[:i])) + " as " +
			g.wrapValue(g.tablePrefix+strings.TrimSpace(name[i+4:]))
	}
	return g.wrapSegments(g.tablePrefix + name)
}

// WrapArray answers Grammar::wrapArray.
func (g *BaseGrammar) WrapArray(values []any) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = g.Wrap(v)
	}
	return out
}

// Columnize answers Grammar::columnize.
func (g *BaseGrammar) Columnize(columns []any) string {
	return strings.Join(g.WrapArray(columns), ", ")
}

// Parameterize answers Grammar::parameterize.
func (g *BaseGrammar) Parameterize(values []any) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = g.Parameter(v)
	}
	return strings.Join(out, ", ")
}

// Parameter answers Grammar::parameter.
//
// The placeholder is "?" as in PHP. A grammar for an engine that numbers its
// placeholders overrides this, and overrides it knowing the number comes from
// the position in the binding list rather than from the value.
func (g *BaseGrammar) Parameter(value any) string {
	if IsExpression(value) {
		return stringify(value)
	}
	return "?"
}

// QuoteString answers Grammar::quoteString.
func (g *BaseGrammar) QuoteString(value any) string {
	if values, ok := value.([]any); ok {
		out := make([]string, len(values))
		for i, v := range values {
			out[i] = g.QuoteString(v)
		}
		return strings.Join(out, ", ")
	}
	return "'" + strings.ReplaceAll(stringify(value), "'", "''") + "'"
}

// Escape answers Grammar::escape.
//
// The base grammar has no connection to escape through, so it reports the same
// refusal the PHP throws. A driver grammar that can escape overrides it. This
// is deliberately not a best-effort quote: a caller reaching for Escape is
// building SQL by hand, and a value that looks escaped but is not is worse than
// one that refuses.
func (g *BaseGrammar) Escape(value any, binary bool) (string, error) {
	return "", fmt.Errorf("query: the base grammar has no connection to escape through; use a parameter placeholder")
}

// GetDateFormat answers Grammar::getDateFormat, translated to Go's reference
// time. The PHP returns 'Y-m-d H:i:s'.
func (g *BaseGrammar) GetDateFormat() string { return "2006-01-02 15:04:05" }

// SupportsSavepoints answers Grammar::supportsSavepoints.
func (g *BaseGrammar) SupportsSavepoints() bool { return true }

// CompileSavepoint answers Grammar::compileSavepoint.
func (g *BaseGrammar) CompileSavepoint(name string) string { return "SAVEPOINT " + name }

// CompileSavepointRollBack answers Grammar::compileSavepointRollBack.
func (g *BaseGrammar) CompileSavepointRollBack(name string) string {
	return "ROLLBACK TO SAVEPOINT " + name
}

// CompileRandom answers Grammar::compileRandom.
func (g *BaseGrammar) CompileRandom(seed string) string { return "RANDOM()" }

// wrapValue quotes one identifier segment. The base grammar uses the SQL
// standard double quote; MySQL overrides it with a backtick.
//
// A quote character inside the identifier is doubled rather than stripped,
// because stripping it would silently rename the column.
func (g *BaseGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// wrapSegments quotes each dotted segment of a qualified name.
func (g *BaseGrammar) wrapSegments(name string) string {
	segments := strings.Split(name, ".")
	out := make([]string, len(segments))
	for i, segment := range segments {
		out[i] = g.wrapValue(segment)
	}
	return strings.Join(out, ".")
}

// aliasIndex reports where the " as " of an aliased name starts, or -1. The
// search is case-insensitive, as the PHP's stripos is.
func aliasIndex(name string) int {
	return strings.Index(lower(name), " as ")
}
