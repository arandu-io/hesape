package relations

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations/concerns"
)

// ThroughKey is the alias the intermediate key is selected under.
//
// The PHP calls it laravel_through_key. The name is the framework's, not the
// user's, so it is this framework's here -- and it is a column name that comes
// back on every row of a has-many-through, which makes it the one string in
// this package a reader is guaranteed to meet in a result set.
const ThroughKey = "arandu_through_key"

// HasOneOrManyThrough answers the abstract
// Illuminate\Database\Eloquent\Relations\HasOneOrManyThrough: a relation that
// reaches its rows through a third table -- a country's posts, through its
// users.
//
// The intermediate table is joined rather than queried, so it is still two
// queries for an eager load and not three. What makes the eager load work is
// selecting the intermediate table's foreign key alongside the related row,
// under ThroughKey: without it the result set could not say which far parent
// each row came from, because the related table has no column that names it.
type HasOneOrManyThrough struct {
	BaseRelation

	throughParent  Model
	farParent      Model
	firstKey       string
	secondKey      string
	localKey       string
	secondLocalKey string
}

// NewHasOneOrManyThrough answers HasOneOrManyThrough::__construct.
func NewHasOneOrManyThrough(query Builder, farParent, throughParent Model, firstKey, secondKey, localKey, secondLocalKey string) HasOneOrManyThrough {
	return HasOneOrManyThrough{
		BaseRelation:   NewBaseRelation(query, throughParent),
		farParent:      farParent,
		throughParent:  throughParent,
		firstKey:       firstKey,
		secondKey:      secondKey,
		localKey:       localKey,
		secondLocalKey: secondLocalKey,
	}
}

// AddConstraints answers HasOneOrManyThrough::addConstraints.
func (r *HasOneOrManyThrough) AddConstraints() {
	r.performJoin(nil)

	if ConstraintsEnabled() {
		r.Query.Where(r.GetQualifiedFirstKeyName(), "=", r.farParent.GetAttribute(r.localKey))
	}
}

// performJoin answers HasOneOrManyThrough::performJoin.
func (r *HasOneOrManyThrough) performJoin(q Builder) {
	if q == nil {
		q = r.Query
	}
	q.Join(r.throughParent.GetTable(), r.GetQualifiedParentKeyName(), "=", r.GetQualifiedFarKeyName())
}

// AddEagerConstraints answers HasOneOrManyThrough::addEagerConstraints.
func (r *HasOneOrManyThrough) AddEagerConstraints(models []Model) error {
	keys, err := r.GetKeys(models, r.localKey)
	if err != nil {
		return err
	}
	r.WhereInEager(r.GetQualifiedFirstKeyName(), keys)
	return nil
}

// buildDictionary answers HasOneOrManyThrough::buildDictionary: keyed by the
// intermediate key that came back under ThroughKey.
func (r *HasOneOrManyThrough) buildDictionary(results []Model) (map[string][]Model, error) {
	dictionary := make(map[string][]Model, len(results))
	for _, result := range results {
		key, err := concerns.GetDictionaryKey(result.GetAttribute(ThroughKey))
		if err != nil {
			return nil, err
		}
		dictionary[key] = append(dictionary[key], result)
	}
	return dictionary, nil
}

// Get answers HasOneOrManyThrough::get: the select carries the intermediate key
// so that Match has something to key on.
func (r *HasOneOrManyThrough) Get(ctx context.Context, g auth.Grant, columns ...any) ([]Model, error) {
	scoped, err := concerns.ScopeTenant(r.Query.Clone(), r.Related, g)
	if err != nil {
		return nil, err
	}
	return scoped.AddSelect(r.shouldSelect(columns)...).Get(ctx, g)
}

// GetEager answers Relation::getEager for a through relation.
func (r *HasOneOrManyThrough) GetEager(ctx context.Context, g auth.Grant) ([]Model, error) {
	if r.EagerKeysWereEmpty {
		return []Model{}, nil
	}
	return r.Get(ctx, g)
}

// First answers the one-row read, with the same select as Get.
func (r *HasOneOrManyThrough) First(ctx context.Context, g auth.Grant) (Model, error) {
	scoped, err := concerns.ScopeTenant(r.Query.Clone(), r.Related, g)
	if err != nil {
		return nil, err
	}
	return scoped.AddSelect(r.shouldSelect(nil)...).First(ctx, g)
}

// shouldSelect answers HasOneOrManyThrough::shouldSelect.
func (r *HasOneOrManyThrough) shouldSelect(columns []any) []any {
	if len(columns) == 0 {
		columns = []any{r.Related.QualifyColumn("*")}
	}
	return append(columns, r.GetQualifiedFirstKeyName()+" as "+ThroughKey)
}

// Limit answers HasOneOrManyThrough::limit.
func (r *HasOneOrManyThrough) Limit(value int) *HasOneOrManyThrough {
	if r.farParent.Exists() {
		r.Query.Limit(value)
	} else {
		r.Query.GetQuery().GroupLimit(value, r.GetQualifiedFirstKeyName())
	}
	return r
}

// Take answers HasOneOrManyThrough::take.
func (r *HasOneOrManyThrough) Take(value int) *HasOneOrManyThrough { return r.Limit(value) }

// GetRelationExistenceQuery answers
// HasOneOrManyThrough::getRelationExistenceQuery.
func (r *HasOneOrManyThrough) GetRelationExistenceQuery(q Builder, parentQuery Builder, columns ...any) Builder {
	if len(columns) == 0 {
		columns = []any{"*"}
	}
	r.performJoin(q)

	return q.Select(columns...).WhereColumn(
		r.GetQualifiedLocalKeyName(), "=", r.GetQualifiedFirstKeyName(),
	)
}

// GetParent answers Relation::getParent. For a through relation the parent of
// the relation is the intermediate model, and the far parent is what declared
// it -- the PHP keeps both, and so does this.
func (r *HasOneOrManyThrough) GetFarParent() Model { return r.farParent }

// GetThroughParent answers HasOneOrManyThrough::$throughParent.
func (r *HasOneOrManyThrough) GetThroughParent() Model { return r.throughParent }

// GetQualifiedParentKeyName answers
// HasOneOrManyThrough::getQualifiedParentKeyName.
func (r *HasOneOrManyThrough) GetQualifiedParentKeyName() string {
	return r.throughParent.QualifyColumn(r.secondLocalKey)
}

// GetQualifiedFarKeyName answers HasOneOrManyThrough::getQualifiedFarKeyName.
func (r *HasOneOrManyThrough) GetQualifiedFarKeyName() string {
	return r.GetQualifiedForeignKeyName()
}

// GetFirstKeyName answers HasOneOrManyThrough::getFirstKeyName.
func (r *HasOneOrManyThrough) GetFirstKeyName() string { return r.firstKey }

// GetQualifiedFirstKeyName answers
// HasOneOrManyThrough::getQualifiedFirstKeyName.
func (r *HasOneOrManyThrough) GetQualifiedFirstKeyName() string {
	return r.throughParent.QualifyColumn(r.firstKey)
}

// GetForeignKeyName answers HasOneOrManyThrough::getForeignKeyName.
func (r *HasOneOrManyThrough) GetForeignKeyName() string { return r.secondKey }

// GetQualifiedForeignKeyName answers
// HasOneOrManyThrough::getQualifiedForeignKeyName.
func (r *HasOneOrManyThrough) GetQualifiedForeignKeyName() string {
	return r.Related.QualifyColumn(r.secondKey)
}

// GetLocalKeyName answers HasOneOrManyThrough::getLocalKeyName.
func (r *HasOneOrManyThrough) GetLocalKeyName() string { return r.localKey }

// GetQualifiedLocalKeyName answers
// HasOneOrManyThrough::getQualifiedLocalKeyName.
func (r *HasOneOrManyThrough) GetQualifiedLocalKeyName() string {
	return r.farParent.QualifyColumn(r.localKey)
}

// GetSecondLocalKeyName answers
// HasOneOrManyThrough::getSecondLocalKeyName.
func (r *HasOneOrManyThrough) GetSecondLocalKeyName() string { return r.secondLocalKey }

// GetParentKey answers the far parent's local key, which is what a through
// relation is narrowed by.
func (r *HasOneOrManyThrough) GetParentKey() any { return r.farParent.GetAttribute(r.localKey) }

// HasManyThrough answers
// Illuminate\Database\Eloquent\Relations\HasManyThrough.
type HasManyThrough struct {
	HasOneOrManyThrough
}

// NewHasManyThrough answers HasManyThrough's constructor.
func NewHasManyThrough(query Builder, farParent, throughParent Model, firstKey, secondKey, localKey, secondLocalKey string) *HasManyThrough {
	relation := &HasManyThrough{
		HasOneOrManyThrough: NewHasOneOrManyThrough(query, farParent, throughParent, firstKey, secondKey, localKey, secondLocalKey),
	}
	relation.AddConstraints()
	return relation
}

// GetResults answers HasManyThrough::getResults.
func (r *HasManyThrough) GetResults(ctx context.Context, g auth.Grant) (any, error) {
	if r.GetParentKey() == nil {
		return []Model{}, nil
	}
	return r.Get(ctx, g)
}

// InitRelation answers HasManyThrough::initRelation.
func (r *HasManyThrough) InitRelation(models []Model, relation string) []Model {
	for _, model := range models {
		model.SetRelation(relation, []Model{})
	}
	return models
}

// Match answers HasManyThrough::match.
func (r *HasManyThrough) Match(models []Model, results []Model, relation string) ([]Model, error) {
	dictionary, err := r.buildDictionary(results)
	if err != nil {
		return nil, err
	}

	for _, model := range models {
		value := model.GetAttribute(r.localKey)
		if value == nil {
			continue
		}
		key, err := concerns.GetDictionaryKey(value)
		if err != nil {
			return nil, err
		}
		if matched, ok := dictionary[key]; ok {
			model.SetRelation(relation, matched)
		}
	}

	return models, nil
}

// One answers HasManyThrough::one.
func (r *HasManyThrough) One() *HasOneThrough {
	var one *HasOneThrough
	NoConstraints(func() {
		q := r.Query
		q.GetQuery().Joins = nil
		one = NewHasOneThrough(q, r.farParent, r.throughParent, r.firstKey, r.secondKey, r.localKey, r.secondLocalKey)
	})
	return one
}

// HasOneThrough answers Illuminate\Database\Eloquent\Relations\HasOneThrough.
type HasOneThrough struct {
	HasOneOrManyThrough
	concerns.SupportsDefaultModels
	concerns.ComparesRelatedModels
}

// NewHasOneThrough answers HasOneThrough's constructor.
func NewHasOneThrough(query Builder, farParent, throughParent Model, firstKey, secondKey, localKey, secondLocalKey string) *HasOneThrough {
	relation := &HasOneThrough{
		HasOneOrManyThrough: NewHasOneOrManyThrough(query, farParent, throughParent, firstKey, secondKey, localKey, secondLocalKey),
	}
	relation.SupportsDefaultModels = concerns.SupportsDefaultModels{
		NewRelatedInstanceFor: relation.newRelatedInstanceFor,
	}
	relation.ComparesRelatedModels = concerns.ComparesRelatedModels{
		CompareRelated:        relation.Related,
		CompareParentKey:      relation.GetParentKey,
		CompareRelatedKeyFrom: relation.getRelatedKeyFrom,
	}

	relation.AddConstraints()
	return relation
}

// GetResults answers HasOneThrough::getResults.
func (r *HasOneThrough) GetResults(ctx context.Context, g auth.Grant) (any, error) {
	if r.GetParentKey() == nil {
		return r.GetDefaultFor(r.farParent), nil
	}

	result, err := r.First(ctx, g)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return r.GetDefaultFor(r.farParent), nil
	}
	return result, nil
}

// InitRelation answers HasOneThrough::initRelation.
func (r *HasOneThrough) InitRelation(models []Model, relation string) []Model {
	for _, model := range models {
		model.SetRelation(relation, r.GetDefaultFor(model))
	}
	return models
}

// Match answers HasOneThrough::match.
func (r *HasOneThrough) Match(models []Model, results []Model, relation string) ([]Model, error) {
	dictionary, err := r.buildDictionary(results)
	if err != nil {
		return nil, err
	}

	for _, model := range models {
		value := model.GetAttribute(r.localKey)
		if value == nil {
			continue
		}
		key, err := concerns.GetDictionaryKey(value)
		if err != nil {
			return nil, err
		}
		if matched, ok := dictionary[key]; ok && len(matched) > 0 {
			model.SetRelation(relation, matched[0])
		}
	}

	return models, nil
}

// newRelatedInstanceFor answers HasOneThrough::newRelatedInstanceFor.
func (r *HasOneThrough) newRelatedInstanceFor(parent Model) Model {
	return r.Related.NewInstance(nil)
}

// getRelatedKeyFrom answers HasOneThrough::getRelatedKeyFrom.
func (r *HasOneThrough) getRelatedKeyFrom(model Model) any {
	return model.GetAttribute(r.GetForeignKeyName())
}
