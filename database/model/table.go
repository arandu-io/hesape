package model

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/str"
)

// NoColumn names a timestamp column the table does not have.
//
// The empty string already means "the default name" in a TableSpec, so the
// absence of the column needs a spelling of its own: a table whose rows are
// never updated sets UpdatedAtColumn to NoColumn, and Save stamps nothing there.
const NoColumn = "-"

// TableSpec is what an application writes once per model: the table, how a row
// of it is allocated, and every setting that differs from the default.
//
// Every zero value is the default, and no zero value turns tenancy off: the
// only way to read a table across tenants is Global, which is a word a reviewer
// sees.
type TableSpec struct {
	// Name is the table name. It is required.
	Name string

	// New allocates an empty row: func() model.Entity { return new(User) }. It
	// is required, and it is what hydration calls once per row, so a row is a
	// plain allocation of the application's own struct.
	New func() Entity

	// PrimaryKey is the primary key column. Empty means "id".
	PrimaryKey string

	// KeyType is the primary key's type, "int" or "string". Empty means "int",
	// or "string" when UniqueIDs or ManualKey is set.
	KeyType string

	// UniqueIDs makes the key an identifier the model generates: text, not
	// incremented by the database, and filled on insert with a version 7 UUID
	// when it is empty. A key that is already set is kept.
	UniqueIDs bool

	// ManualKey says the application writes the key: it is not incremented by
	// the database and nothing fills it.
	ManualKey bool

	// NoTimestamps turns off the created-at and updated-at stamps Save writes.
	NoTimestamps bool

	// CreatedAtColumn, UpdatedAtColumn and DeletedAtColumn name the timestamp
	// columns. Empty means created_at, updated_at and deleted_at; NoColumn
	// means the table has no such column.
	CreatedAtColumn string
	UpdatedAtColumn string
	DeletedAtColumn string

	// SoftDeletes replaces a delete with a stamp on DeletedAtColumn, and
	// filters the stamped rows out of every query until WithTrashed puts them
	// back.
	//
	// The field behind DeletedAtColumn has to be able to hold a null: a
	// *time.Time, or another type that writes NULL when it is empty. A plain
	// time.Time has no null, so restoring would write the zero date and the row
	// would read as deleted at the year one.
	SoftDeletes bool

	// Global says the table is shared by every tenant, and is the only way to
	// drop the tenant filter.
	//
	// It drops the filter and nothing else. Every method that runs still takes
	// an auth.Grant and still refuses one with no tenant, so a global table is a
	// table every tenant may be allowed to read -- not a table reachable without
	// authorization.
	Global bool

	// TenantColumn is the column every statement is scoped by, from
	// auth.Tenant(g). Empty means tenant_id. A Global table names none.
	TenantColumn string

	// PerPage is the default page size for Paginate. Zero or less means 15.
	PerPage int

	// Hidden names the columns left out when a row is serialised, and Visible,
	// when it is set, the only ones kept.
	Hidden  []string
	Visible []string

	// Appends names values serialised with a row without being columns: a raw
	// attribute or a loaded relation.
	Appends []string

	// Touches names the relations whose owner is stamped when a row is saved.
	// Empty means none: a save that silently stamped a parent would be a write
	// the caller did not ask for.
	Touches []string

	// Scopes are the global scopes: a filter every query carries until
	// WithoutGlobalScope removes it by its name here.
	Scopes map[string]Scope

	// Events holds the callbacks each model event runs, in order. A callback
	// that returns an error stops the operation, and the error says why.
	Events map[Event][]func(Entity) error
}

// Table is one TableSpec, checked and resolved: the model of a table, shared by
// every row of it and by every query on it.
//
// It is immutable once built, which is what lets one package-level value serve
// every request at once. The one thing added afterwards is the relations, and
// only before the first query -- see Relate.
type Table struct {
	name         string
	newEntity    func() Entity
	entityType   reflect.Type
	schema       *entitySchema
	morphClass   string
	foreignKey   string
	keyName      string
	keyType      string
	incrementing bool
	uniqueIDs    bool
	timestamps   bool
	createdAt    string
	updatedAt    string
	deletedAt    string
	softDeletes  bool
	tenantColumn string
	perPage      int
	hidden       []string
	visible      []string
	appends      []string
	touches      []string
	scopes       map[string]Scope
	events       map[Event][]func(Entity) error

	relateMu  sync.Mutex
	relations map[string]RelationFunc
	frozen    atomic.Bool
}

// RelationFunc builds a relation from the model it starts at.
//
// It is what Relate registers, and it is called with a prototype -- the model a
// query runs through, which stands for no row -- so the relation factories in
// this package build it unconstrained and the eager loader narrows it to the
// batch afterwards. Called by an application with a row it read, the same
// factories build it constrained to that row.
type RelationFunc func(parent *Model) Relation

// NewTable checks spec and returns the table it describes.
//
// It panics on a spec that cannot work -- no name, no New, a New whose row does
// not embed Model, a Global table that names a tenant column -- because the
// call belongs in a package-level variable, where a panic stops the program
// before it serves anything, and an error would have nowhere to go.
func NewTable(spec TableSpec) *Table {
	if spec.Name == "" {
		panic("model: NewTable needs the table's Name")
	}
	if spec.New == nil {
		panic(fmt.Sprintf("model: NewTable(%q) needs New, the function that allocates an empty row", spec.Name))
	}
	if spec.Global && spec.TenantColumn != "" {
		panic(fmt.Sprintf("model: NewTable(%q) is Global and names the tenant column %q: a shared table has no tenant column", spec.Name, spec.TenantColumn))
	}

	sample := spec.New()
	entityType, err := checkEntity(sample)
	if err != nil {
		panic(fmt.Sprintf("model: NewTable(%q): %v", spec.Name, err))
	}

	t := &Table{
		name:         spec.Name,
		newEntity:    spec.New,
		entityType:   entityType,
		schema:       schemaOf(entityType),
		morphClass:   entityType.Name(),
		keyName:      spec.PrimaryKey,
		keyType:      spec.KeyType,
		incrementing: !spec.UniqueIDs && !spec.ManualKey,
		uniqueIDs:    spec.UniqueIDs,
		timestamps:   !spec.NoTimestamps,
		createdAt:    column(spec.CreatedAtColumn, "created_at"),
		updatedAt:    column(spec.UpdatedAtColumn, "updated_at"),
		deletedAt:    column(spec.DeletedAtColumn, "deleted_at"),
		softDeletes:  spec.SoftDeletes,
		tenantColumn: spec.TenantColumn,
		perPage:      spec.PerPage,
		hidden:       slices.Clone(spec.Hidden),
		visible:      slices.Clone(spec.Visible),
		appends:      slices.Clone(spec.Appends),
		touches:      slices.Clone(spec.Touches),
	}
	if t.keyName == "" {
		t.keyName = "id"
	}
	if t.keyType == "" {
		t.keyType = "int"
		if spec.UniqueIDs || spec.ManualKey {
			t.keyType = "string"
		}
	}
	if t.tenantColumn == "" && !spec.Global {
		t.tenantColumn = "tenant_id"
	}
	if t.perPage <= 0 {
		t.perPage = 15
	}
	t.foreignKey = str.Snake(t.morphClass, "_") + "_" + t.keyName
	if len(spec.Scopes) > 0 {
		t.scopes = make(map[string]Scope, len(spec.Scopes))
		for identifier, scope := range spec.Scopes {
			t.scopes[identifier] = scope
		}
	}
	if len(spec.Events) > 0 {
		t.events = make(map[Event][]func(Entity) error, len(spec.Events))
		for event, callbacks := range spec.Events {
			t.events[event] = slices.Clone(callbacks)
		}
	}
	return t
}

// column resolves a timestamp column name: the default for the empty string,
// and no column for NoColumn.
func column(name, fallback string) string {
	switch name {
	case "":
		return fallback
	case NoColumn:
		return ""
	}
	return name
}

// checkEntity reports the struct type behind an Entity, and refuses one that
// does not embed Model directly.
//
// Depth matters: hydration finds the Model through the Entity's own base method
// and the columns through the struct's fields, and an embedding one level down
// would put a struct between the two that the schema walk treats as columns.
func checkEntity(e Entity) (reflect.Type, error) {
	if e == nil {
		return nil, fmt.Errorf("New returned nil")
	}
	v := reflect.ValueOf(e)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("New returned a %T, and a row is a pointer to a struct", e)
	}
	t := v.Elem().Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous && f.Type == modelType {
			if v.Elem().Field(i).Addr().Interface() != any(e.base()) {
				break
			}
			return t, nil
		}
	}
	return nil, fmt.Errorf("%s does not embed model.Model as a field of its own", t)
}

// modelType is Model's reflect.Type, compared against the fields of an entity.
var modelType = reflect.TypeFor[Model]()

// Name returns the table name.
func (t *Table) Name() string { return t.name }

// Query returns a query on the table, through db, with the global scopes on.
//
// It takes no Grant, and that is the decision rather than an omission. Every
// terminal takes one, because authorization belongs to the statement that runs
// and not to the sentence that builds it: a Grant held on the builder would be a
// second place a tenant could come from, and the two could differ. A builder
// authorizes nothing, which is why it can be built, stored and passed around
// without one.
func (t *Table) Query(db DB) *Builder {
	return t.query(connectionOf(db), true)
}

// New returns an empty row of the table, wired to db and not yet saved.
//
// It is the way to make a row the application fills and then saves: a struct
// literal has no connection behind it and refuses to save with ErrUnwired.
func (t *Table) New(db DB) Entity {
	return t.newModel(connectionOf(db)).r.self
}

// MorphModel returns an empty row of the table on db as the model a relation
// takes, which is what an entry of the morph map in model/relations returns.
func (t *Table) MorphModel(db DB) relations.Model {
	return refOf(t.newModel(connectionOf(db)))
}

// Relate registers the relation name on the table.
//
// It belongs in an init function, after every table it names exists: two table
// variables whose initializers named each other would be an initialization
// cycle, and a function registered here is called only when a query asks for
// the relation. It panics once the table has served a query, because a table
// is shared by every request at once and a relation added under them would be a
// write racing every read.
func (t *Table) Relate(name string, fn RelationFunc) {
	if fn == nil {
		panic(fmt.Sprintf("model: %s.Relate(%q) was given no function", t.name, name))
	}
	t.relateMu.Lock()
	defer t.relateMu.Unlock()
	if t.frozen.Load() {
		panic(fmt.Sprintf("model: %s.Relate(%q) after the table served a query: register relations in init", t.name, name))
	}
	if t.relations == nil {
		t.relations = map[string]RelationFunc{}
	}
	t.relations[name] = fn
}

// freeze marks the table as serving, after which Relate refuses.
func (t *Table) freeze() {
	if !t.frozen.Load() {
		t.relateMu.Lock()
		t.frozen.Store(true)
		t.relateMu.Unlock()
	}
}

// relation returns the function registered for name, and whether there is one.
func (t *Table) relation(name string) (RelationFunc, bool) {
	fn, ok := t.relations[name]
	return fn, ok
}

// query is Query over a resolved connection, with or without the global
// scopes.
func (t *Table) query(c *conn, scoped bool) *Builder {
	t.freeze()
	q := query.NewBuilder(c.connection, c.grammar, c.processor)
	q.From(t.name)
	b := &Builder{query: q, table: t, conn: c, eagerLoad: map[string]func(*query.Builder){}}
	if !scoped {
		return b
	}
	for identifier, scope := range t.scopes {
		b.WithGlobalScope(identifier, scope)
	}
	if t.softDeletes {
		b.WithGlobalScope(SoftDeletingScopeName, softDeletingScope)
		b.onDelete = softDelete
	}
	return b
}

// newModel allocates an empty row through the spec's New and wires its model.
func (t *Table) newModel(c *conn) *Model {
	e := t.newEntity()
	m := e.base()
	m.r = &row{table: t, conn: c, self: e}
	return m
}

// qualify returns column prefixed with the table name, unless it already
// carries one.
func (t *Table) qualify(column string) string {
	if containsDot(column) {
		return column
	}
	return t.name + "." + column
}

// hasColumn reports whether the entity declares column.
func (t *Table) hasColumn(column string) bool {
	if column == "" {
		return false
	}
	_, ok := t.schema.byName[column]
	return ok
}

// conn is a connection as a model holds it: the statements, the grammar they
// compile through and the processor their results are read back through, plus
// the name a queued job writes down.
//
// One is made per Query or New and shared by every row read through it, so a
// thousand rows carry one pointer to it rather than four interfaces each.
type conn struct {
	connection query.Connection
	grammar    query.Grammar
	processor  query.Processor
	name       string
}

// connectionOf resolves db into the connection a model holds.
//
// A db that answers GetName lends it as the connection name; one that does not
// is the unnamed connection.
func connectionOf(db DB) *conn {
	c := &conn{connection: db}
	if db != nil {
		c.grammar = db.GetQueryGrammar()
		c.processor = db.GetPostProcessor()
	}
	if named, ok := db.(interface{ GetName() string }); ok {
		c.name = named.GetName()
	}
	return c
}
