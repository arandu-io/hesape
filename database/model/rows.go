package model

import (
	"context"
	"reflect"
	"slices"

	"github.com/arandu-io/hesape/auth"
)

// Rows is the rows a query came back with, as entities.
//
// It is one type for every table, so it is compiled once rather than once per
// row type in every package that names one. The typed collection an application
// reads -- a slice of its own struct -- is generated beside the entity and
// converts each element with a type assertion; the methods here are the ones
// that know the items are rows of a table: keyed by their key, reloaded from
// their table, hidden and appended per row.
type Rows []Entity

// models returns the model of every row the framework built, in order, and
// leaves out a row written as a literal, which has none.
func (rows Rows) models() []*Model {
	out := make([]*Model, 0, len(rows))
	for _, e := range rows {
		if m := modelOf(e); live(m) {
			out = append(out, m)
		}
	}
	return out
}

// modelsOrFail returns the model of every row, and refuses when a row cannot be
// written to: a relation load attaches to every row, and attaching to a literal
// or a copy would report success having loaded onto nothing.
func (rows Rows) modelsOrFail() ([]*Model, error) {
	out := make([]*Model, 0, len(rows))
	for _, e := range rows {
		m := modelOf(e)
		if err := wired(m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// firstModel returns the model of the first row the framework built, or nil.
func (rows Rows) firstModel() *Model {
	for _, e := range rows {
		if m := modelOf(e); live(m) {
			return m
		}
	}
	return nil
}

// Count returns the number of rows.
func (rows Rows) Count() int { return len(rows) }

// IsEmpty reports whether there are no rows.
func (rows Rows) IsEmpty() bool { return len(rows) == 0 }

// IsNotEmpty reports the opposite of IsEmpty.
func (rows Rows) IsNotEmpty() bool { return len(rows) > 0 }

// First returns the first row, or nil when there is none.
func (rows Rows) First() Entity {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// ModelKeys returns the primary key of every row.
func (rows Rows) ModelKeys() []any {
	found := rows.models()
	out := make([]any, 0, len(found))
	for _, m := range found {
		out = append(out, m.GetKey())
	}
	return out
}

// Find returns the row with this key, out of the ones already in hand, or nil.
func (rows Rows) Find(key any) Entity {
	if i := rows.indexOf(key); i >= 0 {
		return rows[i]
	}
	return nil
}

// indexOf is the position of the row with this key, or -1.
func (rows Rows) indexOf(key any) int {
	for i, e := range rows {
		if m := modelOf(e); live(m) && equalValues(m.GetKey(), key) {
			return i
		}
	}
	return -1
}

// FindOrFail returns the row with this key, or an error naming the table and
// the key when none matches.
func (rows Rows) FindOrFail(key any) (Entity, error) {
	if found := rows.Find(key); found != nil {
		return found, nil
	}
	table := ""
	if first := rows.firstModel(); first != nil {
		table = first.r.table.name
	}
	return nil, modelNotFound(table, key)
}

// Contains reports whether key -- a key value, or a row to compare by key --
// matches one of the rows.
func (rows Rows) Contains(key any) bool {
	if other, ok := key.(Entity); ok {
		o := modelOf(other)
		if !live(o) {
			return false
		}
		for _, m := range rows.models() {
			if sameRow(m, o) {
				return true
			}
		}
		return false
	}
	return rows.indexOf(key) >= 0
}

// DoesntContain reports the opposite of Contains.
func (rows Rows) DoesntContain(key any) bool { return !rows.Contains(key) }

// Pluck returns one attribute of every row, as a slice of any: the value is
// whatever that column holds.
func (rows Rows) Pluck(column string) []any {
	found := rows.models()
	out := make([]any, 0, len(found))
	for _, m := range found {
		out = append(out, m.GetAttribute(column))
	}
	return out
}

// Merge returns the other rows added, with a key that is already here replaced
// rather than repeated.
func (rows Rows) Merge(items Rows) Rows {
	out := make(Rows, 0, len(rows)+len(items))
	out = append(out, rows...)
	for _, e := range items {
		m := modelOf(e)
		if !live(m) {
			continue
		}
		if i := out.indexOf(m.GetKey()); i >= 0 {
			out[i] = e
			continue
		}
		out = append(out, e)
	}
	return out
}

// Diff returns the rows that are not in items.
func (rows Rows) Diff(items Rows) Rows {
	out := Rows{}
	for _, m := range rows.models() {
		if items.indexOf(m.GetKey()) < 0 {
			out = append(out, m.r.self)
		}
	}
	return out
}

// Intersect returns the rows that are also in items.
func (rows Rows) Intersect(items Rows) Rows {
	out := Rows{}
	if len(items) == 0 {
		return out
	}
	for _, m := range rows.models() {
		if items.indexOf(m.GetKey()) >= 0 {
			out = append(out, m.r.self)
		}
	}
	return out
}

// Unique returns one row per key, the first one seen.
func (rows Rows) Unique() Rows {
	out := Rows{}
	for _, m := range rows.models() {
		if out.indexOf(m.GetKey()) < 0 {
			out = append(out, m.r.self)
		}
	}
	return out
}

// Only returns the rows with these keys.
func (rows Rows) Only(keys ...any) Rows {
	out := Rows{}
	for _, m := range rows.models() {
		if containsValue(keys, m.GetKey()) {
			out = append(out, m.r.self)
		}
	}
	return out
}

// Except returns the rows without these keys.
func (rows Rows) Except(keys ...any) Rows {
	out := Rows{}
	for _, m := range rows.models() {
		if !containsValue(keys, m.GetKey()) {
			out = append(out, m.r.self)
		}
	}
	return out
}

// Load eager loads these relations onto every row.
//
// Every row has to be one the framework built: a literal has no model to
// attach a relation to, and the load refuses with ErrUnwired rather than
// quietly attaching nothing.
func (rows Rows) Load(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 || len(rows) == 0 {
		return nil
	}
	if _, err := rows.modelsOrFail(); err != nil {
		return err
	}
	return loadModels(ctx, g, rows, relations...)
}

// LoadMissing eager loads these relations onto every row, skipping the rows
// that already have them. See Load for the rows it takes.
func (rows Rows) LoadMissing(ctx context.Context, g auth.Grant, relations ...string) error {
	if len(relations) == 0 || len(rows) == 0 {
		return nil
	}
	if _, err := rows.modelsOrFail(); err != nil {
		return err
	}
	return loadMissingModels(ctx, g, rows, relations...)
}

// LoadAggregate loads function over column of each relation onto every row, as
// a raw attribute named the way WithAggregate names it. See Load for the rows it
// takes.
func (rows Rows) LoadAggregate(ctx context.Context, g auth.Grant, relations []string, column, function string) error {
	if len(relations) == 0 || len(rows) == 0 {
		return nil
	}
	if _, err := rows.modelsOrFail(); err != nil {
		return err
	}
	return loadAggregateModels(ctx, g, rows, relations, column, function)
}

// LoadCount loads the count of each relation onto every row.
func (rows Rows) LoadCount(ctx context.Context, g auth.Grant, relations ...string) error {
	return rows.LoadAggregate(ctx, g, relations, "*", "count")
}

// Fresh returns the same rows, read again, with the named relations loaded.
//
// A row that has since been deleted drops out of the result.
func (rows Rows) Fresh(ctx context.Context, g auth.Grant, with ...string) (Rows, error) {
	found := rows.models()
	if len(found) == 0 {
		return Rows{}, nil
	}
	first := found[0]
	fresh, err := newModelQuery(first).
		With(with...).
		WhereKey(rows.ModelKeys()).
		get(ctx, g)
	if err != nil {
		return nil, err
	}

	out := make(Rows, 0, len(found))
	for _, m := range found {
		if i := fresh.indexOf(m.GetKey()); i >= 0 && m.r.exists {
			out = append(out, fresh[i])
		}
	}
	return out, nil
}

// MakeVisible calls MakeVisible on every row.
func (rows Rows) MakeVisible(attributes ...string) Rows {
	for _, m := range rows.models() {
		m.MakeVisible(attributes...)
	}
	return rows
}

// MakeHidden calls MakeHidden on every row.
func (rows Rows) MakeHidden(attributes ...string) Rows {
	for _, m := range rows.models() {
		m.MakeHidden(attributes...)
	}
	return rows
}

// ToQuery returns a query over exactly these rows, on the table and connection
// of the first.
//
// No rows is ErrEmptyCollection: with no row there is no table to query.
func (rows Rows) ToQuery() (*Builder, error) {
	first := rows.firstModel()
	if first == nil {
		return nil, ErrEmptyCollection
	}
	return newModelQuery(first).WhereKey(rows.ModelKeys()), nil
}

// ToArray returns every row, serialised.
func (rows Rows) ToArray() []map[string]any {
	found := rows.models()
	out := make([]map[string]any, 0, len(found))
	for _, m := range found {
		out = append(out, m.ToArray())
	}
	return out
}

// Push calls Push on every row, which is what makes a loaded relation
// pushable.
func (rows Rows) Push(ctx context.Context, g auth.Grant) (bool, error) {
	for _, e := range rows {
		pushed, err := modelOf(e).Push(ctx, g)
		if err != nil || !pushed {
			return false, err
		}
	}
	return true, nil
}

// loadModels eager loads these relations onto every row, through a query on the
// first row's table and connection.
func loadModels(ctx context.Context, g auth.Grant, rows Rows, relations ...string) error {
	first := rows.firstModel()
	if first == nil || len(relations) == 0 {
		return nil
	}
	q := newQuery(first).With(relations...)
	return eagerLoadRelations(q, ctx, g, rows)
}

// loadMissingModels eager loads these relations onto every row, skipping the
// rows that already have them.
func loadMissingModels(ctx context.Context, g auth.Grant, rows Rows, relations ...string) error {
	for _, relation := range relations {
		missing := make(Rows, 0, len(rows))
		for _, e := range rows {
			if m := modelOf(e); live(m) && !relationLoaded(m, relation) {
				missing = append(missing, e)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if err := loadModels(ctx, g, missing, relation); err != nil {
			return err
		}
	}
	return nil
}

// loadAggregateModels loads function over column of each relation onto every
// row.
//
// It reads the aggregate columns for the keys already in hand and force fills
// them onto the rows. The columns are not declared on the entity, so they land
// as raw attributes.
func loadAggregateModels(ctx context.Context, g auth.Grant, rows Rows, relations []string, column, function string) error {
	first := rows.firstModel()
	if first == nil || len(relations) == 0 {
		return nil
	}
	aggregates, err := aggregateQuery(newModelQuery(first), ctx, g, rows.ModelKeys(), relations, column, function)
	if err != nil {
		return err
	}

	keyName := first.r.table.keyName
	for _, m := range rows.models() {
		i := aggregates.indexOf(m.GetKey())
		if i < 0 {
			continue
		}
		extra := map[string]any{}
		names := make([]string, 0)
		for key, value := range modelOf(aggregates[i]).GetAttributes() {
			if key == keyName {
				continue
			}
			if _, isField := m.r.table.schema.byName[key]; isField {
				continue
			}
			extra[key] = value
			names = append(names, key)
		}
		if err := m.ForceFill(extra); err != nil {
			return err
		}
		slices.Sort(names)
		syncOriginalAttributes(m, names...)
	}
	return nil
}

// equalValues is how two keys are compared: by value, whatever their type.
func equalValues(a, b any) bool { return reflect.DeepEqual(a, b) }

func containsValue(values []any, value any) bool {
	for _, candidate := range values {
		if equalValues(candidate, value) {
			return true
		}
	}
	return false
}
