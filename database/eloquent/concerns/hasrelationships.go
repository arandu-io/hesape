package concerns

import (
	"sort"
	"strings"

	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/str"
)

// HasRelationships answers
// Illuminate\Database\Eloquent\Concerns\HasRelationships: the loaded-relations
// bag on the model, and the factories that build a relation from a pair of
// models.
//
// The factories are functions rather than methods because they need both ends
// -- PHP reaches the second through `new $related`, and a class name is not a
// type here. What they keep from the PHP is the guessing: pass the empty string
// for a key and the convention fills it in, which is the PHP's null.
type HasRelationships struct {
	// MorphClass is the alias this model is registered under in the morph map.
	// It is what GetMorphClass answers and what a *_type column holds.
	MorphClass string

	relations map[string]any
	touches   []string
	declared  []string
	resolvers map[string]func(any) relations.Relation
}

// GetRelations answers HasRelationships::getRelations.
func (h *HasRelationships) GetRelations() map[string]any {
	if h.relations == nil {
		h.relations = map[string]any{}
	}
	return h.relations
}

// GetRelation answers HasRelationships::getRelation.
//
// The second result reports whether it was loaded, where the PHP throws
// RelationNotFoundException. The difference matters here more than there: a
// relation that was never loaded and a relation that is legitimately empty are
// the same value in Go, and only this flag tells them apart.
func (h *HasRelationships) GetRelation(relation string) (any, bool) {
	value, ok := h.GetRelations()[relation]
	return value, ok
}

// RelationLoaded answers HasRelationships::relationLoaded.
func (h *HasRelationships) RelationLoaded(relation string) bool {
	_, ok := h.GetRelations()[relation]
	return ok
}

// SetRelation answers HasRelationships::setRelation.
func (h *HasRelationships) SetRelation(relation string, value any) {
	h.GetRelations()[relation] = value
}

// UnsetRelation answers HasRelationships::unsetRelation.
func (h *HasRelationships) UnsetRelation(relation string) { delete(h.GetRelations(), relation) }

// SetRelations answers HasRelationships::setRelations.
func (h *HasRelationships) SetRelations(values map[string]any) {
	h.relations = map[string]any{}
	for name, value := range values {
		h.relations[name] = value
	}
}

// UnsetRelations answers HasRelationships::unsetRelations.
func (h *HasRelationships) UnsetRelations() { h.relations = map[string]any{} }

// GetTouchedRelations answers HasRelationships::getTouchedRelations.
func (h *HasRelationships) GetTouchedRelations() []string { return h.touches }

// SetTouchedRelations answers HasRelationships::setTouchedRelations.
func (h *HasRelationships) SetTouchedRelations(touches []string) { h.touches = touches }

// Touches answers HasRelationships::touches.
func (h *HasRelationships) Touches(relation string) bool { return contains(h.touches, relation) }

// GetMorphClass answers HasRelationships::getMorphClass.
func (h *HasRelationships) GetMorphClass() string { return h.MorphClass }

// GetMorphs answers HasRelationships::getMorphs: the pair of column names a
// polymorphic relation reads.
func GetMorphs(name, typ, id string) (string, string) {
	if typ == "" {
		typ = name + "_type"
	}
	if id == "" {
		id = name + "_id"
	}
	return typ, id
}

// JoiningTable answers HasRelationships::joiningTable: the conventional name of
// an intermediate table, the two model names in alphabetical order.
//
// Alphabetical is what makes it the same table from both sides -- role_user
// whether you start at the user or at the role -- and it is why a many-to-many
// declared on both models needs no configuration at all.
func JoiningTable(parent, related relations.Model) string {
	segments := []string{
		str.Snake(parent.GetMorphClass(), "_"),
		str.Snake(related.GetMorphClass(), "_"),
	}
	sort.Strings(segments)
	return strings.Join(segments, "_")
}

// HasOne answers HasRelationships::hasOne.
//
// An empty foreignKey is the PHP's null: the parent's conventional foreign key,
// user_id for a model whose alias is user.
func HasOne(parent, related relations.Model, foreignKey, localKey string) *relations.HasOne {
	foreignKey, localKey = defaultHasKeys(parent, foreignKey, localKey)
	return relations.NewHasOne(related.NewQuery(), parent, related.QualifyColumn(foreignKey), localKey)
}

// HasMany answers HasRelationships::hasMany.
func HasMany(parent, related relations.Model, foreignKey, localKey string) *relations.HasMany {
	foreignKey, localKey = defaultHasKeys(parent, foreignKey, localKey)
	return relations.NewHasMany(related.NewQuery(), parent, related.QualifyColumn(foreignKey), localKey)
}

// BelongsTo answers HasRelationships::belongsTo.
//
// relation is the name the relation is read under, and it is required where the
// PHP guesses it from the calling method's name through debug_backtrace. It is
// not decoration: associate and dissociate write the loaded relation under it.
func BelongsTo(child, related relations.Model, foreignKey, ownerKey, relation string) *relations.BelongsTo {
	if foreignKey == "" {
		foreignKey = str.Snake(relation, "_") + "_" + related.GetKeyName()
	}
	if ownerKey == "" {
		ownerKey = related.GetKeyName()
	}
	return relations.NewBelongsTo(related.NewQuery(), child, foreignKey, ownerKey, relation)
}

// MorphOne answers HasRelationships::morphOne.
func MorphOne(parent, related relations.Model, name, typ, id, localKey string) *relations.MorphOne {
	typ, id = GetMorphs(name, typ, id)
	if localKey == "" {
		localKey = parent.GetKeyName()
	}
	return relations.NewMorphOne(related.NewQuery(), parent, related.QualifyColumn(typ), related.QualifyColumn(id), localKey)
}

// MorphMany answers HasRelationships::morphMany.
func MorphMany(parent, related relations.Model, name, typ, id, localKey string) *relations.MorphMany {
	typ, id = GetMorphs(name, typ, id)
	if localKey == "" {
		localKey = parent.GetKeyName()
	}
	return relations.NewMorphMany(related.NewQuery(), parent, related.QualifyColumn(typ), related.QualifyColumn(id), localKey)
}

// MorphTo answers HasRelationships::morphTo.
//
// related is the model whose query the relation starts from. In PHP there is
// none until the type column is read, and the builder is built per type inside
// getEager; the same happens here -- this one only carries a connection and is
// replaced per type -- but Go needs something concrete to start with.
func MorphTo(parent, related relations.Model, name, typ, id, ownerKey string) *relations.MorphTo {
	typ, id = GetMorphs(str.Snake(name, "_"), typ, id)
	return relations.NewMorphTo(related.NewQuery(), parent, id, ownerKey, typ, name)
}

// BelongsToMany answers HasRelationships::belongsToMany.
func BelongsToMany(parent, related relations.Model, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation string) *relations.BelongsToMany {
	if table == "" {
		table = JoiningTable(parent, related)
	}
	if foreignPivotKey == "" {
		foreignPivotKey = parent.GetForeignKey()
	}
	if relatedPivotKey == "" {
		relatedPivotKey = related.GetForeignKey()
	}
	if parentKey == "" {
		parentKey = parent.GetKeyName()
	}
	if relatedKey == "" {
		relatedKey = related.GetKeyName()
	}

	return relations.NewBelongsToMany(
		related.NewQuery(), parent, table,
		foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation,
	)
}

// MorphToMany answers HasRelationships::morphToMany.
func MorphToMany(parent, related relations.Model, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation string, inverse bool) *relations.MorphToMany {
	if table == "" {
		table = str.Plural(name)
	}
	if foreignPivotKey == "" {
		foreignPivotKey = name + "_id"
	}
	if relatedPivotKey == "" {
		relatedPivotKey = related.GetForeignKey()
	}
	if parentKey == "" {
		parentKey = parent.GetKeyName()
	}
	if relatedKey == "" {
		relatedKey = related.GetKeyName()
	}

	return relations.NewMorphToMany(
		related.NewQuery(), parent, name, table,
		foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation, inverse,
	)
}

// MorphedByMany answers HasRelationships::morphedByMany: the other side of a
// morphToMany, where this model is what the intermediate table points at.
func MorphedByMany(parent, related relations.Model, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation string) *relations.MorphToMany {
	if foreignPivotKey == "" {
		foreignPivotKey = parent.GetForeignKey()
	}
	if relatedPivotKey == "" {
		relatedPivotKey = name + "_id"
	}
	return MorphToMany(parent, related, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation, true)
}

// HasManyThrough answers HasRelationships::hasManyThrough.
func HasManyThrough(farParent, through, related relations.Model, firstKey, secondKey, localKey, secondLocalKey string) *relations.HasManyThrough {
	firstKey, secondKey, localKey, secondLocalKey = defaultThroughKeys(farParent, through, firstKey, secondKey, localKey, secondLocalKey)
	return relations.NewHasManyThrough(related.NewQuery(), farParent, through, firstKey, secondKey, localKey, secondLocalKey)
}

// HasOneThrough answers HasRelationships::hasOneThrough.
func HasOneThrough(farParent, through, related relations.Model, firstKey, secondKey, localKey, secondLocalKey string) *relations.HasOneThrough {
	firstKey, secondKey, localKey, secondLocalKey = defaultThroughKeys(farParent, through, firstKey, secondKey, localKey, secondLocalKey)
	return relations.NewHasOneThrough(related.NewQuery(), farParent, through, firstKey, secondKey, localKey, secondLocalKey)
}

func defaultHasKeys(parent relations.Model, foreignKey, localKey string) (string, string) {
	if foreignKey == "" {
		foreignKey = parent.GetForeignKey()
	}
	if localKey == "" {
		localKey = parent.GetKeyName()
	}
	return foreignKey, localKey
}

func defaultThroughKeys(farParent, through relations.Model, firstKey, secondKey, localKey, secondLocalKey string) (string, string, string, string) {
	if firstKey == "" {
		firstKey = farParent.GetForeignKey()
	}
	if secondKey == "" {
		secondKey = through.GetForeignKey()
	}
	if localKey == "" {
		localKey = farParent.GetKeyName()
	}
	if secondLocalKey == "" {
		secondLocalKey = through.GetKeyName()
	}
	return firstKey, secondKey, localKey, secondLocalKey
}
