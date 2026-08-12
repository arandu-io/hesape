package concerns

import "fmt"

// AsPivot answers the trait of the same name: what a model gains by being the
// row of an intermediate table.
//
// The two static constructors the PHP trait carries -- fromAttributes and
// fromRawAttributes -- are not here. They call `new static`, which is late
// static binding, and the type they have to build is the concrete pivot; they
// live on relations.Pivot and relations.MorphPivot, where the type is known.
//
// A pivot row is not the same shape as an ordinary model, and the trait exists
// for that difference: the table has no id of its own, so a save cannot be keyed
// by one. setKeysForSaveQuery keys it by the two foreign keys instead, which is
// why they are held here rather than guessed at write time.
type AsPivot struct {
	// PivotParent is AsPivot::$pivotParent: the model the relation was declared
	// on. It answers where the timestamp column names come from.
	PivotParent Model

	// PivotRelated is AsPivot::$pivotRelated.
	PivotRelated Model

	foreignKey string
	relatedKey string
}

// SetPivotKeys answers AsPivot::setPivotKeys.
func (p *AsPivot) SetPivotKeys(foreignKey, relatedKey string) *AsPivot {
	p.foreignKey = foreignKey
	p.relatedKey = relatedKey
	return p
}

// SetRelatedModel answers AsPivot::setRelatedModel.
func (p *AsPivot) SetRelatedModel(related Model) *AsPivot {
	p.PivotRelated = related
	return p
}

// GetForeignKey answers AsPivot::getForeignKey.
func (p *AsPivot) GetForeignKey() string { return p.foreignKey }

// GetRelatedKey answers AsPivot::getRelatedKey.
func (p *AsPivot) GetRelatedKey() string { return p.relatedKey }

// GetOtherKey answers AsPivot::getOtherKey, the alias of GetRelatedKey.
func (p *AsPivot) GetOtherKey() string { return p.GetRelatedKey() }

// HasTimestampAttributes answers AsPivot::hasTimestampAttributes.
//
// A pivot table carries timestamps only when the developer said withTimestamps,
// so the question is answered by looking for the column in the row rather than
// by a flag on the class.
func (p *AsPivot) HasTimestampAttributes(attributes map[string]any) bool {
	created := p.GetCreatedAtColumn()
	if created == "" {
		return false
	}
	_, ok := attributes[created]
	return ok
}

// GetCreatedAtColumn answers AsPivot::getCreatedAtColumn: the parent's, because
// the pivot has no configuration of its own.
func (p *AsPivot) GetCreatedAtColumn() string {
	if p.PivotParent != nil {
		return p.PivotParent.GetCreatedAtColumn()
	}
	return "created_at"
}

// GetUpdatedAtColumn answers AsPivot::getUpdatedAtColumn.
func (p *AsPivot) GetUpdatedAtColumn() string {
	if p.PivotParent != nil {
		return p.PivotParent.GetUpdatedAtColumn()
	}
	return "updated_at"
}

// SetKeysForSelectQuery answers AsPivot::setKeysForSelectQuery.
//
// It is what makes a pivot row addressable without an id: the pair of foreign
// keys is the key. The values come from the original attributes rather than the
// current ones, so changing which role a row points at still updates the row it
// came from instead of a row that does not exist yet.
func (p *AsPivot) SetKeysForSelectQuery(query Builder, model Model) Builder {
	if _, ok := model.GetAttributes()[model.GetKeyName()]; ok {
		return query.Where(model.GetKeyName(), model.GetKey())
	}

	return query.
		Where(p.foreignKey, model.GetAttribute(p.foreignKey)).
		Where(p.relatedKey, model.GetAttribute(p.relatedKey))
}

// GetQueueableID answers AsPivot::getQueueableId: what a queued job stores so
// that it can find this row again.
//
// A pivot row usually has no id column, so there is nothing to store; the pair
// of foreign keys is written out instead, in the PHP's own
// "foreign:value:related:value" shape. The row is passed in because the trait
// reads $this->attributes in PHP and this struct holds no attributes -- the
// same reason SetKeysForSelectQuery takes it.
//
// The name carries the initialism in upper case, which is the one mechanical
// change ADR 0044 allows without a note.
func (p *AsPivot) GetQueueableID(model Model) any {
	if _, ok := model.GetAttributes()[model.GetKeyName()]; ok {
		return model.GetKey()
	}
	return fmt.Sprintf("%s:%v:%s:%v",
		p.foreignKey, model.GetAttribute(p.foreignKey),
		p.relatedKey, model.GetAttribute(p.relatedKey))
}

// SetKeysForSaveQuery answers AsPivot::setKeysForSaveQuery.
func (p *AsPivot) SetKeysForSaveQuery(query Builder, model Model) Builder {
	return p.SetKeysForSelectQuery(query, model)
}

// UnsetRelations answers AsPivot::unsetRelations. The parent and the related
// model are dropped with the relations, because a pivot holds them as ordinary
// references and a serialized pivot would otherwise drag both models with it.
func (p *AsPivot) UnsetRelations() {
	p.PivotParent = nil
	p.PivotRelated = nil
}
