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
// Nothing is implemented here, and nothing will be. These are PHP 8 attributes:
// annotations a class carries, which the framework reads back by reflection to
// decide what to do with it.
//
// Go has struct tags and nothing else of the kind, and reading behaviour out of
// them is the mechanism this framework's thesis rejects -- what decides is the
// type, checked by the compiler. What an attribute configures in Illuminate is a
// field or an argument here.
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
