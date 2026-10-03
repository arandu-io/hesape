// Package model holds the Model, its query Builder, the Table they work over,
// Rows and soft deletes.
//
// # There is no dynamic attribute, and that is the whole design
//
// A model here is the application's own struct, embedding Model, and the table
// it is a row of is described once:
//
//	type User struct {
//		model.Model
//
//		ID        string     `db:"id"`
//		Name      string     `db:"name"`
//		Email     string     `db:"email"`
//		CreatedAt time.Time  `db:"created_at"`
//		UpdatedAt time.Time  `db:"updated_at"`
//		DeletedAt *time.Time `db:"deleted_at"`
//	}
//
//	var userTable = model.NewTable(model.TableSpec{
//		Name:        "users",
//		New:         func() model.Entity { return new(User) },
//		UniqueIDs:   true,
//		SoftDeletes: true,
//	})
//
//	rows, err := userTable.Query(db).Where("email", "=", email).Get(ctx, g)
//	user := rows.First().(*User)
//	user.Name = "Ada"        // a struct field, checked by the compiler
//	_, err = user.Save(ctx, g)
//
// # Nothing here has a type parameter
//
// Model, Builder, Table and Rows are one type each, for every table. They are
// compiled once, and every package of every application that reaches a row
// reuses them. A generic model would be compiled again -- every method of it --
// for each row type, in each package that names one, and the build cache would
// hold every copy.
//
// The typed surface an application reads is generated beside each entity: a
// query type whose methods are one-line forwards to a Builder, a collection
// that is a slice of the entity, and a constructor. A terminal here hands back
// an Entity, or Rows, and the generated code converts with a type assertion,
// which costs nothing to compile.
//
// # The row is the model
//
// A terminal hands back the application's own struct, as an Entity, and not a
// wrapper over it. Reading a column is reading a field, and the methods a row is
// saved and deleted through are the ones Go promotes out of the embedded Model.
//
// The embedded Model is one pointer. Hydration allocates the entity through the
// table's New and points its model at the row's bookkeeping, so the model knows
// which entity it is inside -- the one thing an embedded value cannot work out
// for itself. A struct written as a literal holds a nil pointer, and a value
// copy of a row holds the pointer of the row it was copied from: both refuse to
// write, with ErrUnwired, rather than writing a row nothing wired or writing
// somebody else's.
//
// A column is a field. The tag `db:"..."` names it; without a tag the name is
// the field name in snake case. A field tagged `db:"-"` is not a column.
//
// # Reading a relation back
//
// A relation is registered on the table, by name, in an init function:
//
//	func init() {
//		userTable.Relate("posts", func(u *model.Model) model.Relation {
//			return model.HasMany(u, postTable, "", "")
//		})
//	}
//
// With marks a relation to eager load, the terminal attaches what it matched to
// each row, and Related reads it back:
//
//	rows, err := userTable.Query(db).With("posts").Get(ctx, g)
//	posts, ok := rows[0].(*User).Related("posts")
//
// Loading one afterwards is Load, promoted out of the embedded model:
//
//	err := user.Load(ctx, g, "posts")
//
// # There is no mass-assignment allowlist, and nothing replaces one
//
// The allowlist already exists and the compiler enforces it: reflection cannot
// write an unexported field, from any package, ever.
//
//	type User struct {
//		model.Model
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
// Find, First, Get, Value, Pluck, Paginate, Chunk and Cursor take an auth.Grant
// and filter by auth.Tenant(g), exactly as Insert, Update and Delete do. A query
// builder reached without a Grant compiles SQL and cannot run it, on a read as
// much as on a write.
//
// What the Grant settles here is the tenant, not the Policy. auth.Authorize is
// the path a Policy answered on, and it is not the only exported way to obtain a
// Grant: auth.SystemGrant issues one for work that has no subject. So these
// methods can be reached holding a Grant nothing was ever asked about, and what
// reports that is `aru doctor` -- a lint, not the type system.
//
// The tenant filter is on by default and comes off only by naming it: a table
// shared by every tenant sets Global in its TableSpec, where a reader sees it. A
// Grant carrying no tenant is refused with ErrNoTenant before any SQL is built,
// and ErrNoTenant names which Grants those are.
//
// The tenant written on insert and matched on select is always auth.Tenant(g).
// A value the caller put in the struct for that column is overwritten: the
// tenant comes from the Grant and from nowhere else.
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
// Relations live in model/relations. The Relation interface in relation.go is
// what this package asks of one, and it is the contract declared there plus the
// one method the builder needs that the tree does not put in its own interface.
//
// There is no automatic eager loading. Reading an unloaded relation would run a
// query behind the caller, and that query carries no auth.Grant.
// PreventLazyLoading is what is left of the pair, and its doc comment says what
// it means here.
package model
