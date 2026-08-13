package factories

import (
	"context"
	"fmt"
	"maps"
	"math/rand"
	"slices"
	"sync"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/str"
)

// Model is Illuminate\Database\Eloquent\Model, as a factory uses it.
//
// It is an alias rather than a second declaration: relations already writes out
// the surface a model presents to everything that is not the model itself, and
// a factory touches a subset of it. Aliasing means factories.Model and
// relations.Model are one type, so a relation built by a factory and a relation
// built by hand take the same value.
type Model = relations.Model

// State answers the callable form of Factory::$states.
//
// PHP accepts an array or a callable and wraps the array in a closure; this is
// that closure, with the two arguments the PHP passes it -- the attributes
// resolved so far, and the parent model when the factory is being run for a
// relationship.
type State func(attributes map[string]any, parent Model) map[string]any

// Attribute is the deferred form of a value in a factory definition.
//
// PHP writes `fn () => User::factory()` and calls it with no arguments while
// expanding. This carries a context and an auth.Grant because expanding is not
// always pure: an attribute that resolves to another factory reaches the
// database, and RULE 17 puts the Grant in the signature of everything that
// does. An attribute that only computes ignores both.
type Attribute func(ctx context.Context, g auth.Grant, attributes map[string]any) (any, error)

// ChildRelationship is what Factory::$has holds.
//
// PHP stores two unrelated classes there -- Relationship and
// BelongsToManyRelationship -- and duck-types the two calls it makes on them.
// Go has no duck typing, so the pair of calls is written out as an interface.
// It is the one name in this package that Illuminate does not have, and it
// names a set PHP leaves unnamed rather than renaming anything.
type ChildRelationship interface {
	// CreateFor answers createFor on both classes.
	CreateFor(ctx context.Context, g auth.Grant, parent Model) error

	// Recycle answers recycle on both classes.
	Recycle(recycle map[string][]Model) ChildRelationship
}

// Factory answers the abstract Illuminate\Database\Eloquent\Factories\Factory.
//
// PHP's factory is an abstract class the application subclasses: the subclass
// declares $model and implements definition(), and `new static` reaches both.
// Go has no subclass and no late static binding, so the two declarations are
// constructor arguments and every static method of the PHP that needed `new
// static` is a method here. NewFactory says which changes that costs.
//
// Every method that in PHP returns `static` returns a new *Factory here, as it
// does there: a factory is immutable, and `$factory->count(3)` leaves the
// receiver alone. Sharing one and calling Count on it twice therefore gives two
// factories and not one changed twice, which is the property the whole fluent
// chain rests on.
type Factory struct {
	// model is Factory::$model. PHP holds a class name and instantiates it;
	// there is no class name at run time here, so it holds an instance and
	// NewModel goes through NewInstance, which is the same operation with the
	// prototype supplied rather than named.
	model Model

	// definition is Factory::definition, abstract in PHP.
	definition func() map[string]any

	// count is Factory::$count. Nil is PHP's null: one model, not a collection.
	count *int

	states               []State
	has                  []ChildRelationship
	forParents           []*BelongsToRelationship
	recycle              map[string][]Model
	afterMaking          []func(Model)
	afterCreating        []func(Model, Model)
	expandRelationships  bool
	excludeRelationships []string
	connection           string

	// stateErr holds the refusal of a state whose type PHP would have accepted
	// as a callable and Go cannot. It is carried rather than panicked so that
	// the report comes out of Make and Create, which already return an error.
	stateErr error
}

// The statics of the PHP class, which are process-wide there and process-wide
// here. The mutex is what PHP does not need and Go does: a factory used from
// two goroutines would otherwise race on the namespace.
var (
	globalMutex                  sync.RWMutex
	namespace                    = "Database\\Factories\\"
	modelNameResolver            func(*Factory) string
	factoryNameResolver          func(modelName string) string
	expandRelationshipsByDefault = true
	registry                     = map[string]*Factory{}
)

// NewFactory answers Factory::__construct, and the two declarations a PHP
// subclass makes with it.
//
// model is the prototype the factory builds from -- PHP's protected $model,
// as an instance rather than a class name, because Go cannot make a value from
// a name. definition is Factory::definition, which PHP declares abstract and
// the subclass implements.
//
// The remaining constructor arguments of the PHP -- count, states, has, for,
// afterMaking, afterCreating, connection, recycle, expandRelationships,
// excludeRelationships -- are not here: they exist so newInstance can rebuild a
// factory with one field changed, which in Go is a struct copy, and no caller
// of the PHP constructor passes them.
//
// The factory registers itself under its model's name, which is what
// FactoryForModel later looks up. PHP finds the class by name instead, and
// cannot: a Go package that nothing imports is not in the binary.
func NewFactory(model Model, definition func() map[string]any) *Factory {
	globalMutex.RLock()
	expand := expandRelationshipsByDefault
	globalMutex.RUnlock()

	if definition == nil {
		definition = func() map[string]any { return map[string]any{} }
	}

	f := &Factory{
		model:               model,
		definition:          definition,
		recycle:             map[string][]Model{},
		expandRelationships: expand,
	}

	globalMutex.Lock()
	registry[f.ModelName()] = f
	globalMutex.Unlock()

	return f
}

// newInstance answers Factory::newInstance: the receiver copied, so that every
// fluent method leaves the factory it was called on untouched.
func (f *Factory) newInstance() *Factory {
	copied := *f
	copied.states = slices.Clone(f.states)
	copied.has = slices.Clone(f.has)
	copied.forParents = slices.Clone(f.forParents)
	copied.afterMaking = slices.Clone(f.afterMaking)
	copied.afterCreating = slices.Clone(f.afterCreating)
	copied.excludeRelationships = slices.Clone(f.excludeRelationships)
	copied.recycle = map[string][]Model{}
	for name, models := range f.recycle {
		copied.recycle[name] = slices.Clone(models)
	}
	return &copied
}

// New answers Factory::new.
//
// Static in PHP, where `new static` reaches the subclass. There is no subclass
// here, so it is a method: the receiver carries the declaration the static
// would have reached, and the result is a fresh factory carrying the same one.
func (f *Factory) New(attributes ...map[string]any) *Factory {
	next := f.newInstance()
	next.count = nil
	next.states = nil
	next.has = nil
	next.forParents = nil
	next.afterMaking = nil
	next.afterCreating = nil

	for _, attribute := range attributes {
		next = next.State(attribute)
	}

	return next.Configure()
}

// Times answers Factory::times. A method for the reason New is.
func (f *Factory) Times(count int) *Factory { return f.New().Count(count) }

// Configure answers Factory::configure, the hook a PHP subclass overrides to
// register callbacks on every instance. Go has no override, so it answers the
// receiver and a factory that needs the hook chains AfterMaking itself.
func (f *Factory) Configure() *Factory { return f }

// Definition answers Factory::definition: the model's default state.
//
// Abstract in PHP; here it calls the function NewFactory was given, so that a
// caller reading the factory sees the same name it has there.
func (f *Factory) Definition() map[string]any {
	attributes := f.definition()
	if attributes == nil {
		return map[string]any{}
	}
	return attributes
}

// Raw answers Factory::raw: the attributes the factory would build, expanded,
// with nothing saved.
//
// PHP answers one array when the count is null and a list of them otherwise; Go
// has no union, so it always answers the list and a null count is a list of
// one. The context and the Grant are here because expanding an attribute that
// holds another factory creates that factory's model, which is a write.
func (f *Factory) Raw(ctx context.Context, g auth.Grant, attributes map[string]any, parent Model) ([]map[string]any, error) {
	next := f.State(attributes)

	if next.count == nil {
		expanded, err := next.getExpandedAttributes(ctx, g, parent)
		if err != nil {
			return nil, err
		}
		return []map[string]any{expanded}, nil
	}

	raw := make([]map[string]any, 0, *next.count)
	for i := 0; i < *next.count; i++ {
		expanded, err := next.getExpandedAttributes(ctx, g, parent)
		if err != nil {
			return nil, err
		}
		raw = append(raw, expanded)
	}
	return raw, nil
}

// CreateOne answers Factory::createOne.
func (f *Factory) CreateOne(ctx context.Context, g auth.Grant, attributes map[string]any) (Model, error) {
	created, err := f.Count().Create(ctx, g, attributes, nil)
	if err != nil {
		return nil, err
	}
	return first(created), nil
}

// CreateOneQuietly answers Factory::createOneQuietly.
func (f *Factory) CreateOneQuietly(ctx context.Context, g auth.Grant, attributes map[string]any) (Model, error) {
	created, err := f.Count().CreateQuietly(ctx, g, attributes, nil)
	if err != nil {
		return nil, err
	}
	return first(created), nil
}

// CreateMany answers Factory::createMany.
//
// PHP takes an int or an iterable of attribute arrays; Go takes the list, and
// the int form is Count followed by CreateMany with no records.
func (f *Factory) CreateMany(ctx context.Context, g auth.Grant, records ...map[string]any) ([]Model, error) {
	if len(records) == 0 {
		records = make([]map[string]any, f.recordCount())
	}

	next := f.newInstance()
	next.count = nil

	created := make([]Model, 0, len(records))
	for _, record := range records {
		models, err := next.State(record).Create(ctx, g, nil, nil)
		if err != nil {
			return nil, err
		}
		created = append(created, models...)
	}
	return created, nil
}

// CreateManyQuietly answers Factory::createManyQuietly.
func (f *Factory) CreateManyQuietly(ctx context.Context, g auth.Grant, records ...map[string]any) ([]Model, error) {
	var created []Model
	err := f.model.WithoutEvents(func() error {
		var err error
		created, err = f.CreateMany(ctx, g, records...)
		return err
	})
	return created, err
}

// Create answers Factory::create: the models, made and saved, with their
// children and their after-creating callbacks.
//
// PHP answers a Model when the count is null and a Collection otherwise. Go
// answers the slice in both cases, and CreateOne is the one-model reading.
func (f *Factory) Create(ctx context.Context, g auth.Grant, attributes map[string]any, parent Model) ([]Model, error) {
	if len(attributes) > 0 {
		return f.State(attributes).Create(ctx, g, nil, parent)
	}

	results, err := f.Make(ctx, g, attributes, parent)
	if err != nil {
		return nil, err
	}

	if err := f.store(ctx, g, results); err != nil {
		return nil, err
	}

	f.callAfterCreating(results, parent)

	return results, nil
}

// CreateQuietly answers Factory::createQuietly.
func (f *Factory) CreateQuietly(ctx context.Context, g auth.Grant, attributes map[string]any, parent Model) ([]Model, error) {
	var created []Model
	err := f.model.WithoutEvents(func() error {
		var err error
		created, err = f.Create(ctx, g, attributes, parent)
		return err
	})
	return created, err
}

// Lazy answers Factory::lazy: a callback that persists when it is invoked.
//
// PHP's closure takes nothing, because the connection is global there. This one
// takes the context and the Grant, which are per call and cannot be captured
// at the point the callback is built without outliving the request that owned
// them.
func (f *Factory) Lazy(attributes map[string]any, parent Model) func(context.Context, auth.Grant) ([]Model, error) {
	return func(ctx context.Context, g auth.Grant) ([]Model, error) {
		return f.Create(ctx, g, attributes, parent)
	}
}

// store answers Factory::store: it saves each model and creates its children.
//
// PHP also sets the connection name on every model before saving. The model
// interface a factory holds has no setter for it -- the connection belongs to
// the model that was handed in -- so Connection records the name and this does
// not apply it.
func (f *Factory) store(ctx context.Context, g auth.Grant, results []Model) error {
	for _, model := range results {
		if err := model.Save(ctx, g); err != nil {
			return err
		}
		if err := f.createChildren(ctx, g, model); err != nil {
			return err
		}
	}
	return nil
}

// createChildren answers Factory::createChildren.
//
// PHP wraps the loop in Model::unguarded, because a child's foreign key is
// rarely in $fillable. There is no mass assignment guard to lift here: the
// foreign key is set on the state, and the state goes in through ForceFill.
func (f *Factory) createChildren(ctx context.Context, g auth.Grant, model Model) error {
	for _, has := range f.has {
		if err := has.Recycle(f.recycle).CreateFor(ctx, g, model); err != nil {
			return err
		}
	}
	return nil
}

// MakeOne answers Factory::makeOne.
func (f *Factory) MakeOne(ctx context.Context, g auth.Grant, attributes map[string]any) (Model, error) {
	made, err := f.Count().Make(ctx, g, attributes, nil)
	if err != nil {
		return nil, err
	}
	return first(made), nil
}

// Make answers Factory::make: the models, built and not saved.
//
// PHP answers a Model when the count is null and a Collection otherwise; this
// answers the slice in both cases, as Create does.
func (f *Factory) Make(ctx context.Context, g auth.Grant, attributes map[string]any, parent Model) ([]Model, error) {
	if len(attributes) > 0 {
		return f.State(attributes).Make(ctx, g, nil, parent)
	}

	if f.count == nil {
		instance, err := f.makeInstance(ctx, g, parent)
		if err != nil {
			return nil, err
		}
		instances := []Model{instance}
		f.callAfterMaking(instances)
		return instances, nil
	}

	if *f.count < 1 {
		return []Model{}, nil
	}

	instances := make([]Model, 0, *f.count)
	for i := 0; i < *f.count; i++ {
		instance, err := f.makeInstance(ctx, g, parent)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	f.callAfterMaking(instances)

	return instances, nil
}

// MakeMany answers Factory::makeMany.
func (f *Factory) MakeMany(ctx context.Context, g auth.Grant, records ...map[string]any) ([]Model, error) {
	if len(records) == 0 {
		records = make([]map[string]any, f.recordCount())
	}

	next := f.newInstance()
	next.count = nil

	made := make([]Model, 0, len(records))
	for _, record := range records {
		models, err := next.State(record).Make(ctx, g, nil, nil)
		if err != nil {
			return nil, err
		}
		made = append(made, models...)
	}
	return made, nil
}

// Insert answers Factory::insert: the rows written in one statement, with no
// model events and no children.
func (f *Factory) Insert(ctx context.Context, g auth.Grant, attributes map[string]any, parent Model) error {
	made, err := f.Make(ctx, g, attributes, parent)
	if err != nil {
		return err
	}
	if len(made) == 0 {
		return nil
	}

	rows := make([]map[string]any, 0, len(made))
	for _, model := range made {
		rows = append(rows, maps.Clone(model.GetAttributes()))
	}

	return made[0].NewQuery().Insert(ctx, g, rows)
}

// makeInstance answers Factory::makeInstance.
func (f *Factory) makeInstance(ctx context.Context, g auth.Grant, parent Model) (Model, error) {
	attributes, err := f.getExpandedAttributes(ctx, g, parent)
	if err != nil {
		return nil, err
	}
	return f.NewModel(attributes), nil
}

// getExpandedAttributes answers Factory::getExpandedAttributes.
func (f *Factory) getExpandedAttributes(ctx context.Context, g auth.Grant, parent Model) (map[string]any, error) {
	raw, err := f.getRawAttributes(ctx, g, parent)
	if err != nil {
		return nil, err
	}
	return f.expandAttributes(ctx, g, raw)
}

// getRawAttributes answers Factory::getRawAttributes: the definition with every
// state folded over it, in the order the states were added.
func (f *Factory) getRawAttributes(ctx context.Context, g auth.Grant, parent Model) (map[string]any, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}

	states := f.states
	if len(f.forParents) > 0 {
		states = append([]State{func(attributes map[string]any, _ Model) map[string]any {
			return f.parentResolvers()
		}}, states...)
	}

	carry := maps.Clone(f.Definition())
	if carry == nil {
		carry = map[string]any{}
	}

	for _, state := range states {
		produced := state(carry, parent)
		for key, value := range produced {
			carry[key] = value
		}
	}

	return carry, nil
}

// parentResolvers answers Factory::parentResolvers: one deferred attribute per
// "for" relationship, holding the parent's key.
func (f *Factory) parentResolvers() map[string]any {
	resolvers := map[string]any{}
	for _, parent := range f.forParents {
		for key, value := range parent.Recycle(f.recycle).AttributesFor(f.NewModel(nil)) {
			resolvers[key] = value
		}
	}
	return resolvers
}

// expandAttributes answers Factory::expandAttributes: every attribute reduced
// to the value that goes in the column.
//
// A factory becomes the key of a model it creates, a model becomes its own key,
// and an Attribute is called. PHP walks the definition in insertion order,
// which a Go map does not have, so the walk is over the sorted keys: an
// attribute that reads one computed before it therefore sees a deterministic
// order, and the same one on every machine.
func (f *Factory) expandAttributes(ctx context.Context, g auth.Grant, definition map[string]any) (map[string]any, error) {
	expanded := maps.Clone(definition)

	for _, key := range slices.Sorted(maps.Keys(expanded)) {
		value := expanded[key]

		resolved, err := f.evaluateRelations(ctx, g, key, value)
		if err != nil {
			return nil, err
		}

		if attribute, ok := resolved.(Attribute); ok {
			produced, err := attribute(ctx, g, expanded)
			if err != nil {
				return nil, err
			}
			resolved, err = f.evaluateRelations(ctx, g, key, produced)
			if err != nil {
				return nil, err
			}
		}

		expanded[key] = resolved
	}

	return expanded, nil
}

// evaluateRelations answers the inner closure of Factory::expandAttributes.
func (f *Factory) evaluateRelations(ctx context.Context, g auth.Grant, key string, value any) (any, error) {
	switch typed := value.(type) {
	case *Factory:
		if !f.expandRelationships {
			return nil, nil
		}
		if slices.Contains(f.excludeRelationships, typed.ModelName()) ||
			slices.Contains(f.excludeRelationships, key) {
			return nil, nil
		}
		if recycled := f.GetRandomRecycledModel(typed.ModelName()); recycled != nil {
			return recycled.GetKey(), nil
		}
		created, err := typed.Recycle(flatten(f.recycle)...).Create(ctx, g, nil, nil)
		if err != nil {
			return nil, err
		}
		if model := first(created); model != nil {
			return model.GetKey(), nil
		}
		return nil, nil
	case Model:
		return typed.GetKey(), nil
	default:
		return value, nil
	}
}

// State answers Factory::state: one more transformation over the definition.
//
// PHP takes an array or a callable. Go has no union, so the argument is any and
// the accepted dynamic types are a map[string]any, a State, and a Sequence.
// Anything else is recorded as a state that fails when the factory runs, which
// is louder than silently producing a model with a column missing.
func (f *Factory) State(state any) *Factory {
	if state == nil {
		return f
	}
	next := f.newInstance()
	converted, err := stateFrom(state)
	if err != nil {
		next.stateErr = err
		return next
	}
	next.states = append(next.states, converted)
	return next
}

// PrependState answers Factory::prependState: a transformation that runs before
// every state already added, so that a caller's own state still wins.
func (f *Factory) PrependState(state any) *Factory {
	if state == nil {
		return f
	}
	next := f.newInstance()
	converted, err := stateFrom(state)
	if err != nil {
		next.stateErr = err
		return next
	}
	next.states = append([]State{converted}, next.states...)
	return next
}

// stateFrom reads PHP's array-or-callable union as a State.
func stateFrom(state any) (State, error) {
	switch typed := state.(type) {
	case State:
		return typed, nil
	case func(map[string]any, Model) map[string]any:
		return typed, nil
	case map[string]any:
		attributes := maps.Clone(typed)
		return func(map[string]any, Model) map[string]any { return attributes }, nil
	case interface {
		Invoke(map[string]any, Model) any
	}:
		return func(attributes map[string]any, parent Model) map[string]any {
			return toAttributes(typed.Invoke(attributes, parent))
		}, nil
	default:
		return nil, fmt.Errorf("factories: a state is a map[string]any, a State or a Sequence, and %T is none of them", state)
	}
}

// Set answers Factory::set: one attribute, as a state.
func (f *Factory) Set(key string, value any) *Factory {
	return f.State(map[string]any{key: value})
}

// Sequence answers Factory::sequence.
func (f *Factory) Sequence(sequence ...any) *Factory {
	return f.State(NewSequence(sequence...))
}

// ForEachSequence answers Factory::forEachSequence: the sequence, with the
// count set to its length, so that every element is used exactly once.
func (f *Factory) ForEachSequence(sequence ...any) *Factory {
	return f.State(NewSequence(sequence...)).Count(len(sequence))
}

// CrossJoinSequence answers Factory::crossJoinSequence.
func (f *Factory) CrossJoinSequence(sequences ...[]any) *Factory {
	return f.State(NewCrossJoinSequence(sequences...))
}

// Has answers Factory::has: a child relationship the factory creates after the
// parent is saved.
//
// PHP finds the relation by calling $parent->{$relationship}(). Go cannot look
// up a method by name and stay type safe -- the same wall eloquent's
// RelationResolvers hits -- so the relation is a function the caller supplies.
// An empty relationship name is guessed from the related model, as it is there.
func (f *Factory) Has(factory *Factory, relationship string, relation func(parent Model) any) *Factory {
	if relationship == "" {
		relationship = f.guessRelationship(factory.ModelName())
	}
	next := f.newInstance()
	next.has = append(next.has, NewRelationship(factory, relationship, relation))
	return next
}

// guessRelationship answers Factory::guessRelationship.
//
// PHP asks method_exists whether the plural name is a method and falls back to
// the singular. There is no method to ask about here, so it is the plural, and
// a caller that needs the singular passes the name.
func (f *Factory) guessRelationship(related string) string {
	return str.Camel(str.Plural(related))
}

// HasAttached answers Factory::hasAttached: a many-to-many relationship the
// factory attaches after the parent is saved, with the pivot attributes.
//
// related is PHP's Factory|Collection|Model|array union and pivot is its
// callable|array one; NewBelongsToManyRelationship says which dynamic types
// each reads.
func (f *Factory) HasAttached(related any, pivot any, relationship string, relation func(parent Model) any) *Factory {
	if relationship == "" {
		relationship = str.Camel(str.Plural(nameOf(related)))
	}
	next := f.newInstance()
	next.has = append(next.has, NewBelongsToManyRelationship(related, pivot, relationship, relation))
	return next
}

// For answers Factory::for: a parent relationship, whose key becomes a column
// on the model this factory makes.
//
// parent is PHP's Factory|Model union: the owner's factory, or an owner that
// exists already.
func (f *Factory) For(parent any, relationship string, relation func(child Model) any) *Factory {
	if relationship == "" {
		relationship = str.Camel(nameOf(parent))
	}
	next := f.newInstance()
	next.forParents = append(next.forParents, NewBelongsToRelationship(parent, relationship, relation))
	return next
}

// nameOf answers PHP's class_basename over the Factory|Model union the
// relationship methods take: the name the related model is known by.
func nameOf(related any) string {
	switch typed := related.(type) {
	case *Factory:
		return typed.ModelName()
	case []Model:
		if model := first(typed); model != nil {
			return model.GetMorphClass()
		}
		return ""
	case Model:
		return typed.GetMorphClass()
	default:
		return ""
	}
}

// Recycle answers Factory::recycle: models to reuse instead of creating new
// ones for a relationship.
//
// PHP groups them by class; they are grouped by GetMorphClass here, which is
// the name a model answers to in this collection.
func (f *Factory) Recycle(models ...Model) *Factory {
	next := f.newInstance()
	for _, model := range models {
		if model == nil {
			continue
		}
		name := model.GetMorphClass()
		next.recycle[name] = append(next.recycle[name], model)
	}
	return next
}

// GetRandomRecycledModel answers Factory::getRandomRecycledModel: one of the
// recycled models of the given name, or nil when none was provided.
func (f *Factory) GetRandomRecycledModel(modelName string) Model {
	models := f.recycle[modelName]
	if len(models) == 0 {
		return nil
	}
	return models[rand.Intn(len(models))]
}

// AfterMaking answers Factory::afterMaking.
func (f *Factory) AfterMaking(callback func(Model)) *Factory {
	next := f.newInstance()
	next.afterMaking = append(next.afterMaking, callback)
	return next
}

// AfterCreating answers Factory::afterCreating. The second argument is the
// parent the factory was run for, which is nil outside a relationship.
func (f *Factory) AfterCreating(callback func(Model, Model)) *Factory {
	next := f.newInstance()
	next.afterCreating = append(next.afterCreating, callback)
	return next
}

// WithoutAfterMaking answers Factory::withoutAfterMaking.
func (f *Factory) WithoutAfterMaking() *Factory {
	next := f.newInstance()
	next.afterMaking = nil
	return next
}

// WithoutAfterCreating answers Factory::withoutAfterCreating.
func (f *Factory) WithoutAfterCreating() *Factory {
	next := f.newInstance()
	next.afterCreating = nil
	return next
}

// callAfterMaking answers Factory::callAfterMaking.
func (f *Factory) callAfterMaking(instances []Model) {
	for _, model := range instances {
		for _, callback := range f.afterMaking {
			callback(model)
		}
	}
}

// callAfterCreating answers Factory::callAfterCreating.
func (f *Factory) callAfterCreating(instances []Model, parent Model) {
	for _, model := range instances {
		for _, callback := range f.afterCreating {
			callback(model, parent)
		}
	}
}

// Count answers Factory::count: how many models the factory makes.
//
// PHP's argument is nullable, and null means one model rather than a collection
// of one. Go spells the null as no argument: Count() is count(null) and
// Count(3) is count(3).
func (f *Factory) Count(count ...int) *Factory {
	next := f.newInstance()
	if len(count) == 0 {
		next.count = nil
		return next
	}
	value := count[len(count)-1]
	next.count = &value
	return next
}

// recordCount answers the `$records ??= ($this->count ?? 1)` of createMany and
// makeMany.
func (f *Factory) recordCount() int {
	if f.count == nil {
		return 1
	}
	if *f.count < 0 {
		return 0
	}
	return *f.count
}

// WithoutParents answers Factory::withoutParents: no parent model is created
// for a factory-valued attribute.
//
// With no argument every parent is skipped; with names, only those, matched
// against the attribute's key or the related model's name.
func (f *Factory) WithoutParents(parents ...string) *Factory {
	next := f.newInstance()
	if len(parents) == 0 {
		next.expandRelationships = false
		return next
	}
	next.excludeRelationships = slices.Clone(parents)
	return next
}

// GetConnectionName answers Factory::getConnectionName.
func (f *Factory) GetConnectionName() string { return f.connection }

// Connection answers Factory::connection.
//
// It records the name and nothing more. PHP calls setConnection on every model
// it stores; the model surface a factory holds has no such setter, because a
// model in this collection is constructed with its connection rather than
// re-pointed at another one after the fact.
func (f *Factory) Connection(connection string) *Factory {
	next := f.newInstance()
	next.connection = connection
	return next
}

// NewModel answers Factory::newModel.
//
// PHP writes `new $model($attributes)`; here the prototype makes the instance,
// and the attributes go in past the mass assignment guard, which is what
// Model::unguarded does around makeInstance there.
func (f *Factory) NewModel(attributes map[string]any) Model {
	instance := f.model.NewInstance(nil)
	if len(attributes) > 0 {
		instance.ForceFill(attributes)
	}
	return instance
}

// ModelName answers Factory::modelName.
//
// PHP resolves a class name from the factory's own class name, or from the
// UseModel attribute, or from a resolver. There are no class names here, so it
// is the prototype's morph class -- the name this collection already uses
// wherever PHP would use a class name -- unless GuessModelNamesUsing installed
// a resolver.
func (f *Factory) ModelName() string {
	globalMutex.RLock()
	resolver := modelNameResolver
	globalMutex.RUnlock()

	if resolver != nil {
		return resolver(f)
	}
	if f.model == nil {
		return ""
	}
	return f.model.GetMorphClass()
}

// GuessModelNamesUsing answers Factory::guessModelNamesUsing.
//
// Per subclass in PHP, process-wide here: the key there is the factory's class,
// and a Go factory is a value with no class to key on.
func GuessModelNamesUsing(callback func(*Factory) string) {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	modelNameResolver = callback
}

// UseNamespace answers Factory::useNamespace: the prefix ResolveFactoryName
// puts in front of a factory's name.
func UseNamespace(value string) {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	namespace = value
}

// FactoryForModel answers Factory::factoryForModel.
//
// PHP resolves a class name and calls its static new(). There is no class to
// resolve here: a factory registers itself under its model's name when it is
// constructed, and this reads that registry. A model whose factory's package
// was never imported is not in it, and the error says so with the name
// ResolveFactoryName would have produced, which is what a reader will grep for.
func FactoryForModel(modelName string) (*Factory, error) {
	globalMutex.RLock()
	factory, ok := registry[modelName]
	globalMutex.RUnlock()

	if !ok {
		return nil, fmt.Errorf("factories: no factory is registered for model %q; %s is not in the binary, or its package is not imported", modelName, ResolveFactoryName(modelName))
	}

	return factory.New(), nil
}

// GuessFactoryNamesUsing answers Factory::guessFactoryNamesUsing.
func GuessFactoryNamesUsing(callback func(modelName string) string) {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	factoryNameResolver = callback
}

// ResolveFactoryName answers Factory::resolveFactoryName: the name a model's
// factory is expected to carry.
//
// It names nothing the runtime looks up -- FactoryForModel reads a registry --
// but `aru make:factory` writes the file this names, and an error that has to
// tell a reader what is missing has to call it something.
func ResolveFactoryName(modelName string) string {
	globalMutex.RLock()
	resolver := factoryNameResolver
	prefix := namespace
	globalMutex.RUnlock()

	if resolver != nil {
		return resolver(modelName)
	}

	return prefix + str.Studly(modelName) + "Factory"
}

// ExpandRelationshipsByDefault answers Factory::expandRelationshipsByDefault.
func ExpandRelationshipsByDefault() {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	expandRelationshipsByDefault = true
}

// DontExpandRelationshipsByDefault answers
// Factory::dontExpandRelationshipsByDefault.
func DontExpandRelationshipsByDefault() {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	expandRelationshipsByDefault = false
}

// FlushState answers Factory::flushState.
//
// It clears the resolvers and the namespace, as the PHP does. The registry of
// constructed factories is not part of it: there it is the set of classes that
// exist, which no method could flush.
func FlushState() {
	globalMutex.Lock()
	defer globalMutex.Unlock()
	modelNameResolver = nil
	factoryNameResolver = nil
	namespace = "Database\\Factories\\"
	expandRelationshipsByDefault = true
}

// first answers Collection::first over a slice of models.
func first(models []Model) Model {
	if len(models) == 0 {
		return nil
	}
	return models[0]
}

// flatten answers Collection::flatten over the recycle map.
func flatten(recycle map[string][]Model) []Model {
	var models []Model
	for _, name := range slices.Sorted(maps.Keys(recycle)) {
		models = append(models, recycle[name]...)
	}
	return models
}
