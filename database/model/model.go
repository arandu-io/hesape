package model

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/query"
)

// Table returns the table this row belongs to, and nil for a row the framework
// did not build.
func (m *Model) Table() *Table {
	if !live(m) {
		return nil
	}
	return m.r.table
}

// Exists reports whether the row is stored in the database.
func (m *Model) Exists() bool { return live(m) && m.r.exists }

// WasRecentlyCreated reports whether this row was inserted by this value, as
// opposed to read back.
func (m *Model) WasRecentlyCreated() bool { return live(m) && m.r.recent }

// Save inserts or updates the row, and reports whether anything was written.
//
// A row that exists and is clean is a true with no statement: there is nothing
// to write.
func (m *Model) Save(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}

	if err := fireModelEvent(m, Saving); err != nil {
		return false, err
	}

	var (
		saved bool
		err   error
	)
	if m.r.exists {
		if !m.IsDirty() {
			saved = true
		} else {
			saved, err = performUpdate(m, ctx, g)
		}
	} else {
		saved, err = performInsert(m, ctx, g)
	}
	if err != nil {
		return false, err
	}

	if saved {
		if err := fireModelEvent(m, Saved); err != nil {
			return false, err
		}
		m.SyncOriginal()
	}
	return saved, nil
}

// SaveQuietly saves the row without firing model events.
func (m *Model) SaveQuietly(ctx context.Context, g auth.Grant) (saved bool, err error) {
	return saved, m.WithoutEvents(func() error {
		saved, err = m.Save(ctx, g)
		return err
	})
}

// SaveOrFail is the save, inside a transaction.
//
// query.Connection has no transaction on it, so the connection is asked whether
// it can open one, and a connection that cannot says so instead of writing
// outside a transaction the caller believes it is in.
func (m *Model) SaveOrFail(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}
	var saved bool
	err := transaction(m, func() error {
		var err error
		saved, err = m.Save(ctx, g)
		return err
	})
	return saved, err
}

// Update fills the row with attributes, then saves it. A row that does not
// exist yet is false and no statement.
func (m *Model) Update(ctx context.Context, g auth.Grant, attributes map[string]any) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}
	if !m.r.exists {
		return false, nil
	}
	if err := m.Fill(attributes); err != nil {
		return false, err
	}
	return m.Save(ctx, g)
}

// Push saves the row and everything loaded on it.
//
// A loaded relation is held as an any, so Push recurses into whatever
// implements Pushable -- an entity and Rows both do.
func (m *Model) Push(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}

	saved, err := m.Save(ctx, g)
	if err != nil || !saved {
		return saved, err
	}
	for _, name := range sortedKeys(m.r.relations) {
		pushable, ok := m.r.relations[name].(Pushable)
		if !ok {
			continue
		}
		pushed, err := pushable.Push(ctx, g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

// Pushable is what Push recurses into. See Push.
type Pushable interface {
	Push(ctx context.Context, g auth.Grant) (bool, error)
}

// performUpdate fires the Updating/Updated events and writes the dirty
// columns for a row that already exists.
func performUpdate(m *Model, ctx context.Context, g auth.Grant) (bool, error) {
	if err := fireModelEvent(m, Updating); err != nil {
		return false, err
	}
	if usesTimestamps(m) {
		updateTimestamps(m)
	}

	dirty := m.GetDirty()
	// A tenant field the caller changed is not written: the statement keeps the
	// Grant's tenant. The entity is put back to it, so that what the caller
	// holds after the save is the row as stored.
	if column := m.r.table.tenantColumn; column != "" {
		if _, changed := dirty[column]; changed {
			if err := stampTenant(m, g); err != nil {
				return false, err
			}
			dirty = m.GetDirty()
		}
	}
	if len(dirty) == 0 {
		return true, nil
	}

	q := newModelQuery(m)
	setKeysForSaveQuery(m, q)
	if _, err := q.Update(ctx, g, dirty); err != nil {
		return false, err
	}

	syncChanges(m)
	if err := fireModelEvent(m, Updated); err != nil {
		return false, err
	}
	return true, nil
}

// performInsert fires the Creating/Created events and inserts the row for a
// model that does not exist yet.
func performInsert(m *Model, ctx context.Context, g auth.Grant) (bool, error) {
	if err := setUniqueID(m); err != nil {
		return false, err
	}
	// The row is written with the Grant's tenant whatever the entity holds, so
	// the entity is given it too: listeners see the row that will be written,
	// and the value handed back afterwards matches the stored one.
	if err := stampTenant(m, g); err != nil {
		return false, err
	}
	if err := fireModelEvent(m, Creating); err != nil {
		return false, err
	}
	if usesTimestamps(m) {
		updateTimestamps(m)
	}
	// Again after Creating, which may have assigned the field.
	if err := stampTenant(m, g); err != nil {
		return false, err
	}

	attributes := getAttributesForInsert(m)
	q := newModelQuery(m)

	if m.r.table.incrementing {
		id, err := q.InsertGetID(ctx, g, attributes, m.r.table.keyName)
		if err != nil {
			return false, err
		}
		if err := m.SetAttribute(m.r.table.keyName, id); err != nil {
			return false, err
		}
	} else {
		if len(attributes) == 0 {
			return true, nil
		}
		if _, err := q.Insert(ctx, g, attributes); err != nil {
			return false, err
		}
	}

	m.r.exists = true
	m.r.recent = true

	if err := fireModelEvent(m, Created); err != nil {
		return false, err
	}
	return true, nil
}

// stampTenant sets the entity's tenant field to the Grant's tenant.
//
// Every write puts auth.Tenant(g) in the tenant column whatever the entity
// carries, so without this the value a save hands back would disagree with the
// row it stored -- empty after a Create from a map, since Fill never writes the
// tenant. A table with no tenant column, or an entity with no field for it, is
// left alone.
func stampTenant(m *Model, g auth.Grant) error {
	column := m.r.table.tenantColumn
	if column == "" || !m.r.table.hasColumn(column) {
		return nil
	}
	_, err := setAttribute(m, column, auth.Tenant(g))
	return err
}

// getAttributesForInsert returns the row's columns for an insert statement.
//
// It drops the key of an incrementing model when the key is still zero. A Go
// struct always carries the field, so a zero key cannot simply be absent the
// way an unset property would be; inserting id = 0 into an auto-increment
// column is a row with the wrong id on MySQL and an error on Postgres.
func getAttributesForInsert(m *Model) map[string]any {
	attributes := m.GetAttributes()
	if m.r.table.incrementing {
		key := m.r.table.keyName
		if value, ok := attributes[key]; ok && isZero(value) {
			delete(attributes, key)
		}
	}
	return attributes
}

// setKeysForSaveQuery adds the primary key filter a save statement runs
// under.
func setKeysForSaveQuery(m *Model, b *Builder) *Builder {
	return b.Where(m.r.table.qualify(m.r.table.keyName), "=", getKeyForSaveQuery(m))
}

// getKeyForSaveQuery returns the original key, so that changing the key of a
// loaded row updates the right one.
func getKeyForSaveQuery(m *Model) any {
	if original, ok := m.r.original[m.r.table.keyName]; ok {
		return original
	}
	return m.GetKey()
}

// Delete removes the row, and reports whether it was deleted.
//
// A row that does not exist returns false with no error: a Go bool has no
// third state, and "there was nothing to delete" is not a failure. A table that
// soft deletes stamps the row instead.
func (m *Model) Delete(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}
	if !m.r.exists {
		return false, nil
	}
	if err := fireModelEvent(m, Deleting); err != nil {
		return false, err
	}
	if err := performDeleteOnModel(m, ctx, g); err != nil {
		return false, err
	}
	if err := fireModelEvent(m, Deleted); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteQuietly deletes the row without firing model events.
func (m *Model) DeleteQuietly(ctx context.Context, g auth.Grant) (deleted bool, err error) {
	return deleted, m.WithoutEvents(func() error {
		deleted, err = m.Delete(ctx, g)
		return err
	})
}

// DeleteOrFail is Delete, inside a transaction.
func (m *Model) DeleteOrFail(ctx context.Context, g auth.Grant) (bool, error) {
	if err := wired(m); err != nil {
		return false, err
	}
	if !m.r.exists {
		return false, nil
	}
	var deleted bool
	err := transaction(m, func() error {
		var err error
		deleted, err = m.Delete(ctx, g)
		return err
	})
	return deleted, err
}

// Fresh returns the same row, read again, as a new entity, with the named
// relations loaded on it.
//
// It queries without the global scopes, which is what makes it able to find a
// row that has since been soft deleted. A row that does not exist answers
// (nil, nil).
func (m *Model) Fresh(ctx context.Context, g auth.Grant, with ...string) (Entity, error) {
	if err := wired(m); err != nil {
		return nil, err
	}
	if !m.r.exists {
		return nil, nil
	}
	q := newModelQuery(m).With(with...)
	setKeysForSaveQuery(m, q)
	return q.First(ctx, g)
}

// Refresh reads the same row again, into this one, and loads again every
// relation that was loaded.
func (m *Model) Refresh(ctx context.Context, g auth.Grant) error {
	if err := wired(m); err != nil {
		return err
	}
	if !m.r.exists {
		return nil
	}
	q := newModelQuery(m)
	setKeysForSaveQuery(m, q)
	fresh, err := firstOrFail(q, ctx, g)
	if err != nil {
		return err
	}
	if err := m.SetRawAttributes(fresh.GetAttributes(), true); err != nil {
		return err
	}
	loaded := sortedKeys(m.r.relations)
	return m.Load(ctx, g, loaded...)
}

// Replicate returns the same row as a new, unsaved entity on the same
// connection.
//
// The key, the timestamps and anything named in except are left out, and the
// loaded relations come along.
func (m *Model) Replicate(except ...string) (Entity, error) {
	if !live(m) {
		return nil, ErrUnwired
	}
	instance, err := replicate(m, except...)
	if err != nil {
		return nil, err
	}
	return instance.r.self, nil
}

// replicate is Replicate with the model still in hand.
func replicate(m *Model, except ...string) (*Model, error) {
	t := m.r.table
	drop := append(slices.Clone(except), t.keyName, t.createdAt, t.updatedAt)

	attributes := m.GetAttributes()
	for _, key := range drop {
		if key != "" {
			delete(attributes, key)
		}
	}

	instance := t.newModel(m.r.conn)
	if err := instance.SetRawAttributes(attributes, false); err != nil {
		return nil, err
	}
	instance.r.relations = copyMap(m.r.relations)
	if err := fireModelEvent(instance, Replicating); err != nil {
		return nil, err
	}
	return instance, nil
}

// Is reports whether other is the same row: the same key, on the same table, on
// the same connection.
func (m *Model) Is(other Entity) bool {
	o := modelOf(other)
	return live(m) && live(o) && sameRow(m, o)
}

// sameRow is the comparison Is makes.
func sameRow(a, b *Model) bool {
	return equalValues(a.GetKey(), b.GetKey()) &&
		tableNameOf(a) == tableNameOf(b) &&
		a.r.conn.name == b.r.conn.name
}

// Load eager loads these relations onto this row.
func (m *Model) Load(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 {
		return nil
	}
	if err := wired(m); err != nil {
		return err
	}
	return loadModels(ctx, g, Rows{m.r.self}, relations...)
}

// LoadMissing eager loads these relations onto this row, skipping the ones
// already loaded.
func (m *Model) LoadMissing(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 {
		return nil
	}
	if err := wired(m); err != nil {
		return err
	}
	return loadMissingModels(ctx, g, Rows{m.r.self}, relations...)
}

// LoadCount loads the count of each relation onto this row, as the raw
// attribute <relation>_count.
func (m *Model) LoadCount(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 {
		return nil
	}
	if err := wired(m); err != nil {
		return err
	}
	return loadAggregateModels(ctx, g, Rows{m.r.self}, relations, "*", "count")
}

// GetKey returns the value of the primary key column.
func (m *Model) GetKey() any {
	if !live(m) {
		return nil
	}
	return m.GetAttribute(m.r.table.keyName)
}

// WithoutEvents runs callback with model events muted on this row, and
// restores the previous setting however it ends.
func (m *Model) WithoutEvents(callback func() error) error {
	if !live(m) {
		return callback()
	}
	previous := m.r.muted
	m.r.muted = true
	defer func() { m.r.muted = previous }()
	return callback()
}

// setUniqueID fills an empty primary key on a table that uses unique ids, and
// does nothing on any other.
func setUniqueID(m *Model) error {
	if !m.r.table.uniqueIDs || !isZero(m.GetKey()) {
		return nil
	}
	id, err := database.NewOrderedID()
	if err != nil {
		return err
	}
	return m.SetAttribute(m.r.table.keyName, id)
}

// tableName is the table this row is read from: the table's own name, or the
// alias a relation joined it to itself under.
func tableNameOf(m *Model) string {
	if m.r.tableName != "" {
		return m.r.tableName
	}
	return m.r.table.name
}

// qualifyColumn returns column qualified with the row's table name, unless it
// already contains a dot.
func qualifyColumn(m *Model, column string) string {
	if containsDot(column) {
		return column
	}
	return tableNameOf(m) + "." + column
}

// usesTimestamps reports whether Save stamps created_at and updated_at on this
// row now.
func usesTimestamps(m *Model) bool { return m.r.table.timestamps && !m.r.noTimestamps }

// freshTimestamp returns the current time in UTC.
func freshTimestamp() time.Time { return time.Now().UTC() }

// updateTimestamps stamps the updated-at column, and the created-at column
// when the row does not yet exist.
//
// A column the entity does not declare is skipped rather than written as a
// raw attribute: a model without a created_at field is a table without the
// column, and inserting one would fail on the first row.
func updateTimestamps(m *Model) {
	now := freshTimestamp()
	t := m.r.table
	if t.hasColumn(t.updatedAt) {
		_, _ = setAttribute(m, t.updatedAt, now)
	}
	if !m.r.exists && t.hasColumn(t.createdAt) {
		_, _ = setAttribute(m, t.createdAt, now)
	}
}

// newModelQuery returns a query on this row's table and connection, with no
// global scopes.
func newModelQuery(m *Model) *Builder { return m.r.table.query(m.r.conn, false) }

// newQuery returns a query on this row's table and connection, with the global
// scopes on.
func newQuery(m *Model) *Builder { return m.r.table.query(m.r.conn, true) }

// newInstance returns an empty row of the same table on the same connection,
// filled with attributes and not yet saved.
func newInstance(m *Model, attributes map[string]any) (*Model, error) {
	instance := m.r.table.newModel(m.r.conn)
	// Filled before it is marked as existing: the attributes a caller builds an
	// instance with are its row, key included, and Fill guards the key only of
	// a row that already exists.
	if err := instance.Fill(attributes); err != nil {
		return nil, err
	}
	return instance, nil
}

// transaction runs fn inside a transaction when the connection can open one.
//
// See SaveOrFail for why it is an assertion rather than a method on
// query.Connection.
func transaction(m *Model, fn func() error) error {
	transactor, ok := m.r.conn.connection.(Transactor)
	if !ok {
		return fmt.Errorf("model: %s: the connection cannot open a transaction, so this cannot be the atomic form", m.r.table.name)
	}
	return transactor.Transaction(fn)
}

// Transactor is a connection that can open a transaction.
//
// The narrow query.Connection this component builds on does not declare one, and
// widening it would change a contract this package does not own. So the
// capability is asked for by assertion, which is how Go spells an optional one.
type Transactor interface {
	// Transaction runs callback inside a database transaction.
	Transaction(callback func() error) error
}

// Savepointer is a Transactor that also reports how deep it already is.
//
// It is what Builder.WithSavepointIfNeeded asks for, and it is separate from
// Transactor because a connection can be able to open a transaction without
// being able to say whether one is open -- and guessing wrong there is either a
// savepoint nobody asked for or a rollback that takes the caller's work with it.
type Savepointer interface {
	Transactor

	// TransactionLevel returns how many transactions are currently nested.
	TransactionLevel() int
}

// DB is a connection that also carries the grammar its statements compile
// through and the processor their results are read back through.
//
// query.Connection is the five verbs and nothing else, on purpose: it is the
// contract a driver implements. The two below are not a driver's job, they are
// what a connection already knows about itself -- which grammar quotes its
// identifiers, which processor reads an inserted key back -- and asking for them
// by assertion is how Go spells a capability a narrow contract does not carry.
// Transactor is the same shape for the same reason.
type DB interface {
	query.Connection

	// GetQueryGrammar returns the grammar statements are compiled through.
	GetQueryGrammar() query.Grammar

	// GetPostProcessor returns the processor results are read back through.
	GetPostProcessor() query.Processor
}

func containsDot(s string) bool {
	for i := range len(s) {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
