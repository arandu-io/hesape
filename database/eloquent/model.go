package eloquent

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/str"
)

// Model answers Illuminate\Database\Eloquent\Model.
//
// T is the application's own struct, and its exported fields are the columns.
// One Model is one row, as one PHP model is one row: the configuration fields
// below are what a PHP subclass declares as protected properties, and they are
// copied onto every instance NewInstance makes, which is what `new static` does
// there.
//
// The fields are exported because a Go value has no subclass to override them
// in. Read the package comment before setting TenantColumn to the empty string.
type Model[T any] struct {
	// Entity is the row. PHP keeps it in $attributes and reaches it with
	// $model->name; here it is a struct the compiler checks.
	Entity T

	// Table is Model::$table. Empty means the snake-cased plural of the type
	// name, which is what getTable falls back to.
	Table string

	// PrimaryKey is Model::$primaryKey.
	PrimaryKey string

	// KeyType is Model::$keyType. It is kept because whereKey reads it, and
	// because a model whose key is a uuid says so here rather than in a comment.
	KeyType string

	// Incrementing is Model::$incrementing.
	Incrementing bool

	// Timestamps is Model::$timestamps, which HasTimestamps declares.
	Timestamps bool

	// SoftDeletes says whether the SoftDeletingScope is registered. In PHP it is
	// the presence of the SoftDeletes trait; Go has no traits, so it is a field,
	// and NewQuery registers the scope when it is set.
	SoftDeletes bool

	// TenantColumn is the column every statement is scoped by, from
	// auth.Tenant(g). It defaults to tenant_id.
	//
	// The empty string turns the scoping off, and it is spelled out at the
	// construction of the model so that a reader sees a table that is deliberately
	// global rather than a filter somebody forgot (RULE 14, RULE 17).
	TenantColumn string

	// PerPage is Model::$perPage.
	PerPage int

	// ConnectionName is Model::$connection.
	ConnectionName string

	// CreatedAtColumn and UpdatedAtColumn are the CREATED_AT and UPDATED_AT
	// constants. DeletedAtColumn is SoftDeletes' DELETED_AT.
	CreatedAtColumn string
	UpdatedAtColumn string
	DeletedAtColumn string

	// Exists is Model::$exists.
	Exists bool

	// WasRecentlyCreated is Model::$wasRecentlyCreated.
	WasRecentlyCreated bool

	// Connection, Grammar and Processor are what a PHP model reads off the
	// connection resolver. There is no resolver and no container (ADR 0001), so
	// they are given to the constructor and copied onto every instance.
	Connection query.Connection
	Grammar    query.Grammar
	Processor  query.Processor

	// RelationResolvers is what a PHP model declares as one method per relation.
	// Go cannot look a method up by name and stay type safe, so a relation is
	// registered by name once, and With, Load, Has and WithCount resolve through
	// this map.
	RelationResolvers map[string]func(*Model[T]) Relation

	attributes map[string]any
	original   map[string]any
	changes    map[string]any
	previous   map[string]any
	relations  map[string]any

	hidden  []string
	visible []string
	appends []string

	globalScopes  map[string]Scope[T]
	events        map[Event][]func(*Model[T]) error
	muted         bool
	forceDeleting bool
}

// NewModel answers Model::__construct.
//
// The three arguments after the table are what the PHP reads off the connection
// resolver at the moment it needs them; with no container to resolve through
// they are handed over once, here.
//
// The defaults are the PHP's: id, an int key, incrementing, timestamps on, 15
// per page, created_at / updated_at / deleted_at. The one that is not the PHP's
// is TenantColumn, which starts at tenant_id because RULE 14 has no default off.
func NewModel[T any](table string, connection query.Connection, grammar query.Grammar, processor query.Processor) *Model[T] {
	return &Model[T]{
		Table:           table,
		PrimaryKey:      "id",
		KeyType:         "int",
		Incrementing:    true,
		Timestamps:      true,
		TenantColumn:    "tenant_id",
		PerPage:         15,
		CreatedAtColumn: "created_at",
		UpdatedAtColumn: "updated_at",
		DeletedAtColumn: "deleted_at",
		Connection:      connection,
		Grammar:         grammar,
		Processor:       processor,
	}
}

// Fill answers Model::fill.
//
// It writes the columns the entity declares and drops the keys it does not know,
// which is what fill() does with a key outside $fillable. There is no $fillable
// to consult: an unexported field is unreachable to reflection, so the allowlist
// is the initial letter of the field and the compiler keeps it (see the package
// comment).
//
// The PHP throws MassAssignmentException; the error here is the other failure a
// typed model can have -- a value that does not fit the field.
func (m *Model[T]) Fill(attributes map[string]any) error {
	return m.setAttributes(attributes, false)
}

// ForceFill answers Model::forceFill.
//
// Unguarded mass assignment in PHP sets a key even when no property backs it, so
// this keeps the unknown keys as raw attributes where Fill drops them. It still
// cannot reach an unexported field, because nothing can.
func (m *Model[T]) ForceFill(attributes map[string]any) error {
	return m.setAttributes(attributes, true)
}

// QualifyColumn answers Model::qualifyColumn.
func (m *Model[T]) QualifyColumn(column string) string {
	if containsDot(column) {
		return column
	}
	return m.GetTable() + "." + column
}

// QualifyColumns answers Model::qualifyColumns.
func (m *Model[T]) QualifyColumns(columns []string) []string {
	out := make([]string, len(columns))
	for i, column := range columns {
		out[i] = m.QualifyColumn(column)
	}
	return out
}

// NewInstance answers Model::newInstance: a fresh model of the same shape, on
// the same table and connection, filled with the given attributes.
func (m *Model[T]) NewInstance(attributes map[string]any, exists bool) (*Model[T], error) {
	instance := &Model[T]{
		Table:             m.Table,
		PrimaryKey:        m.PrimaryKey,
		KeyType:           m.KeyType,
		Incrementing:      m.Incrementing,
		Timestamps:        m.Timestamps,
		SoftDeletes:       m.SoftDeletes,
		TenantColumn:      m.TenantColumn,
		PerPage:           m.PerPage,
		ConnectionName:    m.ConnectionName,
		CreatedAtColumn:   m.CreatedAtColumn,
		UpdatedAtColumn:   m.UpdatedAtColumn,
		DeletedAtColumn:   m.DeletedAtColumn,
		Connection:        m.Connection,
		Grammar:           m.Grammar,
		Processor:         m.Processor,
		RelationResolvers: m.RelationResolvers,
		Exists:            exists,
		hidden:            slices.Clone(m.hidden),
		visible:           slices.Clone(m.visible),
		appends:           slices.Clone(m.appends),
		globalScopes:      cloneScopes(m.globalScopes),
		events:            cloneEvents(m.events),
	}
	if err := instance.Fill(attributes); err != nil {
		return nil, err
	}
	return instance, nil
}

// NewFromBuilder answers Model::newFromBuilder: the model a row becomes on the
// way out of the database, already existing and already synced.
func (m *Model[T]) NewFromBuilder(attributes map[string]any) (*Model[T], error) {
	instance, err := m.NewInstance(nil, true)
	if err != nil {
		return nil, err
	}
	if err := instance.SetRawAttributes(attributes, true); err != nil {
		return nil, err
	}
	if err := instance.fireModelEvent(Retrieved); err != nil {
		return nil, err
	}
	return instance, nil
}

// Save answers Model::save.
//
// It reports whether anything was written, and returns the error the PHP would
// have thrown out of the connection. A model that exists and is clean is a true
// with no statement, exactly as there.
func (m *Model[T]) Save(g auth.Grant) (bool, error) {
	if err := m.fireModelEvent(Saving); err != nil {
		return false, err
	}

	var (
		saved bool
		err   error
	)
	if m.Exists {
		if !m.IsDirty() {
			saved = true
		} else {
			saved, err = m.performUpdate(g)
		}
	} else {
		saved, err = m.performInsert(g)
	}
	if err != nil {
		return false, err
	}

	if saved {
		if err := m.finishSave(); err != nil {
			return false, err
		}
	}
	return saved, nil
}

// SaveQuietly answers Model::saveQuietly.
func (m *Model[T]) SaveQuietly(g auth.Grant) (saved bool, err error) {
	return saved, m.WithoutEvents(func() error {
		saved, err = m.Save(g)
		return err
	})
}

// SaveOrFail answers Model::saveOrFail: the save, inside a transaction.
//
// The PHP asks the connection for one. query.Connection has no transaction --
// Illuminate's ConnectionInterface does, but the narrow one this component
// declares does not -- so the connection is asked whether it can open one, and
// a connection that cannot says so instead of writing outside a transaction the
// caller believes it is in.
func (m *Model[T]) SaveOrFail(g auth.Grant) (bool, error) {
	var saved bool
	err := m.transaction(func() error {
		var err error
		saved, err = m.Save(g)
		return err
	})
	return saved, err
}

// Update answers Model::update: fill, then save. A model that does not exist
// yet is false and no statement, as there.
func (m *Model[T]) Update(g auth.Grant, attributes map[string]any) (bool, error) {
	if !m.Exists {
		return false, nil
	}
	if err := m.Fill(attributes); err != nil {
		return false, err
	}
	return m.Save(g)
}

// UpdateQuietly answers Model::updateQuietly.
func (m *Model[T]) UpdateQuietly(g auth.Grant, attributes map[string]any) (saved bool, err error) {
	return saved, m.WithoutEvents(func() error {
		saved, err = m.Update(g, attributes)
		return err
	})
}

// UpdateOrFail answers Model::updateOrFail: the update, inside a transaction.
func (m *Model[T]) UpdateOrFail(g auth.Grant, attributes map[string]any) (bool, error) {
	if !m.Exists {
		return false, nil
	}
	if err := m.Fill(attributes); err != nil {
		return false, err
	}
	return m.SaveOrFail(g)
}

// Push answers Model::push: save the model and everything loaded on it.
//
// PHP walks $relations and calls push() on each, which works because every value
// there is a Model. A loaded relation here is an any, so Push recurses into
// whatever answers Pushable -- *Model[R] and Collection[R] both do.
func (m *Model[T]) Push(g auth.Grant) (bool, error) {
	saved, err := m.Save(g)
	if err != nil || !saved {
		return saved, err
	}
	for _, related := range m.relations {
		pushable, ok := related.(Pushable)
		if !ok {
			continue
		}
		pushed, err := pushable.Push(g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

// PushQuietly answers Model::pushQuietly.
func (m *Model[T]) PushQuietly(g auth.Grant) (pushed bool, err error) {
	return pushed, m.WithoutEvents(func() error {
		pushed, err = m.Push(g)
		return err
	})
}

// Pushable is what Push recurses into. See Push.
type Pushable interface {
	Push(g auth.Grant) (bool, error)
}

// finishSave answers Model::finishSave.
//
// touchOwners is not called: touching the owner of a relation needs the relation
// to say who its owner is, which lives in eloquent/relations.
func (m *Model[T]) finishSave() error {
	if err := m.fireModelEvent(Saved); err != nil {
		return err
	}
	m.SyncOriginal()
	return nil
}

// performUpdate answers Model::performUpdate.
func (m *Model[T]) performUpdate(g auth.Grant) (bool, error) {
	if err := m.fireModelEvent(Updating); err != nil {
		return false, err
	}
	if m.UsesTimestamps() {
		m.UpdateTimestamps()
	}

	dirty := m.GetDirty()
	if len(dirty) == 0 {
		return true, nil
	}

	q := m.NewModelQuery()
	m.setKeysForSaveQuery(q)
	if _, err := q.Update(g, dirty); err != nil {
		return false, err
	}

	m.SyncChanges()
	if err := m.fireModelEvent(Updated); err != nil {
		return false, err
	}
	return true, nil
}

// performInsert answers Model::performInsert.
func (m *Model[T]) performInsert(g auth.Grant) (bool, error) {
	if err := m.fireModelEvent(Creating); err != nil {
		return false, err
	}
	if m.UsesTimestamps() {
		m.UpdateTimestamps()
	}

	attributes := m.getAttributesForInsert()
	q := m.NewModelQuery()

	if m.Incrementing {
		id, err := q.InsertGetID(g, attributes, m.GetKeyName())
		if err != nil {
			return false, err
		}
		if err := m.SetAttribute(m.GetKeyName(), id); err != nil {
			return false, err
		}
	} else {
		if len(attributes) == 0 {
			return true, nil
		}
		if _, err := q.Insert(g, attributes); err != nil {
			return false, err
		}
	}

	m.Exists = true
	m.WasRecentlyCreated = true

	if err := m.fireModelEvent(Created); err != nil {
		return false, err
	}
	return true, nil
}

// getAttributesForInsert answers Model::getAttributesForInsert.
//
// It drops the key of an incrementing model when the key is still zero, which
// the PHP never has to do: an unset property is simply absent from $attributes
// there, while a Go struct always carries the field, and inserting id = 0 into
// an auto-increment column is a row with the wrong id on MySQL and an error on
// Postgres.
func (m *Model[T]) getAttributesForInsert() map[string]any {
	attributes := m.GetAttributes()
	if m.Incrementing {
		if value, ok := attributes[m.GetKeyName()]; ok && isZero(value) {
			delete(attributes, m.GetKeyName())
		}
	}
	return attributes
}

// setKeysForSaveQuery answers Model::setKeysForSaveQuery.
func (m *Model[T]) setKeysForSaveQuery(b *Builder[T]) *Builder[T] {
	return b.Where(m.GetQualifiedKeyName(), "=", m.getKeyForSaveQuery())
}

// getKeyForSaveQuery answers Model::getKeyForSaveQuery: the original key, so
// that changing the key of a loaded row updates the right one.
func (m *Model[T]) getKeyForSaveQuery() any {
	if original, ok := m.original[m.GetKeyName()]; ok {
		return original
	}
	return m.GetKey()
}

// Delete answers Model::delete.
//
// The PHP returns null when the model does not exist; here that is false with no
// error, because a Go bool has no third state and "there was nothing to delete"
// is not a failure.
func (m *Model[T]) Delete(g auth.Grant) (bool, error) {
	if m.GetKeyName() == "" {
		return false, ErrNoKey
	}
	if !m.Exists {
		return false, nil
	}
	if err := m.fireModelEvent(Deleting); err != nil {
		return false, err
	}
	if err := m.performDeleteOnModel(g); err != nil {
		return false, err
	}
	if err := m.fireModelEvent(Deleted); err != nil {
		return false, err
	}
	return true, nil
}

// DeleteQuietly answers Model::deleteQuietly.
func (m *Model[T]) DeleteQuietly(g auth.Grant) (deleted bool, err error) {
	return deleted, m.WithoutEvents(func() error {
		deleted, err = m.Delete(g)
		return err
	})
}

// DeleteOrFail answers Model::deleteOrFail: the delete, inside a transaction.
func (m *Model[T]) DeleteOrFail(g auth.Grant) (bool, error) {
	if !m.Exists {
		return false, nil
	}
	var deleted bool
	err := m.transaction(func() error {
		var err error
		deleted, err = m.Delete(g)
		return err
	})
	return deleted, err
}

// Destroy answers Model::destroy.
//
// It loads the rows and deletes them one at a time, as the PHP does, so that a
// soft delete stays a soft delete and every event fires with the row it is about.
func (m *Model[T]) Destroy(g auth.Grant, ids ...any) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	models, err := m.NewQuery().WhereKey(ids).Get(g)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, model := range models {
		deleted, err := model.Delete(g)
		if err != nil {
			return count, err
		}
		if deleted {
			count++
		}
	}
	return count, nil
}

// Fresh answers Model::fresh: the same row, read again, as a new model.
//
// It queries without the global scopes, which is what makes it able to find a
// row that has since been soft deleted.
func (m *Model[T]) Fresh(g auth.Grant, with ...string) (*Model[T], error) {
	if !m.Exists {
		return nil, nil
	}
	q := m.NewQueryWithoutScopes().With(with...)
	m.setKeysForSelectQuery(q)
	return q.First(g)
}

// Refresh answers Model::refresh: the same row, read again, into this model.
func (m *Model[T]) Refresh(g auth.Grant) error {
	if !m.Exists {
		return nil
	}
	q := m.NewQueryWithoutScopes()
	m.setKeysForSelectQuery(q)
	fresh, err := q.FirstOrFail(g)
	if err != nil {
		return err
	}
	if err := m.SetRawAttributes(fresh.GetAttributes(), true); err != nil {
		return err
	}
	loaded := make([]string, 0, len(m.relations))
	for name := range m.relations {
		loaded = append(loaded, name)
	}
	slices.Sort(loaded)
	return m.Load(g, loaded...)
}

// setKeysForSelectQuery answers Model::setKeysForSelectQuery.
func (m *Model[T]) setKeysForSelectQuery(b *Builder[T]) *Builder[T] {
	return b.Where(m.GetQualifiedKeyName(), "=", m.getKeyForSaveQuery())
}

// Replicate answers Model::replicate: the same row as a new, unsaved model.
//
// The key, the timestamps and anything named in except are left out.
func (m *Model[T]) Replicate(except ...string) (*Model[T], error) {
	defaults := []string{m.GetKeyName(), m.GetCreatedAtColumn(), m.GetUpdatedAtColumn()}
	drop := append(slices.Clone(except), defaults...)

	attributes := m.GetAttributes()
	for _, key := range drop {
		if key != "" {
			delete(attributes, key)
		}
	}

	instance, err := m.NewInstance(nil, false)
	if err != nil {
		return nil, err
	}
	if err := instance.SetRawAttributes(attributes, false); err != nil {
		return nil, err
	}
	instance.relations = copyMap(m.relations)
	if err := instance.fireModelEvent(Replicating); err != nil {
		return nil, err
	}
	return instance, nil
}

// ReplicateQuietly answers Model::replicateQuietly.
func (m *Model[T]) ReplicateQuietly(except ...string) (copied *Model[T], err error) {
	return copied, m.WithoutEvents(func() error {
		copied, err = m.Replicate(except...)
		return err
	})
}

// Is answers Model::is: the same key, on the same table, on the same connection.
func (m *Model[T]) Is(other *Model[T]) bool {
	return other != nil &&
		reflect.DeepEqual(m.GetKey(), other.GetKey()) &&
		m.GetTable() == other.GetTable() &&
		m.GetConnectionName() == other.GetConnectionName()
}

// IsNot answers Model::isNot.
func (m *Model[T]) IsNot(other *Model[T]) bool { return !m.Is(other) }

// Load answers Model::load: eager load these relations onto this model.
func (m *Model[T]) Load(g auth.Grant, relations ...string) error {
	if len(relations) == 0 {
		return nil
	}
	return m.NewQueryWithoutRelationships().With(relations...).EagerLoadRelations(g, Collection[T]{m})
}

// LoadMissing answers Model::loadMissing.
func (m *Model[T]) LoadMissing(g auth.Grant, relations ...string) error {
	return Collection[T]{m}.LoadMissing(g, relations...)
}

// LoadCount answers Model::loadCount.
func (m *Model[T]) LoadCount(g auth.Grant, relations ...string) error {
	return Collection[T]{m}.LoadCount(g, relations...)
}

// LoadAggregate answers Model::loadAggregate.
func (m *Model[T]) LoadAggregate(g auth.Grant, relations []string, column, function string) error {
	return Collection[T]{m}.LoadAggregate(g, relations, column, function)
}

// GetRelation answers Model::getRelation.
//
// The value is an any because the related rows are models of another type, and
// a Go field cannot hold "some other model". Related is the typed way to read it.
func (m *Model[T]) GetRelation(name string) (any, bool) {
	value, ok := m.relations[name]
	return value, ok
}

// SetRelation answers Model::setRelation.
func (m *Model[T]) SetRelation(name string, value any) *Model[T] {
	if m.relations == nil {
		m.relations = map[string]any{}
	}
	m.relations[name] = value
	return m
}

// RelationLoaded answers Model::relationLoaded.
func (m *Model[T]) RelationLoaded(name string) bool {
	_, ok := m.relations[name]
	return ok
}

// GetRelations answers Model::getRelations.
func (m *Model[T]) GetRelations() map[string]any { return copyMap(m.relations) }

// SetRelations answers Model::setRelations.
func (m *Model[T]) SetRelations(relations map[string]any) *Model[T] {
	m.relations = copyMap(relations)
	return m
}

// UnsetRelation answers Model::unsetRelation.
func (m *Model[T]) UnsetRelation(name string) *Model[T] {
	delete(m.relations, name)
	return m
}

// UnsetRelations answers Model::unsetRelations.
func (m *Model[T]) UnsetRelations() *Model[T] {
	m.relations = nil
	return m
}

// WithoutRelations answers Model::withoutRelations: the same row, with nothing
// loaded hanging off it.
func (m *Model[T]) WithoutRelations() (*Model[T], error) {
	instance, err := m.NewInstance(nil, m.Exists)
	if err != nil {
		return nil, err
	}
	if err := instance.SetRawAttributes(m.GetAttributes(), false); err != nil {
		return nil, err
	}
	instance.original = copyMap(m.original)
	return instance, nil
}

// Related reads a loaded relation as the type it was loaded as.
//
// It is the typed half of $model->posts, which Go cannot spell as a field: the
// relation was loaded as models of another type, so the read is a generic
// function rather than a method. It reports false when the relation was not
// loaded, or was loaded as something else.
func Related[T, R any](m *Model[T], name string) (Collection[R], bool) {
	value, ok := m.GetRelation(name)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case Collection[R]:
		return typed, true
	case *Model[R]:
		return Collection[R]{typed}, true
	}
	return nil, false
}

// GetKey answers Model::getKey.
func (m *Model[T]) GetKey() any { return m.GetAttribute(m.GetKeyName()) }

// GetKeyName answers Model::getKeyName.
func (m *Model[T]) GetKeyName() string { return m.PrimaryKey }

// SetKeyName answers Model::setKeyName.
func (m *Model[T]) SetKeyName(key string) *Model[T] {
	m.PrimaryKey = key
	return m
}

// GetQualifiedKeyName answers Model::getQualifiedKeyName.
func (m *Model[T]) GetQualifiedKeyName() string { return m.QualifyColumn(m.GetKeyName()) }

// GetKeyType answers Model::getKeyType.
func (m *Model[T]) GetKeyType() string { return m.KeyType }

// SetKeyType answers Model::setKeyType.
func (m *Model[T]) SetKeyType(keyType string) *Model[T] {
	m.KeyType = keyType
	return m
}

// GetIncrementing answers Model::getIncrementing.
func (m *Model[T]) GetIncrementing() bool { return m.Incrementing }

// SetIncrementing answers Model::setIncrementing.
func (m *Model[T]) SetIncrementing(value bool) *Model[T] {
	m.Incrementing = value
	return m
}

// GetTable answers Model::getTable.
//
// The fallback is the PHP's: the class name, pluralised and snake cased. Here it
// is the name of T, so a struct called User reads from users.
func (m *Model[T]) GetTable() string {
	if m.Table != "" {
		return m.Table
	}
	name := reflect.TypeFor[T]().Name()
	if name == "" {
		return ""
	}
	return str.Snake(str.PluralStudly(name), "_")
}

// SetTable answers Model::setTable.
func (m *Model[T]) SetTable(table string) *Model[T] {
	m.Table = table
	return m
}

// GetConnection answers Model::getConnection.
//
// The PHP resolves the name through the connection resolver; there is no
// resolver (ADR 0001), so the connection is the one the model was given.
func (m *Model[T]) GetConnection() query.Connection { return m.Connection }

// GetConnectionName answers Model::getConnectionName.
func (m *Model[T]) GetConnectionName() string { return m.ConnectionName }

// SetConnection answers Model::setConnection.
//
// It takes the connection as well as the name, for the reason GetConnection
// gives: with no resolver, a name is not enough to reach a connection.
func (m *Model[T]) SetConnection(name string, connection query.Connection) *Model[T] {
	m.ConnectionName = name
	if connection != nil {
		m.Connection = connection
	}
	return m
}

// GetPerPage answers Model::getPerPage.
func (m *Model[T]) GetPerPage() int {
	if m.PerPage <= 0 {
		return 15
	}
	return m.PerPage
}

// SetPerPage answers Model::setPerPage.
func (m *Model[T]) SetPerPage(perPage int) *Model[T] {
	m.PerPage = perPage
	return m
}

// GetForeignKey answers Model::getForeignKey: the name this model has when
// another table points at it.
func (m *Model[T]) GetForeignKey() string {
	name := reflect.TypeFor[T]().Name()
	return str.Snake(name, "_") + "_" + m.GetKeyName()
}

// UsesTimestamps answers HasTimestamps::usesTimestamps.
func (m *Model[T]) UsesTimestamps() bool { return m.Timestamps }

// GetCreatedAtColumn answers HasTimestamps::getCreatedAtColumn.
func (m *Model[T]) GetCreatedAtColumn() string { return m.CreatedAtColumn }

// GetUpdatedAtColumn answers HasTimestamps::getUpdatedAtColumn.
func (m *Model[T]) GetUpdatedAtColumn() string { return m.UpdatedAtColumn }

// GetQualifiedCreatedAtColumn answers HasTimestamps::getQualifiedCreatedAtColumn.
func (m *Model[T]) GetQualifiedCreatedAtColumn() string {
	return m.QualifyColumn(m.GetCreatedAtColumn())
}

// GetQualifiedUpdatedAtColumn answers HasTimestamps::getQualifiedUpdatedAtColumn.
func (m *Model[T]) GetQualifiedUpdatedAtColumn() string {
	return m.QualifyColumn(m.GetUpdatedAtColumn())
}

// FreshTimestamp answers HasTimestamps::freshTimestamp.
func (m *Model[T]) FreshTimestamp() time.Time { return time.Now().UTC() }

// WithoutTimestamps answers HasTimestamps::withoutTimestamps: the callback runs
// with the timestamps switched off, and they come back on however it ends.
func (m *Model[T]) WithoutTimestamps(callback func() error) error {
	previous := m.Timestamps
	m.Timestamps = false
	defer func() { m.Timestamps = previous }()
	return callback()
}

// UpdateTimestamps answers HasTimestamps::updateTimestamps.
//
// A column the entity does not declare is skipped rather than written as a raw
// attribute: a model without a created_at field is a table without the column,
// and inserting one would fail on the first row.
func (m *Model[T]) UpdateTimestamps() {
	now := m.FreshTimestamp()
	if column := m.GetUpdatedAtColumn(); m.hasColumn(column) {
		_, _ = m.setAttribute(column, now)
	}
	if column := m.GetCreatedAtColumn(); !m.Exists && m.hasColumn(column) {
		_, _ = m.setAttribute(column, now)
	}
}

// hasColumn reports whether the entity declares this column.
func (m *Model[T]) hasColumn(column string) bool {
	if column == "" {
		return false
	}
	_, ok := fieldByColumn(reflect.TypeFor[T](), column)
	return ok
}

// transaction runs fn inside a transaction when the connection can open one.
//
// See SaveOrFail for why it is an assertion rather than a method on
// query.Connection.
func (m *Model[T]) transaction(fn func() error) error {
	transactor, ok := m.Connection.(Transactor)
	if !ok {
		return fmt.Errorf("eloquent: %s: the connection cannot open a transaction, so this cannot be the atomic form", m.GetTable())
	}
	return transactor.Transaction(fn)
}

// Transactor is a connection that can open a transaction.
//
// Illuminate's ConnectionInterface declares transaction(); the narrow
// query.Connection this component builds on does not, and widening it would
// change a contract this slice does not own. So the capability is asked for by
// assertion, which is how Go spells an optional one.
type Transactor interface {
	// Transaction answers ConnectionInterface::transaction.
	Transaction(callback func() error) error
}

func containsDot(s string) bool {
	for i := range len(s) {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
