// Package attributes mirrors Illuminate\Database\Eloquent\Attributes.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Eloquent/Attributes:
//
//	Appends.php
//	Boot.php
//	CollectedBy.php
//	Connection.php
//	Fillable.php
//	Guarded.php
//	Hidden.php
//	Initialize.php
//	ObservedBy.php
//	Scope.php
//	ScopedBy.php
//	Table.php
//	Touches.php
//	Unguarded.php
//	UseEloquentBuilder.php
//	UseFactory.php
//	UsePolicy.php
//	UseResource.php
//	UseResourceCollection.php
//	Visible.php
//
// Nothing is implemented here yet. docs/31-reorganizacao-hesape.md says what
// moves in, from where, and in which phase.
//
// # Why the coverage count does not move here (ADR 0044)
//
// None of the twenty declares a public method. Each is a PHP attribute -- a
// constructor and a readonly property, written above a class as #[Table('x')]
// and read back with ReflectionClass::getAttributes. The declaration is the
// whole of it, and what reads it is the trait boot hook: ObservedBy is read by
// HasEvents::bootHasEvents, ScopedBy by HasGlobalScopes::bootHasGlobalScopes,
// UseResource by TransformsToResource, and so on -- all of which are skipped
// in eloquent/concerns for the same reason, motive (1).
//
// Go has no class attribute and no reflection over one. What a model declares
// here it declares in Go instead, where a reader can see it: a struct tag on
// the entity field, or a call in the constructor -- Observe, AddGlobalScope,
// SetTable, Guard. That is one way to say it rather than two (RULE 9).
package attributes
