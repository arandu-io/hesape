// Package relations mirrors Illuminate\Database\Eloquent\Relations: the sixteen
// relation types, and the eager loading that keeps them from being N+1 queries.
//
// The source is the clone at laravel_illuminate/database/Eloquent/Relations,
// read as PHP rather than as documentation -- the bodies decide, because a
// method with the right name and the wrong behaviour is worse than a missing
// one: nobody checks it.
//
//	Relation.php            -> relation.go, morphmap.go
//	HasOneOrMany.php        -> hasoneormany.go
//	HasOne.php              -> hasone.go
//	HasMany.php             -> hasmany.go
//	BelongsTo.php           -> belongsto.go
//	BelongsToMany.php       -> belongstomany.go
//	MorphOneOrMany.php      -> morphoneormany.go
//	MorphOne.php            -> morphone.go
//	MorphMany.php           -> morphmany.go
//	MorphTo.php             -> morphto.go
//	MorphToMany.php         -> morphtomany.go
//	HasOneOrManyThrough.php -> hasoneormanythrough.go
//	HasOneThrough.php       -> hasoneormanythrough.go
//	HasManyThrough.php      -> hasoneormanythrough.go
//	Pivot.php               -> pivot.go
//	MorphPivot.php          -> pivot.go
//
// The thirteen walking methods that BelongsToMany and HasOneOrManyThrough each
// declare -- chunk, chunkById, chunkByIdDesc, orderedChunkById, each, eachById,
// lazy, lazyById, lazyByIdDesc, cursor, paginate, simplePaginate and
// cursorPaginate -- have one body between them, in chunking.go. The two PHP
// classes write them out twice because they sit on opposite branches of the
// hierarchy and share no parent that could hold them; the only line that
// differs is BelongsToMany hydrating the pivot on each page, which is a field
// of the shared body rather than a second copy of it.
//
// # The four methods
//
// AddEagerConstraints, InitRelation, Match and GetEager are what turn a hundred
// queries into two. One query fetches the parents; AddEagerConstraints widens
// the relation's where into a single `in (...)` over every parent key;
// InitRelation seeds each parent so that one with no children answers empty
// instead of going back to the database; Match buckets the flat result set by
// key and hands each parent its own. EagerLoadRelation is the loop, and the
// test for it counts the statements the connection was asked for -- because the
// values come out right either way, which is why an N+1 passes review.
//
// # Every read takes the Grant, and every read filters by tenant
//
// RULE 17 has no read half that is optional. GetResults, Get, First, GetEager,
// Attach, Detach and Sync all take a context and an auth.Grant, and every
// statement is narrowed to auth.Tenant(g) before it leaves -- on the eager path
// as much as the lazy one. The eager path is where it matters most: a with()
// whose parent query is correctly scoped and whose child query is not returns
// the right parents carrying another customer's children, and every row on the
// screen looks like it belongs there. A Grant carrying no tenant is refused
// rather than compiled into `tenant_id = ”`.
//
// The query builder decides no such thing -- it builds SQL. What is enforced
// here is that a relation cannot be executed without the Grant that authorized
// it, because the Grant is in the signature.
//
// # The morph map is mandatory here, and that is the trade being bought
//
// In PHP the *_type column can hold a class name, and Eloquent instantiates it.
// There is no class name at run time in Go, so the column holds an alias
// registered with MorphMap and the type it names can be renamed, moved or split
// without a single stored row becoming unreadable. Laravel recommends the map
// for exactly this reason; here it is the mechanism rather than the advice, and
// an unregistered alias is an error that says which alias and what is
// registered.
//
// # What Go changed, and what it did not
//
// The names are Illuminate's, capitalized (ADR 0044): Where, OrderBy, Attach,
// Detach, Sync, SyncWithoutDetaching, WithPivot, WithTimestamps, As. The
// mechanical changes are three, and each is written where it happens: a method
// that throws returns (T, error); an initialism is upper case (parseIds ->
// ParseIDs, laravel_through_key -> ThroughKey); and a PHP trait becomes a
// struct to embed whose abstract methods arrive as function fields.
//
// The one shape that had to move is virtual dispatch. The PHP constructor ends
// with $this->addConstraints() and late binding sends it to the subclass; Go
// embedding does not, so each concrete constructor calls its own AddConstraints
// as its last statement, and an override the parent needs to reach -- the
// aliased pivot columns, the one-of-many relation query -- is a field the
// subtype sets rather than a method it redeclares.
package relations
