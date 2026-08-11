// Package concerns mirrors Illuminate\Database\Eloquent\Concerns: the traits a
// model is assembled from.
//
// It answers, from the clone at
// laravel_illuminate/database/Eloquent/Concerns:
//
//	GuardsAttributes.php            -> guardsattributes.go
//	HasAttributes.php               -> hasattributes.go
//	HasEvents.php                   -> hasevents.go
//	HasGlobalScopes.php             -> hasglobalscopes.go
//	HasRelationships.php            -> hasrelationships.go
//	HasTimestamps.php               -> hastimestamps.go
//	HasUlids.php                    -> hasuniqueids.go (HasULIDs)
//	HasUniqueIds.php                -> hasuniqueids.go
//	HasUuids.php                    -> hasuniqueids.go (HasUUIDs)
//	HidesAttributes.php             -> hidesattributes.go
//	PreventsCircularRecursion.php   -> preventscircularrecursion.go
//	QueriesRelationships.php        -> queriesrelationships.go
//
// # A trait is a struct, and a static is a package function
//
// Each trait is a struct a model embeds, which gives it the fields and the
// methods the same way `use` does. What could not stay a method is the static:
// PHP resolves Model::unguarded() and Model::withoutEvents() through late
// static binding, which Go does not have, so those are package functions --
// Unguard, Unguarded, WithoutEvents, WithoutTimestamps -- and the global they
// guard is a package variable behind a mutex rather than a static property.
//
// The trait that reads $this is the other shape: HasTimestamps cannot reach the
// model it was embedded in, so UpdateTimestamps takes the attribute bag. That
// is mechanical, and it is written on each method that does it.
//
// # What is not here
//
// Casting. HasAttributes in the PHP is 2,584 lines, and most of them are the
// cast system -- dates, enums, encrypted, hashed, custom cast classes. That
// belongs in eloquent/casts, one implementation, and a copy here would be the
// second answer to what a column means.
package concerns
