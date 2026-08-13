package query

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/str"
)

// The rest of the where vocabulary of Illuminate\Database\Query\Builder: the
// clause types the grammar knows how to compile and the "or" twin of each one.
//
// PHP writes the conjunction and the negation as trailing arguments -- where(...,
// $boolean = 'and', $not = false) -- and Go has no default argument, so each of
// them is a method here, which is the pair of names the PHP also declares.
// Nothing in this file reaches the connection: a where clause is state, and the
// tenant is put on the statement at execution, in scoped.

// PrepareValueAndOperator answers Builder::prepareValueAndOperator.
//
// It carries an error where the PHP throws InvalidArgumentException, for the
// combination it refuses: an operator with no value, such as where('votes',
// '>'), which would otherwise compile to a comparison against NULL and quietly
// match nothing.
//
// The fluent methods do not call it, because the question it answers -- "were
// two arguments passed, or three?" -- is func_num_args in PHP and is the length
// of a variadic slice here. It is exported because it is public there, and
// because a caller assembling an operator and a value from user input has the
// same question to ask.
func (b *Builder) PrepareValueAndOperator(value, operator any, useDefault bool) (any, string, error) {
	if useDefault {
		return operator, "=", nil
	}
	if invalidOperatorAndValue(stringify(operator), value) {
		return nil, "", fmt.Errorf("query: illegal operator and value combination")
	}
	return value, stringify(operator), nil
}

// invalidOperatorAndValue answers Builder::invalidOperatorAndValue: an operator
// that needs a value, with no value given.
func invalidOperatorAndValue(operator string, value any) bool {
	if value != nil {
		return false
	}
	switch operator {
	case "=", "<>", "!=":
		return false
	}
	return containsFold(operators, operator)
}

// invalidOperator answers Builder::invalidOperator: an operator no grammar in
// the list accepts, which is how the PHP tells where('votes', 100) from
// where('votes', '>', 100) after the arguments have been shuffled.
func (b *Builder) invalidOperator(operator any) bool {
	name, ok := operator.(string)
	if !ok {
		return true
	}
	if containsFold(operators, name) {
		return false
	}
	if b.Grammar == nil {
		return true
	}
	return !containsFold(b.Grammar.GetOperators(), name)
}

func containsFold(list []string, value string) bool {
	for _, item := range list {
		if item == lower(value) {
			return true
		}
	}
	return false
}

// shortcutOperator answers the three lines every date and JSON length clause
// repeats: an operator that is not an operator was meant as the value, with "="
// understood.
func (b *Builder) shortcutOperator(operator string, value any) (string, any) {
	if b.invalidOperator(operator) {
		return "=", operator
	}
	return operator, value
}

// flattenValue answers Builder::flattenValue: the first leaf of a value that
// arrived as a list.
func flattenValue(value any) any {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return value
	}
	return flattenValue(values[0])
}

// refuse records a clause that could not be built as a false one carrying the
// reason, which is what the grammars do with a clause they cannot compile.
//
// The PHP throws instead. A fluent method has no error to return, and the two
// alternatives are worse: dropping the clause widens the query to rows nobody
// filtered for, and panicking takes down a request over a query that was about
// to be refused anyway.
func (b *Builder) refuse(boolean string, err error) *Builder {
	b.Wheres = append(b.Wheres, Where{
		Type:    "Raw",
		SQL:     "1 = 0 /* " + strings.ReplaceAll(err.Error(), "*/", "* /") + " */",
		Boolean: boolean,
	})
	return b
}

// MergeWheres answers Builder::mergeWheres.
//
// The bindings are appended to the where segment as the PHP does. They are the
// caller's to line up with the clauses: the two arguments are the two halves of
// somebody else's query, and nothing here can check that they match.
func (b *Builder) MergeWheres(wheres []Where, bindings []any) *Builder {
	b.Wheres = append(b.Wheres, wheres...)
	b.Bindings["where"] = append(b.Bindings["where"], bindings...)
	return b
}

// OrWhereColumn answers Builder::orWhereColumn.
func (b *Builder) OrWhereColumn(first any, args ...any) *Builder {
	operator, second := prepareValueAndOperator(args...)
	b.Wheres = append(b.Wheres, Where{
		Type:     "Column",
		First:    first,
		Operator: operator,
		Second:   second,
		Boolean:  "or",
	})
	return b
}

// OrWhereExists answers Builder::orWhereExists.
func (b *Builder) OrWhereExists(callback func(*Builder), not ...bool) *Builder {
	return b.WhereExists(callback, "or", len(not) > 0 && not[0])
}

// OrWhereNotExists answers Builder::orWhereNotExists.
func (b *Builder) OrWhereNotExists(callback func(*Builder)) *Builder {
	return b.OrWhereExists(callback, true)
}

// WhereLike answers Builder::whereLike.
//
// The clause carries the value that is bound rather than the pattern that was
// written, because a grammar may rewrite it -- SQLite spells a case sensitive
// like as a glob, whose wildcards are the other way round -- and the tenant
// scoping rebuilds the binding list from the clauses. A clause that did not
// carry its own value would lose it there.
func (b *Builder) WhereLike(column any, value any, caseSensitive bool, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	if grammar, ok := b.Grammar.(LikeBindingGrammar); ok {
		value = grammar.PrepareWhereLikeBinding(stringify(value), caseSensitive)
	}
	b.Wheres = append(b.Wheres, Where{
		Type:          "Like",
		Column:        column,
		Value:         value,
		CaseSensitive: caseSensitive,
		Boolean:       boolean,
		Not:           not,
	})
	b.AddBinding([]any{value}, "where")
	return b
}

// LikeBindingGrammar is the part of Illuminate\Database\Query\Grammars\Grammar
// that rewrites a like pattern for the engine. The PHP asks
// method_exists($this->grammar, 'prepareWhereLikeBinding'); this is the same
// question with a type.
type LikeBindingGrammar interface {
	// PrepareWhereLikeBinding answers SQLiteGrammar::prepareWhereLikeBinding.
	PrepareWhereLikeBinding(value string, caseSensitive bool) string
}

// OrWhereLike answers Builder::orWhereLike.
func (b *Builder) OrWhereLike(column any, value any, caseSensitive bool) *Builder {
	return b.WhereLike(column, value, caseSensitive, "or", false)
}

// WhereNotLike answers Builder::whereNotLike.
func (b *Builder) WhereNotLike(column any, value any, caseSensitive bool, boolean string) *Builder {
	return b.WhereLike(column, value, caseSensitive, boolean, true)
}

// OrWhereNotLike answers Builder::orWhereNotLike.
func (b *Builder) OrWhereNotLike(column any, value any, caseSensitive bool) *Builder {
	return b.WhereNotLike(column, value, caseSensitive, "or")
}

// WhereIntegerInRaw answers Builder::whereIntegerInRaw.
//
// The values are written into the statement rather than bound, which is why
// every one of them is cast to an integer first: an integer has no quoting to
// escape. A value that is not a number at all becomes zero rather than reaching
// the statement as itself.
func (b *Builder) WhereIntegerInRaw(column any, values []any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	integers := make([]any, 0, len(values))
	for _, value := range values {
		number, _ := castNumber(value)
		integers = append(integers, int64(number))
	}
	b.Wheres = append(b.Wheres, Where{
		Type: "InRaw", Column: column, Values: integers, Boolean: boolean, Not: not,
	})
	return b
}

// OrWhereIntegerInRaw answers Builder::orWhereIntegerInRaw.
func (b *Builder) OrWhereIntegerInRaw(column any, values []any) *Builder {
	return b.WhereIntegerInRaw(column, values, "or", false)
}

// WhereIntegerNotInRaw answers Builder::whereIntegerNotInRaw.
func (b *Builder) WhereIntegerNotInRaw(column any, values []any, boolean string) *Builder {
	return b.WhereIntegerInRaw(column, values, boolean, true)
}

// OrWhereIntegerNotInRaw answers Builder::orWhereIntegerNotInRaw.
func (b *Builder) OrWhereIntegerNotInRaw(column any, values []any) *Builder {
	return b.WhereIntegerNotInRaw(column, values, "or")
}

// WhereBetweenColumns answers Builder::whereBetweenColumns: a column between
// two other columns, so nothing here is bound.
func (b *Builder) WhereBetweenColumns(column any, values []any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	b.Wheres = append(b.Wheres, Where{
		Type: "betweenColumns", Column: column, Values: values, Boolean: boolean, Not: not,
	})
	return b
}

// OrWhereBetweenColumns answers Builder::orWhereBetweenColumns.
func (b *Builder) OrWhereBetweenColumns(column any, values []any) *Builder {
	return b.WhereBetweenColumns(column, values, "or", false)
}

// WhereNotBetweenColumns answers Builder::whereNotBetweenColumns.
func (b *Builder) WhereNotBetweenColumns(column any, values []any, boolean string) *Builder {
	return b.WhereBetweenColumns(column, values, boolean, true)
}

// OrWhereNotBetweenColumns answers Builder::orWhereNotBetweenColumns.
func (b *Builder) OrWhereNotBetweenColumns(column any, values []any) *Builder {
	return b.WhereNotBetweenColumns(column, values, "or")
}

// WhereValueBetween answers Builder::whereValueBetween: a value between two
// columns, which is the between comparison the other way round.
func (b *Builder) WhereValueBetween(value any, columns []any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	b.Wheres = append(b.Wheres, Where{
		Type: "valueBetween", Value: value, Columns: columns, Boolean: boolean, Not: not,
	})
	b.AddBinding([]any{value}, "where")
	return b
}

// OrWhereValueBetween answers Builder::orWhereValueBetween.
func (b *Builder) OrWhereValueBetween(value any, columns []any) *Builder {
	return b.WhereValueBetween(value, columns, "or", false)
}

// WhereValueNotBetween answers Builder::whereValueNotBetween.
func (b *Builder) WhereValueNotBetween(value any, columns []any, boolean string) *Builder {
	return b.WhereValueBetween(value, columns, boolean, true)
}

// OrWhereValueNotBetween answers Builder::orWhereValueNotBetween.
func (b *Builder) OrWhereValueNotBetween(value any, columns []any) *Builder {
	return b.WhereValueNotBetween(value, columns, "or")
}

// WhereDate answers Builder::whereDate: the date part of a timestamp column
// compared with a date.
//
// The operator is optional, as it is on Where: two arguments are a column and a
// value compared with "=". A time.Time is formatted to the day, which is what
// the PHP's format('Y-m-d') does.
func (b *Builder) WhereDate(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Date", column, "and", args...)
}

// OrWhereDate answers Builder::orWhereDate.
func (b *Builder) OrWhereDate(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Date", column, "or", args...)
}

// WhereTime answers Builder::whereTime.
func (b *Builder) WhereTime(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Time", column, "and", args...)
}

// OrWhereTime answers Builder::orWhereTime.
func (b *Builder) OrWhereTime(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Time", column, "or", args...)
}

// WhereDay answers Builder::whereDay.
func (b *Builder) WhereDay(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Day", column, "and", args...)
}

// OrWhereDay answers Builder::orWhereDay.
func (b *Builder) OrWhereDay(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Day", column, "or", args...)
}

// WhereMonth answers Builder::whereMonth.
func (b *Builder) WhereMonth(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Month", column, "and", args...)
}

// OrWhereMonth answers Builder::orWhereMonth.
func (b *Builder) OrWhereMonth(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Month", column, "or", args...)
}

// WhereYear answers Builder::whereYear.
func (b *Builder) WhereYear(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Year", column, "and", args...)
}

// OrWhereYear answers Builder::orWhereYear.
func (b *Builder) OrWhereYear(column any, args ...any) *Builder {
	return b.addDateBasedWhere("Year", column, "or", args...)
}

// addDateBasedWhere answers Builder::addDateBasedWhere, with the five methods'
// shared preamble folded in: the operator shortcut and the formatting of the
// value, which is the one line that differs between them.
func (b *Builder) addDateBasedWhere(typ string, column any, boolean string, args ...any) *Builder {
	operator, value := prepareValueAndOperator(args...)
	if len(args) > 1 {
		operator, value = b.shortcutOperator(operator, value)
	}
	value = formatDatePart(typ, flattenValue(value))

	b.Wheres = append(b.Wheres, Where{
		Type: typ, Column: column, Operator: operator, Value: value, Boolean: boolean,
	})
	if !IsExpression(value) {
		b.AddBinding([]any{value}, "where")
	}
	return b
}

// formatDatePart is the PHP's format('Y-m-d'), format('H:i:s'), format('d'),
// format('m') and format('Y'), plus the sprintf('%02d') the day and the month
// apply to a value that arrived as a number.
func formatDatePart(typ string, value any) any {
	if IsExpression(value) {
		return value
	}
	if moment, ok := value.(time.Time); ok {
		switch typ {
		case "Date":
			return moment.Format("2006-01-02")
		case "Time":
			return moment.Format("15:04:05")
		case "Day":
			return moment.Format("02")
		case "Month":
			return moment.Format("01")
		case "Year":
			return moment.Format("2006")
		}
		return value
	}
	if typ != "Day" && typ != "Month" {
		return value
	}
	if number, ok := castNumber(value); ok {
		return fmt.Sprintf("%02d", int64(number))
	}
	return value
}

// WhereRowValues answers Builder::whereRowValues: a tuple of columns compared
// with a tuple of values.
func (b *Builder) WhereRowValues(columns []any, operator string, values []any, boolean string) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	if len(columns) != len(values) {
		return b.refuse(boolean, fmt.Errorf(
			"query: the number of columns must match the number of values, got %d and %d",
			len(columns), len(values)))
	}
	b.Wheres = append(b.Wheres, Where{
		Type: "RowValues", Columns: columns, Operator: operator, Values: values, Boolean: boolean,
	})
	b.AddBinding(b.CleanBindings(values), "where")
	return b
}

// OrWhereRowValues answers Builder::orWhereRowValues.
func (b *Builder) OrWhereRowValues(columns []any, operator string, values []any) *Builder {
	return b.WhereRowValues(columns, operator, values, "or")
}

// WhereJSONContains answers Builder::whereJsonContains. The PHP spells the
// middle word Json; Go initialisms are upper case throughout.
func (b *Builder) WhereJSONContains(column any, value any, boolean string, not bool) *Builder {
	return b.addWhereJSON("JsonContains", column, value, boolean, not)
}

// OrWhereJSONContains answers Builder::orWhereJsonContains.
func (b *Builder) OrWhereJSONContains(column any, value any) *Builder {
	return b.WhereJSONContains(column, value, "or", false)
}

// WhereJSONDoesntContain answers Builder::whereJsonDoesntContain.
func (b *Builder) WhereJSONDoesntContain(column any, value any, boolean string) *Builder {
	return b.WhereJSONContains(column, value, boolean, true)
}

// OrWhereJSONDoesntContain answers Builder::orWhereJsonDoesntContain.
func (b *Builder) OrWhereJSONDoesntContain(column any, value any) *Builder {
	return b.WhereJSONDoesntContain(column, value, "or")
}

// WhereJSONOverlaps answers Builder::whereJsonOverlaps.
func (b *Builder) WhereJSONOverlaps(column any, value any, boolean string, not bool) *Builder {
	return b.addWhereJSON("JsonOverlaps", column, value, boolean, not)
}

// OrWhereJSONOverlaps answers Builder::orWhereJsonOverlaps.
func (b *Builder) OrWhereJSONOverlaps(column any, value any) *Builder {
	return b.WhereJSONOverlaps(column, value, "or", false)
}

// WhereJSONDoesntOverlap answers Builder::whereJsonDoesntOverlap.
func (b *Builder) WhereJSONDoesntOverlap(column any, value any, boolean string) *Builder {
	return b.WhereJSONOverlaps(column, value, boolean, true)
}

// OrWhereJSONDoesntOverlap answers Builder::orWhereJsonDoesntOverlap.
func (b *Builder) OrWhereJSONDoesntOverlap(column any, value any) *Builder {
	return b.WhereJSONDoesntOverlap(column, value, "or")
}

// addWhereJSON is the shared body of the contains and overlaps clauses: the
// value is bound as JSON, through the grammar when it has an opinion about how.
func (b *Builder) addWhereJSON(typ string, column any, value any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	if IsExpression(value) {
		b.Wheres = append(b.Wheres, Where{
			Type: typ, Column: column, Value: value, Boolean: boolean, Not: not,
		})
		return b
	}

	binding, err := prepareBindingForJSONContains(b.Grammar, value)
	if err != nil {
		return b.refuse(boolean, err)
	}
	b.Wheres = append(b.Wheres, Where{
		Type: typ, Column: column, Value: binding, Boolean: boolean, Not: not,
	})
	b.AddBinding([]any{binding}, "where")
	return b
}

// JSONContainsGrammar is the part of Illuminate\Database\Query\Grammars\Grammar
// that turns a value into the JSON binding its engine expects. It is asked for
// rather than declared on Grammar, for the reason InsertUsingGrammar is.
type JSONContainsGrammar interface {
	// PrepareBindingForJSONContains answers
	// Grammar::prepareBindingForJsonContains.
	PrepareBindingForJSONContains(binding any) (any, error)
}

// prepareBindingForJSONContains asks the grammar, and encodes the value itself
// when the grammar has nothing to say.
func prepareBindingForJSONContains(grammar Grammar, value any) (any, error) {
	if grammar, ok := grammar.(JSONContainsGrammar); ok {
		return grammar.PrepareBindingForJSONContains(value)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("query: the value of a JSON clause cannot be encoded: %w", err)
	}
	return string(encoded), nil
}

// WhereJSONContainsKey answers Builder::whereJsonContainsKey: the key is in the
// column's path, so there is nothing to bind.
func (b *Builder) WhereJSONContainsKey(column any, boolean string, not bool) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	b.Wheres = append(b.Wheres, Where{
		Type: "JsonContainsKey", Column: column, Boolean: boolean, Not: not,
	})
	return b
}

// OrWhereJSONContainsKey answers Builder::orWhereJsonContainsKey.
func (b *Builder) OrWhereJSONContainsKey(column any) *Builder {
	return b.WhereJSONContainsKey(column, "or", false)
}

// WhereJSONDoesntContainKey answers Builder::whereJsonDoesntContainKey.
func (b *Builder) WhereJSONDoesntContainKey(column any, boolean string) *Builder {
	return b.WhereJSONContainsKey(column, boolean, true)
}

// OrWhereJSONDoesntContainKey answers Builder::orWhereJsonDoesntContainKey.
func (b *Builder) OrWhereJSONDoesntContainKey(column any) *Builder {
	return b.WhereJSONDoesntContainKey(column, "or")
}

// WhereJSONLength answers Builder::whereJsonLength. The bound value is an
// integer: it is compared with a length.
func (b *Builder) WhereJSONLength(column any, args ...any) *Builder {
	return b.addWhereJSONLength(column, "and", args...)
}

// OrWhereJSONLength answers Builder::orWhereJsonLength.
func (b *Builder) OrWhereJSONLength(column any, args ...any) *Builder {
	return b.addWhereJSONLength(column, "or", args...)
}

func (b *Builder) addWhereJSONLength(column any, boolean string, args ...any) *Builder {
	operator, value := prepareValueAndOperator(args...)
	if len(args) > 1 {
		operator, value = b.shortcutOperator(operator, value)
	}

	b.Wheres = append(b.Wheres, Where{
		Type: "JsonLength", Column: column, Operator: operator, Value: value, Boolean: boolean,
	})
	if !IsExpression(value) {
		number, _ := castNumber(flattenValue(value))
		bound := int64(number)
		b.Wheres[len(b.Wheres)-1].Value = bound
		b.AddBinding([]any{bound}, "where")
	}
	return b
}

// WhereFullText answers Builder::whereFullText.
//
// options carries the engine's search modes -- MySQL reads "mode" and
// "expanded", Postgres reads "language" -- and a grammar that has none ignores
// it, as the PHP does.
func (b *Builder) WhereFullText(columns []any, value any, options map[string]any, boolean string) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	b.Wheres = append(b.Wheres, Where{
		Type: "Fulltext", Columns: columns, Value: value, Options: options, Boolean: boolean,
	})
	b.AddBinding([]any{value}, "where")
	return b
}

// OrWhereFullText answers Builder::orWhereFullText.
func (b *Builder) OrWhereFullText(columns []any, value any, options map[string]any) *Builder {
	return b.WhereFullText(columns, value, options, "or")
}

// WhereAll answers Builder::whereAll: every one of the columns compared with
// the same value, joined by "and" inside one group.
func (b *Builder) WhereAll(columns []any, args ...any) *Builder {
	return b.addWhereAcross(columns, "and", "and", args...)
}

// OrWhereAll answers Builder::orWhereAll.
func (b *Builder) OrWhereAll(columns []any, args ...any) *Builder {
	return b.addWhereAcross(columns, "or", "and", args...)
}

// WhereAny answers Builder::whereAny: any one of the columns, joined by "or".
func (b *Builder) WhereAny(columns []any, args ...any) *Builder {
	return b.addWhereAcross(columns, "and", "or", args...)
}

// OrWhereAny answers Builder::orWhereAny.
func (b *Builder) OrWhereAny(columns []any, args ...any) *Builder {
	return b.addWhereAcross(columns, "or", "or", args...)
}

// WhereNone answers Builder::whereNone: none of the columns, which the PHP
// spells as whereAny negated -- the group is joined by "and not".
func (b *Builder) WhereNone(columns []any, args ...any) *Builder {
	return b.addWhereAcross(columns, "and not", "or", args...)
}

// OrWhereNone answers Builder::orWhereNone.
func (b *Builder) OrWhereNone(columns []any, args ...any) *Builder {
	return b.addWhereAcross(columns, "or not", "or", args...)
}

// addWhereAcross is the shared body of whereAll, whereAny and whereNone: one
// nested group holding the same comparison over each column.
//
// The group is what makes the three of them safe next to a tenant filter --
// `tenant_id = ? and (name like ? or email like ?)` -- and it is the shape the
// PHP produces too, through whereNested.
func (b *Builder) addWhereAcross(columns []any, boolean, inner string, args ...any) *Builder {
	operator, value := prepareValueAndOperator(args...)
	return b.WhereNested(func(query *Builder) {
		for _, column := range columns {
			query.addWhere(inner, column, operator, value)
		}
	}, boolean)
}

// DynamicWhere answers Builder::dynamicWhere: the where clause spelled in a
// method name, as in whereNameAndEmail('taylor', 'taylor@laravel.com').
//
// PHP routes there from __call, which Go has no equivalent of, so the method is
// called by name and given the name it would have been called with. It is here
// because it is public there, and because a caller with a column list coming
// from configuration has the same string to turn into clauses.
func (b *Builder) DynamicWhere(method string, parameters []any) *Builder {
	finder := strings.TrimPrefix(method, "where")
	connector := "and"
	index := 0

	for _, segment := range splitDynamicSegments(finder) {
		if segment != "And" && segment != "Or" {
			if index < len(parameters) {
				b.addWhere(lower(connector), str.Snake(segment, "_"), "=", parameters[index])
			}
			index++
			continue
		}
		connector = segment
	}
	return b
}

// splitDynamicSegments answers the PHP's preg_split on /(And|Or)(?=[A-Z])/ with
// PREG_SPLIT_DELIM_CAPTURE: the column names and the connectors between them,
// in order.
func splitDynamicSegments(finder string) []string {
	segments := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(finder); i++ {
		for _, connector := range []string{"And", "Or"} {
			if !strings.HasPrefix(finder[i:], connector) {
				continue
			}
			next := i + len(connector)
			if next >= len(finder) || finder[next] < 'A' || finder[next] > 'Z' {
				continue
			}
			if i > start {
				segments = append(segments, finder[start:i])
			}
			segments = append(segments, connector)
			i = next - 1
			start = next
			break
		}
	}
	if start < len(finder) {
		segments = append(segments, finder[start:])
	}
	return segments
}

// WhereVectorDistanceLessThan answers Builder::whereVectorDistanceLessThan: the
// rows whose embedding is nearer than a distance to the vector given.
//
// The PHP also accepts a string there and turns it into an embedding through
// Str::of($text)->toEmbeddings(), which calls out to a model provider. That
// argument shape is not here: an embedding is fetched by the caller and passed
// in, because a query builder that makes a network call is a query builder that
// can time out.
//
// The PHP refuses on a connection that is not Postgres. Here the engine
// refuses, because the operator is Postgres's and a fluent method has nothing to
// report an error through.
func (b *Builder) WhereVectorDistanceLessThan(column any, vector []float64, maxDistance float64, boolean string) *Builder {
	if boolean == "" {
		boolean = "and"
	}
	encoded, err := json.Marshal(vector)
	if err != nil {
		return b.refuse(boolean, fmt.Errorf("query: the vector cannot be encoded: %w", err))
	}

	sql := "(" + b.wrapColumn(column) + " <=> ?) <= ?"
	bindings := []any{string(encoded), maxDistance}
	b.Wheres = append(b.Wheres, Where{Type: "Raw", SQL: sql, Boolean: boolean, Values: bindings})
	b.AddBinding(bindings, "where")
	return b
}

// OrWhereVectorDistanceLessThan answers
// Builder::orWhereVectorDistanceLessThan.
func (b *Builder) OrWhereVectorDistanceLessThan(column any, vector []float64, maxDistance float64) *Builder {
	return b.WhereVectorDistanceLessThan(column, vector, maxDistance, "or")
}

// WhereVectorSimilarTo answers Builder::whereVectorSimilarTo: the distance
// filter and the ordering that goes with it, given a similarity between 0 and 1.
func (b *Builder) WhereVectorSimilarTo(column any, vector []float64, minSimilarity float64, order bool) *Builder {
	b.WhereVectorDistanceLessThan(column, vector, 1-minSimilarity, "and")
	if order {
		b.OrderByVectorDistance(column, vector)
	}
	return b
}

// wrapColumn quotes a column through the grammar, and leaves it alone when
// there is no grammar to quote it with.
func (b *Builder) wrapColumn(column any) string {
	if b.Grammar == nil {
		return stringify(column)
	}
	return b.Grammar.Wrap(column)
}
