// Package eloquent answers Illuminate\Database\Eloquent: the Model, its query
// Builder, its Collection and soft deletes.
//
// The source is the clone at laravel_illuminate/database/Eloquent (illuminate
// 13.x, "illuminate/support": "^13.0"), read method body by method body. Where a
// body could not be carried over, the doc comment on the Go method says so and
// says why.
//
// # There is no $model->name, and that is the whole design
//
// PHP has dynamic properties, so Illuminate keeps a row in an array and lets
// $user->name reach into it. Go has none. A model here is the application's own
// struct, and the Eloquent machinery works over it with a type parameter:
//
//	type User struct {
//		ID        int64      `db:"id"`
//		Name      string     `db:"name"`
//		Email     string     `db:"email"`
//		CreatedAt time.Time  `db:"created_at"`
//		UpdatedAt time.Time  `db:"updated_at"`
//		DeletedAt *time.Time `db:"deleted_at"`
//	}
//
//	users := eloquent.NewModel[User]("users", conn, grammar, processor)
//	users.SoftDeletes = true
//
//	found, err := users.NewQuery().Where("email", email).First(g)
//	if err != nil {
//		return err
//	}
//	fmt.Println(found.Entity.Name) // a struct field, checked by the compiler
//
// A column is a field. The tag `db:"..."` names it; without a tag the name is
// the field name in snake case, which is Illuminate's own convention. A field
// tagged `db:"-"` is not a column.
//
// # $fillable and $guarded do not exist here, and nothing replaces them
//
// They are the first thing a Laravel developer looks for, so: 00-meta/DOC-architecture.md
// rejected both, and the answer is the initial letter of the field.
//
// Mass assignment is dangerous in PHP because every property is reachable and
// the danger is discovered at runtime; $fillable is a runtime allowlist bolted
// on top. In Go the allowlist already exists and the compiler enforces it --
// reflection cannot write an unexported field, from any package, ever:
//
//	type User struct {
//		Name  string `db:"name"`  // Fill sets this
//		admin bool   `db:"admin"` // nothing outside the package can, at all
//	}
//
// So Fill writes the exported fields it finds and drops keys it does not know,
// which is what fill() does with a key outside $fillable. ForceFill answers
// forceFill: it keeps the unknown keys as raw attributes, which is what
// unguarded mass assignment does in PHP -- a key with no property behind it
// still lands in $attributes. Neither can reach an unexported field.
//
// The cost is stated plainly: an unexported field is not a column at all, so it
// is not persisted either. A value that must be stored but must never come from
// a request is an exported field the caller does not put in the map -- in this
// framework a request never becomes a model, it becomes a validated struct
// first.
//
// # Every read carries the Grant (RULE 17)
//
// All, Find, First, Get, Value, Pluck, Paginate, Chunk and Cursor take an
// auth.Grant and filter by auth.Tenant(g), exactly as Insert, Update and Delete
// do. There is no read path without authorization: a query builder reached
// without a Grant compiles SQL and cannot run it.
//
// The tenant filter is on by default and comes off only by naming it: a model
// whose table has no tenant column sets TenantColumn to the empty string, in
// its constructor, where a reader sees it. A Grant carrying no tenant -- the
// zero Grant, which is the only one constructible outside the auth package --
// is refused with ErrNoTenant before any SQL is built.
//
// The tenant written on insert and matched on select is always auth.Tenant(g).
// A value the caller put in the struct for that column is overwritten, because
// RULE 14 says the tenant comes from the Grant and from nowhere else.
//
// # What the signatures do NOT carry, and why
//
// There is no context.Context. The connection contract in this component is
// query.Connection, which mirrors Illuminate's and takes none; declaring a
// second, context-carrying connection interface here would be a second way to
// reach the database, which RULE 9 rules out. A signature that accepted a
// context it could not pass on would be worse than one that does not accept it.
//
// # Names
//
// The names are Illuminate's, capitalised (ADR 0044). The mechanical changes,
// each stated on the method that makes it:
//
//   - what PHP throws, Go returns: (T, error). FindOrFail answers
//     ModelNotFoundException with ErrModelNotFound.
//   - what PHP spells with a lower-case initialism, Go spells in caps: toJson
//     is ToJSON, insertGetId is InsertGetID.
//   - a static method is a method on the model the application constructed. Go
//     generic types have no statics, so the prototype instance plays the part of
//     the class: users.All(g), not User::all().
//   - a property and a method of the same name cannot coexist, so the property
//     is a field and the method keeps the verb: Model.Table is $table and
//     GetTable is getTable().
//
// # Column order on insert
//
// Values reach the grammar as a map, and a Go map has no order. Illuminate has
// the same problem for a batch insert and answers it with ksort, so the same
// answer is used for every insert here: columns and their bindings go in sorted
// order. The grammar must sort identically -- both sides sort by column name,
// which is the only ordering either side can derive from the values alone.
//
// # What is not here
//
// Relations live in eloquent/relations and reach this package through the
// Relation interface declared in relation.go, which is declared here for the
// reason query.Connection is declared in query: in Go the interface belongs with
// its consumer, and relations imports this package for Builder.
//
// # What is skipped, and why (ADR 0056)
//
// Every name below exists in the clone and does not exist here. Three motives
// allow that -- a PHP language feature Go does not have, surface that only
// serves the container or a service provider, and a driver this ecosystem does
// not carry -- and each line says which.
//
//   - Model::setConnectionResolver and unsetConnectionResolver. Both write the
//     static resolver a model looks its connection up in. There is no resolver
//     and no container (ADR 0001): the connection is handed to NewModel and
//     copied onto every instance, and SetConnection takes it alongside the
//     name. Motive (2).
//   - Model::clearBootedModels. Its body empties $booted, $bootedCallbacks,
//     $classAttributes and $globalScopes -- the four statics that exist because
//     a PHP class boots once per process. A Go value has no boot: the global
//     scopes are a field on the model, and clearing them is building another
//     model. Motive (1).
//   - Model::initializeModelAttributes. Its body reads the #[Table] and
//     #[Connection] PHP attributes off the class with reflection and fills in
//     $table, $connection, $primaryKey, $keyType and $incrementing. Go has no
//     class attributes, and those five are fields NewModel sets where a reader
//     sees them. Motive (1).
//   - Model::escapeWhenCastingToString. It decides whether __toString escapes
//     for HTML, which exists because Blade interpolates an object by calling
//     __toString on it. Go has no __toString and the view layer takes typed
//     values. Motive (1).
//   - Model::automaticallyEagerLoadRelationships and
//     isAutomaticallyEagerLoadingRelationships, Collection::loadMissingRelationshipChain
//     and withRelationshipAutoloading. All four are automatic eager loading:
//     reading an unloaded relation runs a query behind the caller. That query
//     carries no auth.Grant, which breaks RULE 17 outright, and eloquent/concerns
//     refused the same four members of HasRelationships for the same reason.
//     PreventLazyLoading is what is left of the pair, and its doc comment says
//     what it means here.
//   - Model::broadcastChannel, broadcastChannelRoute and withoutBroadcasting,
//     and the whole of BroadcastsEvents, BroadcastsEventsAfterCommit and
//     BroadcastableModelEventOccurred. They dispatch model events onto a
//     broadcast connection resolved out of the container, and the channel name
//     is built from the model's class name and namespace. Broadcasting is its
//     own component; a model that must announce itself does it from the handler
//     that saved it, where the reader can see it happen. Motive (2).
//   - ModelInspector::inspect. It builds the `model:show` output by reflecting
//     over a class found from a string, listing its relation methods and its
//     casts. Neither lookup exists in Go. Motive (1).
//   - Prunable::prunable, prune and pruneAll, and MassPrunable::prunable and
//     pruneAll. The three-line bodies are the easy half; what makes them work is
//     `model:prune`, which scans the application's files for classes using the
//     trait and instantiates each by name. Go resolves no type from a file, and
//     a prune that must be registered by hand is the delete the application
//     already writes. Motive (2).
//   - Builder::getMacro, hasMacro, getGlobalMacro and hasGlobalMacro. Macros are
//     methods added to a class at run time and dispatched through __call, which
//     Go has no form of. The decision, with what to write instead, is in
//     macroable/doc.go. Motive (1).
//   - Builder::withCasts. Its body is $model->mergeCasts($casts): a per-query
//     override of what a column means. A column here is a struct field, and its
//     type is the cast -- there is no map to merge into, and a query that
//     changed a field's type would not compile against the entity it hydrates.
//     Motive (1).
//   - Collection::collapse. It is an #[\Override] that narrows the return type
//     from static to Support\Collection, and its body is toBase()->collapse().
//     Arr::collapse merges the items that are themselves lists, and a collection
//     of models has none, so the PHP answers an empty collection for every input
//     this method can receive. A Go method that can only ever return nothing is
//     not the same method. The narrowing the override existed for is the
//     compiler's here: ToBase, Flatten, Pad, Partition and Zip all say in their
//     signature what they hand back. Motive (1).
//   - HasCollection::resolveCollectionFromAttribute. It reads the #[CollectedBy]
//     PHP attribute off the class to pick a collection subclass. Go has neither
//     class attributes nor a type resolved from a name. Motive (1).
//   - SoftDeletes::bootSoftDeletes and initializeSoftDeletes. Both are trait
//     hooks PHP calls by method name at class boot: the first registers the
//     SoftDeletingScope, the second casts deleted_at to a date. Go has no boot
//     and no name lookup, so RegisterGlobalScopes registers the scope where the
//     scopes are collected, and the cast is the type of the DeletedAt field.
//     Motive (1).
package eloquent
