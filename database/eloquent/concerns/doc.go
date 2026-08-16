// Package concerns mirrors Illuminate\Database\Eloquent\Concerns: the traits a
// model is assembled from.
//
// It answers, from the clone at
// laravel_illuminate/database/Eloquent/Concerns:
//
//	GuardsAttributes.php            -> guardsattributes.go
//	HasAttributes.php               -> hasattributes.go, hasattributescasting.go
//	HasEvents.php                   -> hasevents.go
//	HasGlobalScopes.php             -> hasglobalscopes.go
//	HasRelationships.php            -> hasrelationships.go
//	HasTimestamps.php               -> hastimestamps.go
//	HasUlids.php                    -> hasuniqueids.go (HasULIDs)
//	HasUniqueIds.php                -> hasuniqueids.go
//	HasUniqueStringIds.php          -> hasuniqueids.go
//	HasUuids.php                    -> hasuniqueids.go (HasUUIDs)
//	HidesAttributes.php             -> hidesattributes.go
//	PreventsCircularRecursion.php   -> preventscircularrecursion.go
//	QueriesRelationships.php        -> queriesrelationships.go
//
// Two files come from one directory up, laravel_illuminate/database/Eloquent:
//
//	BroadcastsEvents.php                -> broadcastsevents.go
//	BroadcastableModelEventOccurred.php -> broadcastablemodeleventoccurred.go
//
// BroadcastsEvents is a trait, and every other trait of a model is here; the
// PHP keeps it out of Concerns/ for no reason it states. Its event class
// follows it because nothing else builds one, and Model::broadcastChannel,
// ::broadcastChannelRoute and ::withoutBroadcasting follow both, because they
// are the rest of that one feature and splitting a feature across two packages
// is how the two halves drift.
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
// The cast implementations. HasAttributes in the PHP is 2,584 lines, and most
// of them are the cast system -- dates, enums, encrypted, hashed, custom cast
// classes. Those belong in eloquent/casts, one implementation, and a copy here
// would be the second answer to what a column means. What is here is the
// declaration side the PHP keeps in this trait -- getCasts, hasCast,
// mergeCasts and the four encoders fromJson, fromFloat, fromDateTime and
// fromEncryptedString -- in hasattributescasting.go.
//
// # What is skipped, and why (ADR 0056)
//
//   - HasAttributes::getMutatorMethods and getAttributeMarkedMutatorMethods.
//     Both read a class's method list with get_class_methods and match it
//     against a regular expression. Go cannot list a type's methods by name
//     and read their return types, so the model registers each accessor with
//     SetAttributeMutator instead. Motive (1), a PHP language feature.
//   - HasEvents::bootHasEvents and resolveObserveAttributes,
//     HasGlobalScopes::bootHasGlobalScopes and resolveGlobalScopeAttributes.
//     All four read PHP attributes -- #[ObservedBy], #[ScopedBy] -- off the
//     class with ReflectionClass, and hang them on the trait boot hook that
//     Model::bootIfNotBooted calls by name. Go has neither class attributes
//     nor a boot hook found by method name; a model here calls Observe and
//     AddGlobalScope where a reader can see it. Motive (1).
//   - HasUniqueStringIds::initializeHasUniqueStringIds. Its whole body sets
//     the $usesUniqueIds property to true. Here that property is the Generator
//     field being non-nil, which HasUUIDs and HasULIDs set, so there is
//     nothing left for an initializer to do. Motive (1).
//   - The whole of TransformsToResource: toResource, guessResource,
//     guessResourceName and resolveResourceFromAttribute. All four build a
//     class name out of the model's namespace and ask class_exists, or read a
//     #[UseResource] attribute off the class. Go resolves no type from a name
//     in a string, so a resource is a value a handler names. Motive (1).
//   - HasRelationships::autoloadRelationsUsing, hasRelationAutoloadCallback,
//     attemptToAutoloadRelation and withRelationshipAutoloading. They are
//     automatic eager loading: reading an unloaded relation runs a query
//     behind the caller. That is the lazy load this framework does not have,
//     and a query with no auth.Grant on it breaks RULE 17 outright.
//     GetRelationValue answers what is loaded and nothing else.
//   - HasRelationships::through. It returns a PendingHasThroughRelationship
//     built from a relation named by a string, which the PHP calls as a
//     method. HasOneThrough and HasManyThrough are the two relations it can
//     end at, and both are here already, spelled with the models rather than
//     with their method names. Motive (1).
//
// # The tenant is in every channel, and it is added once
//
// Nothing in Illuminate prefixes a channel by tenant, and RULE 14 requires it:
// "private-App.Models.Order.17" without one is a single channel shared by every
// customer holding an order 17, and the first subscriber reads the others'
// events. [BroadcastsEvents.BroadcastChannel] and
// [BroadcastsEvents.BroadcastChannelRoute] answer the tenant-free name and
// pattern, exactly as Model::broadcastChannel does, and
// broadcasting.TenantChannel puts the tenant in front -- once, on the way into a
// driver and again on the way out of the authentication endpoint, so the name
// published and the name authorized cannot disagree (RULE 9). The tenant comes
// from the auth.Grant a channel Policy produced and never from the channel the
// client asked for, which is why a client asking for "private-orders.17" is
// authorized for "acme:private-orders.17" and never gets to choose the "acme".
package concerns
