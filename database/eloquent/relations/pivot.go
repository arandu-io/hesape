package relations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations/concerns"
)

// Pivot answers Illuminate\Database\Eloquent\Relations\Pivot: the row of an
// intermediate table, as a model.
//
// In PHP it is `class Pivot extends Model` with the AsPivot trait, so it
// inherits the whole model. There is no model to inherit here without importing
// the eloquent package, which imports this one -- so the model surface is
// implemented on the type, and it is the small surface a pivot actually needs:
// attributes, the two keys, a table and a save. What it is not is a second
// model implementation for general use; nothing but a pivot should embed it.
//
// $incrementing is false and $guarded is empty, exactly as on the PHP class: a
// pivot row has no id of its own to increment, and it is written by the
// relation rather than by mass assignment from a request.
type Pivot struct {
	concerns.AsPivot

	table      string
	attributes map[string]any
	original   map[string]any
	relations  map[string]any
	exists     bool
	timestamps bool

	// NewQueryFor is how a pivot reaches the database. The relation that built
	// it supplies the factory, because a pivot has no connection resolver of
	// its own -- and no facade to ask for one (ADR 0002).
	NewQueryFor func(table string) Builder
}

// FromAttributes answers AsPivot::fromAttributes.
//
// A static in PHP that calls `new static`. Go has no late static binding, so
// there is one function per pivot type rather than one method that knows which
// type it was called on -- MorphPivotFromAttributes is the other.
func FromAttributes(parent Model, attributes map[string]any, table string, exists bool) *Pivot {
	pivot := &Pivot{
		table:      table,
		attributes: map[string]any{},
		original:   map[string]any{},
		relations:  map[string]any{},
		exists:     exists,
	}
	pivot.PivotParent = parent
	pivot.timestamps = pivot.HasTimestampAttributes(attributes)

	pivot.ForceFill(attributes)
	pivot.SyncOriginal()
	return pivot
}

// FromRawAttributes answers AsPivot::fromRawAttributes.
func FromRawAttributes(parent Model, attributes map[string]any, table string, exists bool) *Pivot {
	pivot := FromAttributes(parent, nil, table, exists)
	pivot.timestamps = pivot.HasTimestampAttributes(attributes)
	pivot.SetRawAttributes(merge(pivot.original, attributes), exists)
	return pivot
}

// GetTable answers AsPivot::getTable.
func (p *Pivot) GetTable() string { return p.table }

// SetTable answers Model::setTable.
func (p *Pivot) SetTable(table string) { p.table = table }

// QualifyColumn answers Model::qualifyColumn.
func (p *Pivot) QualifyColumn(column string) string { return qualifyColumn(p.table, column) }

// GetKeyName answers Model::getKeyName.
func (p *Pivot) GetKeyName() string { return "id" }

// GetKeyType answers Model::getKeyType.
func (p *Pivot) GetKeyType() string { return "int" }

// GetKey answers Model::getKey.
func (p *Pivot) GetKey() any { return p.attributes[p.GetKeyName()] }

// GetForeignKey answers Model::getForeignKey.
func (p *Pivot) GetForeignKey() string { return strings.TrimSuffix(p.table, "s") + "_id" }

// GetMorphClass answers Model::getMorphClass: the table, because a pivot is not
// something a morph column ever points at.
func (p *Pivot) GetMorphClass() string { return p.table }

// Exists answers Model::$exists.
func (p *Pivot) Exists() bool { return p.exists }

// GetAttribute answers Model::getAttribute.
func (p *Pivot) GetAttribute(key string) any { return p.attributes[key] }

// SetAttribute answers Model::setAttribute.
func (p *Pivot) SetAttribute(key string, value any) { p.attributes[key] = value }

// GetAttributes answers Model::getAttributes.
func (p *Pivot) GetAttributes() map[string]any { return p.attributes }

// UnsetAttribute answers unset($pivot->$key).
func (p *Pivot) UnsetAttribute(key string) { delete(p.attributes, key) }

// SetRawAttributes answers Model::setRawAttributes.
func (p *Pivot) SetRawAttributes(attributes map[string]any, sync bool) {
	p.attributes = map[string]any{}
	for key, value := range attributes {
		p.attributes[key] = value
	}
	if sync {
		p.SyncOriginal()
	}
}

// SyncOriginal answers Model::syncOriginal.
func (p *Pivot) SyncOriginal() {
	p.original = make(map[string]any, len(p.attributes))
	for key, value := range p.attributes {
		p.original[key] = value
	}
}

// GetOriginal answers Model::getOriginal, which setKeysForSelectQuery reads so
// that a changed key still updates the row it came from.
func (p *Pivot) GetOriginal(key string) any {
	if value, ok := p.original[key]; ok {
		return value
	}
	return p.attributes[key]
}

// Fill answers Model::fill. A pivot's $guarded is empty, so it is ForceFill.
func (p *Pivot) Fill(attributes map[string]any) { p.ForceFill(attributes) }

// ForceFill answers Model::forceFill.
func (p *Pivot) ForceFill(attributes map[string]any) {
	for key, value := range attributes {
		p.attributes[key] = value
	}
}

// WasRecentlyCreated answers Model::$wasRecentlyCreated.
func (p *Pivot) WasRecentlyCreated() bool { return false }

// WithoutEvents answers Model::withoutEvents. A pivot fires none, so it runs the
// callback.
func (p *Pivot) WithoutEvents(callback func() error) error { return callback() }

// GetRelation answers Model::getRelation.
func (p *Pivot) GetRelation(relation string) (any, bool) {
	value, ok := p.relations[relation]
	return value, ok
}

// SetRelation answers Model::setRelation.
func (p *Pivot) SetRelation(relation string, value any) { p.relations[relation] = value }

// RelationLoaded answers Model::relationLoaded.
func (p *Pivot) RelationLoaded(relation string) bool {
	_, ok := p.relations[relation]
	return ok
}

// UnsetRelation answers Model::unsetRelation.
func (p *Pivot) UnsetRelation(relation string) { delete(p.relations, relation) }

// IsRelation answers Model::isRelation.
func (p *Pivot) IsRelation(key string) bool { return false }

// Touches answers Model::touches.
func (p *Pivot) Touches(relation string) bool { return false }

// UsesTimestamps answers Model::usesTimestamps.
func (p *Pivot) UsesTimestamps() bool { return p.timestamps }

// FreshTimestamp answers Model::freshTimestamp.
func (p *Pivot) FreshTimestamp() time.Time { return time.Now().UTC() }

// NewInstance answers Model::newInstance.
func (p *Pivot) NewInstance(attributes map[string]any) Model {
	instance := FromAttributes(p.PivotParent, attributes, p.table, false)
	instance.NewQueryFor = p.NewQueryFor
	instance.SetPivotKeys(p.GetForeignKey(), p.GetRelatedKey())
	return instance
}

// NewQuery answers Model::newQuery.
func (p *Pivot) NewQuery() Builder {
	if p.NewQueryFor == nil {
		return nil
	}
	return p.NewQueryFor(p.table)
}

// Save answers Model::save for a pivot row.
//
// The interesting half is which row it writes to. A pivot table usually has no
// id, so an update cannot be keyed by one; setKeysForSaveQuery keys it by the
// pair of foreign keys, taken from the original attributes. Without that, an
// update after changing one of the keys would write to a row that does not
// exist and report zero rows affected -- silently.
func (p *Pivot) Save(ctx context.Context, g auth.Grant) error {
	q := p.NewQuery()
	if q == nil {
		return errNoPivotQuery
	}

	if !p.exists {
		attributes := p.attributesForWrite(g)
		if err := q.Insert(ctx, g, []map[string]any{attributes}); err != nil {
			return err
		}
		p.exists = true
		p.SyncOriginal()
		return nil
	}

	scoped, err := concerns.ScopeTenant(q, p, g)
	if err != nil {
		return err
	}
	if _, err := p.SetKeysForSaveQuery(scoped, p).Update(ctx, g, p.attributes); err != nil {
		return err
	}
	p.SyncOriginal()
	return nil
}

// Delete answers AsPivot::delete.
func (p *Pivot) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	q := p.NewQuery()
	if q == nil {
		return 0, errNoPivotQuery
	}

	scoped, err := concerns.ScopeTenant(q, p, g)
	if err != nil {
		return 0, err
	}

	affected, err := p.getDeleteQuery(scoped).Delete(ctx, g)
	if err != nil {
		return 0, err
	}
	p.exists = false
	return affected, nil
}

// getDeleteQuery answers AsPivot::getDeleteQuery.
func (p *Pivot) getDeleteQuery(q Builder) Builder {
	return q.
		Where(p.GetForeignKey(), p.GetOriginal(p.GetForeignKey())).
		Where(p.GetRelatedKey(), p.GetOriginal(p.GetRelatedKey()))
}

// SetKeysForSaveQuery answers AsPivot::setKeysForSaveQuery, with the receiver
// the trait cannot reach in Go.
func (p *Pivot) SetKeysForSaveQuery(q Builder, model Model) Builder {
	return p.AsPivot.SetKeysForSaveQuery(q, model)
}

// attributesForWrite is the row as it goes to the database, with the tenant on
// it. RULE 14 is not only a where clause: a pivot row written without the
// tenant is a row every scoped read will miss.
func (p *Pivot) attributesForWrite(g auth.Grant) map[string]any {
	attributes := make(map[string]any, len(p.attributes)+1)
	for key, value := range p.attributes {
		attributes[key] = value
	}

	if column := concerns.TenantColumnFor(p); column != "" {
		if _, ok := attributes[column]; !ok {
			if tenant := auth.Tenant(g); tenant != "" {
				attributes[column] = tenant
			}
		}
	}
	return attributes
}

// Touch answers Model::touch.
func (p *Pivot) Touch(ctx context.Context, g auth.Grant) error {
	if !p.timestamps {
		return nil
	}
	p.SetAttribute(p.GetUpdatedAtColumn(), p.FreshTimestamp())
	return p.Save(ctx, g)
}

// GetQueueableID answers AsPivot::getQueueableId for a pivot row.
//
// PHP's trait reads $this->attributes directly; the concern here holds no
// attributes, so it is given the row. This is the no-argument method a caller
// reaches, and it hands itself over.
func (p *Pivot) GetQueueableID() any { return p.AsPivot.GetQueueableID(p) }

// NewQueryForRestoration answers AsPivot::newQueryForRestoration for a pivot
// row: the query that finds again what GetQueueableID wrote down.
//
// Like GetQueueableID above, this is the no-argument-model form a caller
// reaches, handing itself to the concern that holds no attributes.
func (p *Pivot) NewQueryForRestoration(ids ...any) (Builder, error) {
	return p.AsPivot.NewQueryForRestoration(p, ids...)
}

// MorphPivot answers Illuminate\Database\Eloquent\Relations\MorphPivot: a pivot
// row on a table shared by several parent types, so every statement it writes
// carries the type as well as the keys.
type MorphPivot struct {
	Pivot

	morphType  string
	morphClass string
}

// MorphPivotFromAttributes answers MorphPivot's inherited fromAttributes.
func MorphPivotFromAttributes(parent Model, attributes map[string]any, table string, exists bool) *MorphPivot {
	return &MorphPivot{Pivot: *FromAttributes(parent, attributes, table, exists)}
}

// SetMorphType answers MorphPivot::setMorphType.
func (p *MorphPivot) SetMorphType(morphType string) *MorphPivot {
	p.morphType = morphType
	return p
}

// SetMorphClass answers MorphPivot::setMorphClass.
func (p *MorphPivot) SetMorphClass(morphClass string) *MorphPivot {
	p.morphClass = morphClass
	return p
}

// GetMorphType answers MorphPivot::getMorphType.
func (p *MorphPivot) GetMorphType() string { return p.morphType }

// GetQueueableID answers MorphPivot::getQueueableId: the pair of keys of
// AsPivot, plus the type column and the alias it holds.
//
// Without the type half, two rows of the same intermediate table -- a post's
// tag and a video's tag, same tag id -- would serialize to the same identity
// and a queued job would restore the wrong one.
func (p *MorphPivot) GetQueueableID() any {
	if _, ok := p.GetAttributes()[p.GetKeyName()]; ok {
		return p.GetKey()
	}
	// AsPivot.GetForeignKey is the pivot column; Pivot.GetForeignKey is
	// Model::getForeignKey and shadows it, so the concern is named outright.
	foreign := p.AsPivot.GetForeignKey()
	related := p.GetRelatedKey()
	return fmt.Sprintf("%s:%v:%s:%v:%s:%s",
		foreign, p.GetAttribute(foreign),
		related, p.GetAttribute(related),
		p.morphType, p.morphClass)
}

// SetKeysForSaveQuery answers MorphPivot::setKeysForSaveQuery.
func (p *MorphPivot) SetKeysForSaveQuery(q Builder, model Model) Builder {
	return p.Pivot.SetKeysForSaveQuery(q.Where(p.morphType, p.morphClass), model)
}

// Delete answers MorphPivot::delete: the type column narrows the delete, or a
// post's tag row would be removed by a video that shares the tag id.
func (p *MorphPivot) Delete(ctx context.Context, g auth.Grant) (int64, error) {
	q := p.NewQuery()
	if q == nil {
		return 0, errNoPivotQuery
	}

	scoped, err := concerns.ScopeTenant(q, p, g)
	if err != nil {
		return 0, err
	}

	affected, err := p.getDeleteQuery(scoped).Where(p.morphType, p.morphClass).Delete(ctx, g)
	if err != nil {
		return 0, err
	}
	p.exists = false
	return affected, nil
}

// qualifyColumn answers Model::qualifyColumn for the types in this package.
func qualifyColumn(table, column string) string {
	if strings.Contains(column, ".") {
		return column
	}
	return table + "." + column
}
