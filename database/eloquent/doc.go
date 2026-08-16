// Package eloquent holds the Model, its query Builder, its Collection and soft
// deletes.
//
// # There is no dynamic attribute, and that is the whole design
//
// A model here is the application's own struct, and the machinery works over it
// with a type parameter:
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
// the field name in snake case. A field tagged `db:"-"` is not a column.
//
// # There is no mass-assignment allowlist, and nothing replaces one
//
// The allowlist already exists and the compiler enforces it: reflection cannot
// write an unexported field, from any package, ever.
//
//	type User struct {
//		Name  string `db:"name"`  // Fill sets this
//		admin bool   `db:"admin"` // nothing outside the package can, at all
//	}
//
// So Fill writes the exported fields it finds and drops keys it does not know.
// ForceFill keeps the unknown keys as raw attributes instead. Neither can reach
// an unexported field.
//
// The cost is stated plainly: an unexported field is not a column at all, so it
// is not persisted either. A value that must be stored but must never come from
// a request is an exported field the caller does not put in the map -- in this
// framework a request never becomes a model, it becomes a validated struct
// first.
//
// # Every read carries the Grant
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
// A value the caller put in the struct for that column is overwritten: the
// tenant comes from the Grant and from nowhere else.
//
// # What the signatures do NOT carry, and why
//
// There is no context.Context. The connection contract in this component is
// query.Connection, which takes none; declaring a second, context-carrying
// connection interface here would be a second way to reach the database. A
// signature that accepted a context it could not pass on would be worse than one
// that does not accept it.
//
// # Column order on insert
//
// Values reach the grammar as a map, and a Go map has no order, so columns and
// their bindings go in sorted order on every insert. The grammar must sort
// identically -- both sides sort by column name, which is the only ordering
// either side can derive from the values alone.
//
// # What is not here
//
// Relations live in eloquent/relations and reach this package through the
// Relation interface declared in relation.go, which is declared here for the
// reason query.Connection is declared in query: in Go the interface belongs with
// its consumer, and relations imports this package for Builder.
//
// There is no automatic eager loading. Reading an unloaded relation would run a
// query behind the caller, and that query carries no auth.Grant.
// PreventLazyLoading is what is left of the pair, and its doc comment says what
// it means here.
package eloquent
