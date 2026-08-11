package concerns

import (
	"context"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/str"
)

// The rest of the model surface: the initializers PHP calls through
// bootIfNotBooted, the observable-event bookkeeping, and the relation helpers
// that read rather than build.

// InitializeGuardsAttributes answers
// GuardsAttributes::initializeGuardsAttributes.
//
// PHP runs it once per instance through the trait initializer. Go has no
// constructor hook on an embedded struct, so it is called by whoever builds the
// model -- and it is idempotent, so calling it twice costs nothing.
func (g *GuardsAttributes) InitializeGuardsAttributes() {
	if g.guarded == nil {
		g.guarded = []string{"*"}
	}
}

// InitializeHidesAttributes answers
// HidesAttributes::initializeHidesAttributes.
func (h *HidesAttributes) InitializeHidesAttributes() {
	if h.hidden == nil {
		h.hidden = []string{}
	}
	if h.visible == nil {
		h.visible = []string{}
	}
}

// InitializeHasRelationships answers
// HasRelationships::initializeHasRelationships.
func (h *HasRelationships) InitializeHasRelationships() {
	if h.relations == nil {
		h.relations = map[string]any{}
	}
}

// Declared answers the relation names this model defines, which IsRelation
// checks. It is a field's worth of state that PHP reads with method_exists.
func (h *HasRelationships) Declared(names ...string) { h.declared = append(h.declared, names...) }

// IsRelation answers HasAttributes::isRelation.
//
// PHP asks method_exists, plus the dynamic relations resolveRelationUsing
// registered. There is no method_exists here, so a model declares its relation
// names -- which is the same list, written down instead of discovered.
func (h *HasRelationships) IsRelation(key string) bool {
	if contains(h.declared, key) {
		return true
	}
	_, dynamic := h.resolvers[key]
	return dynamic
}

// ResolveRelationUsing answers HasRelationships::resolveRelationUsing: a
// relation added from outside the model, which a package that extends another
// package's model needs.
func (h *HasRelationships) ResolveRelationUsing(name string, resolver func(model any) relations.Relation) {
	if h.resolvers == nil {
		h.resolvers = map[string]func(any) relations.Relation{}
	}
	h.resolvers[name] = resolver
}

// RelationResolver answers HasRelationships::relationResolver.
func (h *HasRelationships) RelationResolver(name string) func(any) relations.Relation {
	return h.resolvers[name]
}

// WithoutRelations answers HasRelationships::withoutRelations: the same model
// with nothing loaded, which is what a queued job should carry rather than the
// object graph the request happened to have.
func (h *HasRelationships) WithoutRelations() { h.UnsetRelations() }

// RelationsToArray answers HasAttributes::relationsToArray.
//
// Only what is loaded. A relation that was never loaded is absent rather than
// null, and it is certainly not fetched: serializing a model must not be a
// query, which is the rule that lazy loading breaks quietly.
func (h *HasRelationships) RelationsToArray() map[string]any {
	out := make(map[string]any, len(h.GetRelations()))
	for name, value := range h.GetRelations() {
		out[name] = value
	}
	return out
}

// GetRelationValue answers HasAttributes::getRelationValue: the loaded
// relation, or nil.
//
// It does not lazy load. The PHP's version calls
// getRelationshipFromMethod when the relation is missing, which is the line
// that turns a template into N queries; here a relation that was not loaded is
// not there, and loading it is something a caller does with a Grant.
func (h *HasRelationships) GetRelationValue(key string) any {
	value, _ := h.GetRelation(key)
	return value
}

// JoiningTableSegment answers HasRelationships::joiningTableSegment.
func (h *HasRelationships) JoiningTableSegment() string {
	return str.Snake(h.MorphClass, "_")
}

// TouchOwners answers HasRelationships::touchOwners: the relations listed in
// touches get their updated_at stamped.
//
// It takes the Grant like everything else that reaches the database. A touch is
// an UPDATE on another table, and an UPDATE that skipped the tenant filter
// would stamp another customer's row.
func (h *HasRelationships) TouchOwners(ctx context.Context, g auth.Grant, resolve func(string) relations.Relation) error {
	for _, name := range h.GetTouchedRelations() {
		relation := resolve(name)
		if relation == nil {
			continue
		}
		if toucher, ok := relation.(interface {
			Touch(context.Context, auth.Grant) error
		}); ok {
			if err := toucher.Touch(ctx, g); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetActualClassNameForMorph answers
// HasRelationships::getActualClassNameForMorph.
func GetActualClassNameForMorph(alias string) (relations.Model, error) {
	return relations.CreateModelByType(alias)
}

// GetQualifiedCreatedAtColumn answers
// HasTimestamps::getQualifiedCreatedAtColumn.
func (h *HasTimestamps) GetQualifiedCreatedAtColumn(table string) string {
	return qualify(table, h.GetCreatedAtColumn())
}

// GetQualifiedUpdatedAtColumn answers
// HasTimestamps::getQualifiedUpdatedAtColumn.
func (h *HasTimestamps) GetQualifiedUpdatedAtColumn(table string) string {
	return qualify(table, h.GetUpdatedAtColumn())
}

// Touch answers HasTimestamps::touch: stamp the timestamps and write.
//
// save is the model's own, because a trait cannot reach the model it was
// embedded in. attribute names one column instead of the pair, as the PHP's
// argument does.
func (h *HasTimestamps) Touch(attributes map[string]any, exists bool, save func() error, attribute ...string) error {
	if len(attribute) > 0 && attribute[0] != "" {
		attributes[attribute[0]] = h.FreshTimestamp()
		return save()
	}

	if !h.UsesTimestamps() {
		return nil
	}

	h.UpdateTimestamps(attributes, exists)
	return save()
}

// TouchQuietly answers HasTimestamps::touchQuietly.
func (h *HasTimestamps) TouchQuietly(attributes map[string]any, exists bool, save func() error, attribute ...string) error {
	return WithoutEvents(func() error { return h.Touch(attributes, exists, save, attribute...) })
}

// WithoutTimestampsOn answers HasTimestamps::withoutTimestampsOn.
//
// The PHP takes a list of model classes and suspends timestamps for those
// alone. There are no class names here, so it suspends them for the callback --
// the same effect for the case it is used in, which is a bulk write of one
// model, and a narrower guarantee stated plainly rather than a wider one
// implied.
func WithoutTimestampsOn(models []string, callback func() error) error {
	return WithoutTimestamps(callback)
}

// GetAllGlobalScopes answers HasGlobalScopes::getAllGlobalScopes.
func (h *HasGlobalScopes) GetAllGlobalScopes() map[string]Scope { return h.GetGlobalScopes() }

// SetAllGlobalScopes answers HasGlobalScopes::setAllGlobalScopes.
func (h *HasGlobalScopes) SetAllGlobalScopes(scopes map[string]Scope) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.scopes = make(map[string]Scope, len(scopes))
	for name, scope := range scopes {
		h.scopes[name] = scope
	}
}

// GetObservableEvents answers HasEvents::getObservableEvents for one model: the
// standard list plus whatever it added.
func (h *HasEvents) GetObservableEvents() []Event {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	events := append([]Event(nil), GetObservableEvents()...)
	for _, extra := range h.observables {
		if !containsEvent(events, extra) {
			events = append(events, extra)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i] < events[j] })
	return events
}

// SetObservableEvents answers HasEvents::setObservableEvents.
func (h *HasEvents) SetObservableEvents(events []Event) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.observables = events
}

// AddObservableEvents answers HasEvents::addObservableEvents.
func (h *HasEvents) AddObservableEvents(events ...Event) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.observables = append(h.observables, events...)
}

// RemoveObservableEvents answers HasEvents::removeObservableEvents.
func (h *HasEvents) RemoveObservableEvents(events ...Event) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	kept := make([]Event, 0, len(h.observables))
	for _, have := range h.observables {
		if !containsEvent(events, have) {
			kept = append(kept, have)
		}
	}
	h.observables = kept
}

// Observe answers HasEvents::observe.
//
// The PHP takes an observer class and binds each of its methods by name. There
// is no method lookup by name here, so it takes the map -- which is the same
// binding, written out.
func (h *HasEvents) Observe(observer map[Event]Listener) {
	for event, listener := range observer {
		h.Listen(event, listener)
	}
}

// DispatchesEvents answers HasEvents::dispatchesEvents: the custom event a
// model publishes for each model event, by name.
func (h *HasEvents) DispatchesEvents() map[Event]string {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.dispatches
}

// SetDispatchesEvents registers the custom events, which the PHP declares as a
// $dispatchesEvents property.
func (h *HasEvents) SetDispatchesEvents(dispatches map[Event]string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.dispatches = dispatches
}

func containsEvent(events []Event, needle Event) bool {
	for _, event := range events {
		if event == needle {
			return true
		}
	}
	return false
}

func qualify(table, column string) string {
	if table == "" || strings.Contains(column, ".") {
		return column
	}
	return table + "." + column
}
