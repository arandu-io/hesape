package concerns

import (
	"context"
	"sort"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/eloquent/relations"
	"github.com/arandu-io/hesape/str"
)

// The rest of the model surface: the per-instance initializers, the
// observable-event bookkeeping, and the relation helpers that read rather
// than build.

// InitializeGuardsAttributes sets the default guard -- every attribute
// guarded -- the first time it runs.
//
// Go has no constructor hook on an embedded struct, so it is called by
// whoever builds the model -- and it is idempotent, so calling it twice
// costs nothing.
func (g *GuardsAttributes) InitializeGuardsAttributes() {
	if g.guarded == nil {
		g.guarded = []string{"*"}
	}
}

// InitializeHidesAttributes sets the hidden and visible lists to empty the
// first time it runs.
func (h *HidesAttributes) InitializeHidesAttributes() {
	if h.hidden == nil {
		h.hidden = []string{}
	}
	if h.visible == nil {
		h.visible = []string{}
	}
}

// InitializeHasRelationships sets the loaded-relations map to empty the
// first time it runs.
func (h *HasRelationships) InitializeHasRelationships() {
	if h.relations == nil {
		h.relations = map[string]any{}
	}
}

// Declared adds names to the relation names this model defines, which
// IsRelation checks.
func (h *HasRelationships) Declared(names ...string) { h.declared = append(h.declared, names...) }

// IsRelation reports whether key is a relation: one declared through
// Declared, or one registered dynamically through ResolveRelationUsing.
func (h *HasRelationships) IsRelation(key string) bool {
	if contains(h.declared, key) {
		return true
	}
	_, dynamic := h.resolvers[key]
	return dynamic
}

// ResolveRelationUsing registers a relation added from outside the model,
// which a package that extends another package's model needs.
func (h *HasRelationships) ResolveRelationUsing(name string, resolver func(model any) relations.Relation) {
	if h.resolvers == nil {
		h.resolvers = map[string]func(any) relations.Relation{}
	}
	h.resolvers[name] = resolver
}

// RelationResolver returns the resolver registered for name through
// ResolveRelationUsing, or nil.
func (h *HasRelationships) RelationResolver(name string) func(any) relations.Relation {
	return h.resolvers[name]
}

// WithoutRelations clears every loaded relation: what a queued job should
// carry rather than the object graph the request happened to have.
func (h *HasRelationships) WithoutRelations() { h.UnsetRelations() }

// RelationsToArray returns every loaded relation, serialised.
//
// Only what is loaded. A relation that was never loaded is absent rather
// than null, and it is certainly not fetched: serializing a model must not
// be a query.
func (h *HasRelationships) RelationsToArray() map[string]any {
	out := make(map[string]any, len(h.GetRelations()))
	for name, value := range h.GetRelations() {
		out[name] = value
	}
	return out
}

// GetRelationValue returns the loaded relation, or nil.
//
// It does not lazy load: a relation that was not loaded is simply not
// there, and loading it is something a caller does with a Grant.
func (h *HasRelationships) GetRelationValue(key string) any {
	value, _ := h.GetRelation(key)
	return value
}

// JoiningTableSegment returns the model's morph class, snake cased, for
// building a pivot table name.
func (h *HasRelationships) JoiningTableSegment() string {
	return str.Snake(h.MorphClass, "_")
}

// TouchOwners stamps updated_at on the owner of every touched relation
// resolve can resolve.
//
// It takes the Grant like everything else that reaches the database. A
// touch is an UPDATE on another table, and an UPDATE that skipped the
// tenant filter would stamp another customer's row.
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

// GetActualClassNameForMorph returns the model a morph alias resolves to.
func GetActualClassNameForMorph(alias string) (relations.Model, error) {
	return relations.CreateModelByType(alias)
}

// GetQualifiedCreatedAtColumn returns the created-at column qualified with
// table.
func (h *HasTimestamps) GetQualifiedCreatedAtColumn(table string) string {
	return qualify(table, h.GetCreatedAtColumn())
}

// GetQualifiedUpdatedAtColumn returns the updated-at column qualified with
// table.
func (h *HasTimestamps) GetQualifiedUpdatedAtColumn(table string) string {
	return qualify(table, h.GetUpdatedAtColumn())
}

// Touch stamps the timestamps and writes, through save.
//
// save is the model's own, because HasTimestamps, embedded by composition,
// cannot reach the model it is embedded in. attribute optionally names one
// column instead of the pair.
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

// TouchQuietly touches without firing model events.
func (h *HasTimestamps) TouchQuietly(attributes map[string]any, exists bool, save func() error, attribute ...string) error {
	return WithoutEvents(func() error { return h.Touch(attributes, exists, save, attribute...) })
}

// WithoutTimestampsOn suspends timestamps for the callback.
//
// models is accepted but unused: there are no class names to suspend
// timestamps for individually, so this suspends them for the callback as a
// whole -- the same effect for the case it is used in, which is a bulk
// write of one model, and a narrower guarantee stated plainly rather than a
// wider one implied.
func WithoutTimestampsOn(models []string, callback func() error) error {
	return WithoutTimestamps(callback)
}

// GetAllGlobalScopes returns every registered scope.
func (h *HasGlobalScopes) GetAllGlobalScopes() map[string]Scope { return h.GetGlobalScopes() }

// SetAllGlobalScopes replaces every registered scope.
func (h *HasGlobalScopes) SetAllGlobalScopes(scopes map[string]Scope) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	h.scopes = make(map[string]Scope, len(scopes))
	for name, scope := range scopes {
		h.scopes[name] = scope
	}
}

// GetObservableEvents returns the standard events plus whatever this model
// added.
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

// SetObservableEvents replaces the events this model added beyond the
// standard list.
func (h *HasEvents) SetObservableEvents(events []Event) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.observables = events
}

// AddObservableEvents adds events to the ones this model added beyond the
// standard list.
func (h *HasEvents) AddObservableEvents(events ...Event) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.observables = append(h.observables, events...)
}

// RemoveObservableEvents removes events from the ones this model added
// beyond the standard list.
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

// Observe registers a listener for each event in observer.
//
// There is no method lookup by name in Go, so the binding is written out
// as a map instead of discovered from an observer's method names.
func (h *HasEvents) Observe(observer map[Event]Listener) {
	for event, listener := range observer {
		h.Listen(event, listener)
	}
}

// DispatchesEvents returns the custom event a model publishes for each
// model event, by name.
func (h *HasEvents) DispatchesEvents() map[Event]string {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.dispatches
}

// SetDispatchesEvents registers the custom events a model publishes for
// each model event.
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
