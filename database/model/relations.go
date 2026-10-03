package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/database/model/relations"
	"github.com/arandu-io/hesape/str"
)

// The relation factories: one function per relation, from the model it starts
// at to the table it reaches.
//
// They are functions rather than methods because a relation needs both ends,
// and a method has one receiver. The related end is a *Table rather than a row,
// because what a relation reads is a table: the query is built on it, through
// the connection the parent came from.
//
// An empty key means the convention: user_id from a User, id from the parent,
// role_user from a Role and a User sorted so the intermediate table is the same
// table read from either side. Naming one is for the schema that does not follow
// it, and naming one that matches the convention is noise.
//
// # Constrained or not is the parent's to say
//
// A relation read off one row is narrowed to it: the posts of this user. A
// relation the eager loader resolves is not: it is narrowed to the whole batch
// afterwards, and a narrowing already on the query would be one for a parent
// that does not exist -- `where user_id = 0 and user_id in (1, 2)`, which
// matches nothing and reads as a parent with no children.
//
// The difference is the parent. A RelationFunc registered with Relate is called
// with the prototype a query runs through, which stands for no row, and every
// factory here builds the unconstrained relation from a prototype and the
// constrained one from a row. The choice is carried by the value the relation
// is built from, so a relation built on another goroutine at the same moment
// cannot come back with the other answer.
//
// # Reading the result back
//
// A relation loads the narrow interface the relations tree consumes, and
// Model.Related hands the loaded rows back as Rows, which the accessor
// generated beside the entity converts to its own type.

// HasOne returns a has-one from parent to related.
func HasOne(parent *Model, related *Table, foreignKey, localKey string) *relations.HasOne {
	p, q, c := relationEnds(parent, related, "HasOne")
	foreignKey, localKey = defaultHasKeys(p, foreignKey, localKey)
	if parent.r.prototype {
		return relations.NewHasOneUnconstrained(q, p, c.QualifyColumn(foreignKey), localKey)
	}
	return relations.NewHasOne(q, p, c.QualifyColumn(foreignKey), localKey)
}

// HasMany returns a has-many from parent to related.
func HasMany(parent *Model, related *Table, foreignKey, localKey string) *relations.HasMany {
	p, q, c := relationEnds(parent, related, "HasMany")
	foreignKey, localKey = defaultHasKeys(p, foreignKey, localKey)
	if parent.r.prototype {
		return relations.NewHasManyUnconstrained(q, p, c.QualifyColumn(foreignKey), localKey)
	}
	return relations.NewHasMany(q, p, c.QualifyColumn(foreignKey), localKey)
}

// BelongsTo returns a belongs-to from child to related.
//
// relation is the name the relation is read under, and it is required rather
// than guessed from the call site. It is not decoration: associate and
// dissociate write the loaded relation under it, and the error for a missing key
// names it.
func BelongsTo(child *Model, related *Table, foreignKey, ownerKey, relation string) *relations.BelongsTo {
	c, q, p := relationEnds(child, related, "BelongsTo")
	foreignKey, ownerKey = defaultBelongsToKeys(p, foreignKey, ownerKey, relation)
	if child.r.prototype {
		return relations.NewBelongsToUnconstrained(q, c, foreignKey, ownerKey, relation)
	}
	return relations.NewBelongsTo(q, c, foreignKey, ownerKey, relation)
}

// BelongsToMany returns a many-to-many from parent to related.
//
// An empty table is the conventional intermediate name, which is the two morph
// classes snake cased, sorted, joined by an underscore. The join onto the
// intermediate table is there constrained or not: it is how the related table is
// reached at all, for one parent or for a hundred.
func BelongsToMany(parent *Model, related *Table, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation string) *relations.BelongsToMany {
	p, q, c := relationEnds(parent, related, "BelongsToMany")
	table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey = defaultPivotKeys(p, c, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey)
	if parent.r.prototype {
		return relations.NewBelongsToManyUnconstrained(q, p, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation)
	}
	return relations.NewBelongsToMany(q, p, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation)
}

// MorphOne returns a morph-one from parent to related.
func MorphOne(parent *Model, related *Table, name, typ, id, localKey string) *relations.MorphOne {
	p, q, c := relationEnds(parent, related, "MorphOne")
	typ, id, localKey = defaultMorphKeys(p, name, typ, id, localKey)
	if parent.r.prototype {
		return relations.NewMorphOneUnconstrained(q, p, c.QualifyColumn(typ), c.QualifyColumn(id), localKey)
	}
	return relations.NewMorphOne(q, p, c.QualifyColumn(typ), c.QualifyColumn(id), localKey)
}

// MorphMany returns a morph-many from parent to related.
func MorphMany(parent *Model, related *Table, name, typ, id, localKey string) *relations.MorphMany {
	p, q, c := relationEnds(parent, related, "MorphMany")
	typ, id, localKey = defaultMorphKeys(p, name, typ, id, localKey)
	if parent.r.prototype {
		return relations.NewMorphManyUnconstrained(q, p, c.QualifyColumn(typ), c.QualifyColumn(id), localKey)
	}
	return relations.NewMorphMany(q, p, c.QualifyColumn(typ), c.QualifyColumn(id), localKey)
}

// MorphTo returns a morph-to on parent.
//
// It has no related table: the table it reads is resolved from the type column,
// through the morph map in model/relations, per row. The query it starts from is
// the parent's own, which is only where the connection comes from.
func MorphTo(parent *Model, name, typ, id, ownerKey string) *relations.MorphTo {
	mustLive(parent, "MorphTo")
	p := refOf(parent)
	q := parent.r.table.query(parent.r.conn, true).Ref()
	typ, id = GetMorphs(str.Snake(name, "_"), typ, id)
	if parent.r.prototype {
		return relations.NewMorphToUnconstrained(q, p, id, ownerKey, typ, name)
	}
	return relations.NewMorphTo(q, p, id, ownerKey, typ, name)
}

// MorphToMany returns a polymorphic many-to-many from parent to related.
func MorphToMany(parent *Model, related *Table, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation string, inverse bool) *relations.MorphToMany {
	p, q, c := relationEnds(parent, related, "MorphToMany")
	table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey = defaultMorphPivotKeys(p, c, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey)
	if parent.r.prototype {
		return relations.NewMorphToManyUnconstrained(q, p, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation, inverse)
	}
	return relations.NewMorphToMany(q, p, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation, inverse)
}

// MorphedByMany returns the other side of a MorphToMany, where parent is what
// the intermediate table points at.
func MorphedByMany(parent *Model, related *Table, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation string) *relations.MorphToMany {
	mustLive(parent, "MorphedByMany")
	if foreignPivotKey == "" {
		foreignPivotKey = parent.r.table.foreignKey
	}
	if relatedPivotKey == "" {
		relatedPivotKey = name + "_id"
	}
	return MorphToMany(parent, related, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey, relation, true)
}

// HasManyThrough returns a has-many-through from farParent to related, by way of
// through. The join onto the intermediate table is there constrained or not, for
// the reason BelongsToMany keeps its own.
func HasManyThrough(farParent *Model, through, related *Table, firstKey, secondKey, localKey, secondLocalKey string) *relations.HasManyThrough {
	f, q, _ := relationEnds(farParent, related, "HasManyThrough")
	t := refOf(through.newModel(farParent.r.conn))
	firstKey, secondKey, localKey, secondLocalKey = defaultThroughKeys(f, t, firstKey, secondKey, localKey, secondLocalKey)
	if farParent.r.prototype {
		return relations.NewHasManyThroughUnconstrained(q, f, t, firstKey, secondKey, localKey, secondLocalKey)
	}
	return relations.NewHasManyThrough(q, f, t, firstKey, secondKey, localKey, secondLocalKey)
}

// HasOneThrough returns a has-one-through from farParent to related, by way of
// through.
func HasOneThrough(farParent *Model, through, related *Table, firstKey, secondKey, localKey, secondLocalKey string) *relations.HasOneThrough {
	f, q, _ := relationEnds(farParent, related, "HasOneThrough")
	t := refOf(through.newModel(farParent.r.conn))
	firstKey, secondKey, localKey, secondLocalKey = defaultThroughKeys(f, t, firstKey, secondKey, localKey, secondLocalKey)
	if farParent.r.prototype {
		return relations.NewHasOneThroughUnconstrained(q, f, t, firstKey, secondKey, localKey, secondLocalKey)
	}
	return relations.NewHasOneThrough(q, f, t, firstKey, secondKey, localKey, secondLocalKey)
}

// relationEnds returns the three things every factory starts from: the parent as
// a relation takes it, a query on the related table through the parent's
// connection, and the model that query runs through.
func relationEnds(parent *Model, related *Table, factory string) (relations.Model, relations.Builder, relations.Model) {
	mustLive(parent, factory)
	if related == nil {
		panic(fmt.Sprintf("model: %s was given no related table", factory))
	}
	q := related.query(parent.r.conn, true).Ref()
	return refOf(parent), q, q.GetModel()
}

// mustLive panics when a relation is asked of a row the framework did not
// build: the relation reads the table and the connection off the row, and a
// literal has neither. It is a panic rather than an error because the factories
// are called while declaring a relation, where there is no error to return.
func mustLive(parent *Model, factory string) {
	if !live(parent) {
		panic(fmt.Sprintf("model: %s on a row the framework did not build: %v", factory, ErrUnwired))
	}
}

// GetMorphs returns the pair of column names a polymorphic relation reads.
func GetMorphs(name, typ, id string) (string, string) {
	if typ == "" {
		typ = name + "_type"
	}
	if id == "" {
		id = name + "_id"
	}
	return typ, id
}

// JoiningTable returns the conventional name of an intermediate table, the two
// model names in alphabetical order.
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

// The conventions, one function each. Two copies of a rule that picks a column
// name is two schemas one refactor apart.

func defaultHasKeys(parent relations.Model, foreignKey, localKey string) (string, string) {
	if foreignKey == "" {
		foreignKey = parent.GetForeignKey()
	}
	if localKey == "" {
		localKey = parent.GetKeyName()
	}
	return foreignKey, localKey
}

func defaultBelongsToKeys(related relations.Model, foreignKey, ownerKey, relation string) (string, string) {
	if foreignKey == "" {
		foreignKey = str.Snake(relation, "_") + "_" + related.GetKeyName()
	}
	if ownerKey == "" {
		ownerKey = related.GetKeyName()
	}
	return foreignKey, ownerKey
}

func defaultMorphKeys(parent relations.Model, name, typ, id, localKey string) (string, string, string) {
	typ, id = GetMorphs(name, typ, id)
	if localKey == "" {
		localKey = parent.GetKeyName()
	}
	return typ, id, localKey
}

func defaultPivotKeys(parent, related relations.Model, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey string) (string, string, string, string, string) {
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
	return table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey
}

func defaultMorphPivotKeys(parent, related relations.Model, name, table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey string) (string, string, string, string, string) {
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
	return table, foreignPivotKey, relatedPivotKey, parentKey, relatedKey
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
