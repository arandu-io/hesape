package eloquent

import (
	"errors"
	"fmt"
)

// ErrModelNotFound answers Illuminate\Database\Eloquent\ModelNotFoundException.
//
// The PHP throws; Go returns it, wrapped with the table and the ids that were
// looked for, so errors.Is keeps working and the message still says which row:
//
//	fmt.Errorf("%w: users [7]", ErrModelNotFound)
//
// It is a sentinel and not a type for the reason database.ErrNotFound is one:
// the exception classifier turns it into a 404, and a caller comparing against
// it should not have to name a struct.
var ErrModelNotFound = errors.New("eloquent: no query results for model")

// ErrMultipleRecordsFound answers
// Illuminate\Database\MultipleRecordsFoundException, which Sole throws when the
// query matched more than one row.
var ErrMultipleRecordsFound = errors.New("eloquent: multiple records found")

// ErrNoTenant is what every executing method returns when the Grant carries no
// tenant.
//
// Illuminate has no equivalent, because Illuminate has no tenant. It is RULE 17
// made into a value: the zero Grant is the only one constructible outside the
// auth package, auth.Tenant reads the empty string off it, and a query with no
// tenant in its where clause reads every customer of the system. So it does not
// run.
var ErrNoTenant = errors.New("eloquent: the grant carries no tenant, and a query without one reads every tenant (call auth.Authorize, or auth.SystemGrant with the tenant this work belongs to)")

// ErrNoKey answers the LogicException Model::delete throws when the model has no
// primary key defined.
var ErrNoKey = errors.New("eloquent: no primary key defined on model")

// ErrEmptyCollection answers the LogicException Collection::toQuery throws for
// an empty collection: there is no model to take the table from.
var ErrEmptyCollection = errors.New("eloquent: unable to create query for empty collection")

// ErrRelationNotFound answers
// Illuminate\Database\Eloquent\RelationNotFoundException.
var ErrRelationNotFound = errors.New("eloquent: call to undefined relationship")

// modelNotFound builds the wrapped ErrModelNotFound with the table and ids, the
// way ModelNotFoundException::setModel formats its message.
func modelNotFound(table string, ids ...any) error {
	if len(ids) == 0 {
		return fmt.Errorf("%w: %s", ErrModelNotFound, table)
	}
	return fmt.Errorf("%w: %s %v", ErrModelNotFound, table, ids)
}
