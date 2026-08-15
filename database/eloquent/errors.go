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

// ErrRelationNotFound is returned when a query names a relation the model never
// registered: an eager load of "author" on a model that declares no author, a
// nested path whose second segment does not exist on the model the first
// segment reaches, or a route binding through a relation that is not there.
//
// Wrapping it always carries the name and the table, because the useful part of
// the message is which relation was asked for on which model. It is a
// programming mistake rather than a runtime condition -- the fix is the
// declaration, not a retry -- so callers usually let it travel rather than
// matching on it.
//
// Answers Illuminate\Database\Eloquent\RelationNotFoundException.
var ErrRelationNotFound = errors.New("eloquent: call to undefined relationship")

// ErrNamedScopeNotFound is what CallNamedScope reports for a scope the model
// never registered.
//
// PHP raises BadMethodCallException from __call, because there a scope is a
// method and the miss is a missing method. Here a scope is an entry in
// Model.NamedScopes, so the miss is a missing key.
var ErrNamedScopeNotFound = errors.New("eloquent: call to undefined named scope")

// ModelNotFoundError carries what ModelNotFoundException records beyond its
// message: the model that was looked for and the ids it was looked for by.
//
// It unwraps to ErrModelNotFound, so errors.Is keeps working and a caller that
// only wants the 404 never has to name this type. A caller that wants the ids --
// a log line, a retry that drops the missing rows -- reaches them with
// errors.As.
type ModelNotFoundError struct {
	// Model is the model the query was on. The PHP holds the class name; here it
	// is the table, which is what identifies a row on this side.
	Model string

	// IDs is what ModelNotFoundException::$ids holds.
	IDs []any
}

// Error formats the message ModelNotFoundException::setModel builds.
func (e *ModelNotFoundError) Error() string {
	if len(e.IDs) == 0 {
		return fmt.Sprintf("%s: %s", ErrModelNotFound, e.Model)
	}
	return fmt.Sprintf("%s: %s %v", ErrModelNotFound, e.Model, e.IDs)
}

// Unwrap makes errors.Is(err, ErrModelNotFound) true. See ErrModelNotFound for
// why the sentinel is what callers compare against.
func (e *ModelNotFoundError) Unwrap() error { return ErrModelNotFound }

// SetModel answers ModelNotFoundException::setModel.
func (e *ModelNotFoundError) SetModel(model string, ids ...any) *ModelNotFoundError {
	e.Model = model
	e.IDs = ids
	return e
}

// GetModel answers ModelNotFoundException::getModel.
func (e *ModelNotFoundError) GetModel() string { return e.Model }

// GetIDs answers ModelNotFoundException::getIds. The PHP spells it getIds.
func (e *ModelNotFoundError) GetIDs() []any { return e.IDs }

// modelNotFound builds the wrapped ErrModelNotFound with the table and ids, the
// way ModelNotFoundException::setModel formats its message.
func modelNotFound(table string, ids ...any) error {
	return &ModelNotFoundError{Model: table, IDs: ids}
}

// ErrJSONEncoding is the sentinel every failure to turn a model, an attribute
// or a resource into JSON wraps.
//
// The wrapping is done by ForModel, ForAttribute and the resource helper below,
// each of which adds what encoding failed and on which row -- a cast that
// produced a value encoding/json refuses, an attribute holding something with
// no JSON form. Callers match on this with errors.Is when they want to answer
// one status for any encoding failure; the message says which row to look at.
//
// Answers Illuminate\Database\Eloquent\JsonEncodingException. The PHP spells it
// JsonEncodingException.
var ErrJSONEncoding = errors.New("eloquent: json encoding failed")

// ForModel answers JsonEncodingException::forModel.
//
// The PHP static is a package function, because Go has no static and an error
// value has no class to hang one on. It names the model by its table and key,
// where the PHP names it by class and key: the class of a Model[T] is its type
// parameter, and the table is what a reader of the log can look in.
func ForModel(table string, key any, message string) error {
	return fmt.Errorf("%w: model [%s] with id [%v]: %s", ErrJSONEncoding, table, key, message)
}

// ForAttribute answers JsonEncodingException::forAttribute.
func ForAttribute(table, key, message string) error {
	return fmt.Errorf("%w: attribute [%s] of model [%s]: %s", ErrJSONEncoding, key, table, message)
}

// ForResource answers JsonEncodingException::forResource.
//
// The PHP takes the JsonResource and reads the model off it. A resource here is
// a value an http handler names, and http/resources does not reach into this
// package, so the resource arrives as its own name.
func ForResource(resource, table string, key any, message string) error {
	return fmt.Errorf("%w: resource [%s] with model [%s] with id [%v]: %s", ErrJSONEncoding, resource, table, key, message)
}
