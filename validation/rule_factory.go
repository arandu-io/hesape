package validation

import (
	"context"

	"github.com/arandu-io/hesape/auth"
)

// RuleFactory answers to Illuminate\Validation\Rule: the static class whose
// methods build the fluent rule objects.
//
// It is not spelled Rule because that name answers to
// Illuminate\Contracts\Validation\Rule, the interface a custom rule implements
// (ADR 0044: two PHP names, one Go namespace, and the contract took the shorter
// one). Go has no static method either, so the factory is an empty struct and
// they are methods on it, which is what keeps each of them at the PHP's name:
//
//	validation.Rules{
//		"role":  validation.RuleFactory{}.In("admin", "member").String(),
//		"email": "required|email|" + validation.RuleFactory{}.Unique("users").String(),
//	}
//
// Each method is the PHP's, and each returns the same builder NewX does: the
// factory is a second way of NAMING a constructor, not a second constructor.
type RuleFactory struct{}

// In answers to Rule::in.
func (RuleFactory) In(values ...string) *In { return NewIn(values...) }

// NotIn answers to Rule::notIn.
func (RuleFactory) NotIn(values ...string) *NotIn { return NewNotIn(values...) }

// Array answers to Rule::array.
func (RuleFactory) Array(keys ...string) *ArrayRule { return NewArrayRule(keys...) }

// Date answers to Rule::date.
func (RuleFactory) Date() *Date { return NewDate() }

// Numeric answers to Rule::numeric.
func (RuleFactory) Numeric() *Numeric { return NewNumeric() }

// Dimensions answers to Rule::dimensions.
func (RuleFactory) Dimensions() *Dimensions { return NewDimensions() }

// Enum answers to Rule::enum.
//
// The PHP names a backed enum class and asks it tryFrom; Go has no enum type to
// ask, so the cases are given -- a Go program spells an enum as a named string
// type with a list of values, and that list is what this takes.
func (RuleFactory) Enum(cases ...string) *Enum { return NewEnum(cases...) }

// Exists answers to Rule::exists.
func (RuleFactory) Exists(table string, column ...string) *Exists {
	return NewExists(table, column...)
}

// Unique answers to Rule::unique.
func (RuleFactory) Unique(table string, column ...string) *Unique {
	return NewUnique(table, column...)
}

// File answers to Rule::file.
func (RuleFactory) File() *FileRule { return NewFileRule() }

// ImageFile answers to Rule::imageFile.
func (RuleFactory) ImageFile(allowSvg ...bool) *FileRule { return NewImageFile(allowSvg...) }

// Image answers to File::image, which the PHP declares static on the rule.
func (RuleFactory) Image(allowSvg ...bool) *FileRule { return NewImageFile(allowSvg...) }

// Default answers to File::default: the file rule an application uses
// everywhere unless it says otherwise.
//
// The PHP keeps it in the container and reads it back from there (ADR 0002
// refuses that), so what it returns here is a plain builder and the application
// keeps the one it made.
func (RuleFactory) Default() *FileRule { return NewFileRule() }

// Defaults answers to File::defaults, with the same change Default carries: the
// rules are applied to a new builder rather than stored in a container.
func (RuleFactory) Defaults(rules ...string) *FileRule { return NewFileRule().Rules(rules...) }

// Email answers to Rule::email.
func (RuleFactory) Email() *EmailRule { return NewEmailRule() }

// Password answers to Rule::password.
func (RuleFactory) Password(min int) *Password { return NewPassword(min) }

// ExcludeIf answers to Rule::excludeIf.
func (RuleFactory) ExcludeIf(condition bool) *ExcludeIf { return NewExcludeIf(condition) }

// ProhibitedIf answers to Rule::prohibitedIf.
func (RuleFactory) ProhibitedIf(condition bool) *ProhibitedIf { return NewProhibitedIf(condition) }

// RequiredIf answers to Rule::requiredIf.
func (RuleFactory) RequiredIf(condition bool) *RequiredIf { return NewRequiredIf(condition) }

// AnyOf answers to Rule::anyOf: the value has to satisfy one of the rule sets.
func (RuleFactory) AnyOf(sets ...*Set) *AnyOf { return NewAnyOf(sets...) }

// Can answers to Rule::can: the rule passes when the subject the Grant was
// issued to is allowed the ability against the value.
func (RuleFactory) Can(allows func(g auth.Grant, ability string, arguments []string, value any) bool, ability string, arguments ...string) *Can {
	return NewCan(allows, ability, arguments...)
}

// ---------------------------------------------------------------------------
// The presence verifier.
// ---------------------------------------------------------------------------

// PresenceQuery is the one question `unique` and `exists` ask of a store: how
// many rows of the collection hold any of the values in the column, once the
// Grant's tenant and the extra conditions are applied.
//
// It is a function rather than a repository because building the query needs
// hesape/database, and this package does not import it: the application passes
// the query in, already written against whatever it stores rows in.
type PresenceQuery func(ctx context.Context, g auth.Grant, collection, column string, values []any,
	excludeID any, idColumn string, extra map[string]string) (int, error)

// DatabasePresenceVerifier answers to
// Illuminate\Validation\DatabasePresenceVerifier: the PresenceVerifier over a
// relational store.
//
// The PHP holds a connection resolver and builds the query itself. Here the
// query is given -- see PresenceQuery -- and what is left is the two counts and
// the connection name, which is the whole of the class that is not query
// building.
type DatabasePresenceVerifier struct {
	query      PresenceQuery
	connection string
}

// NewDatabasePresenceVerifier answers to the DatabasePresenceVerifier
// constructor.
func NewDatabasePresenceVerifier(query PresenceQuery) *DatabasePresenceVerifier {
	return &DatabasePresenceVerifier{query: query}
}

// SetConnection answers to DatabasePresenceVerifier::setConnection.
func (d *DatabasePresenceVerifier) SetConnection(connection string) {
	d.connection = connection
}

// Connection reports the connection SetConnection named, which is what the
// query is expected to run on.
func (d *DatabasePresenceVerifier) Connection() string { return d.connection }

// GetCount answers to DatabasePresenceVerifier::getCount.
func (d *DatabasePresenceVerifier) GetCount(ctx context.Context, g auth.Grant, collection, column string,
	value any, excludeID any, idColumn string, extra map[string]string) (int, error) {
	if d.query == nil {
		return 0, errNoPresenceQuery
	}
	return d.query(ctx, g, collection, column, []any{value}, excludeID, idColumn, extra)
}

// GetMultiCount answers to DatabasePresenceVerifier::getMultiCount.
func (d *DatabasePresenceVerifier) GetMultiCount(ctx context.Context, g auth.Grant, collection, column string,
	values []any, extra map[string]string) (int, error) {
	if d.query == nil {
		return 0, errNoPresenceQuery
	}
	return d.query(ctx, g, collection, column, values, nil, "", extra)
}

// errNoPresenceQuery is what a verifier with no query answers. It is an error
// rather than a zero count, because a zero count reads as "nothing is there"
// and would make `unique` pass on a row that exists.
var errNoPresenceQuery = presenceError("validation: the presence verifier was built with no query")

type presenceError string

func (e presenceError) Error() string { return string(e) }
