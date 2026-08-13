package eloquent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
)

// GetAttributes answers Model::getAttributes: every column of the row, as the
// database sees it.
//
// PHP returns the $attributes array itself. Here the row lives in the entity
// struct, so the map is built from it -- plus the raw attributes a column with
// no field behind it left behind (a withCount alias, a column a migration added
// and the struct has not caught up with).
func (m *Model[T]) GetAttributes() map[string]any {
	entity := reflect.ValueOf(m.Entity)
	out := make(map[string]any, len(fieldsOf(entity.Type()))+len(m.attributes))
	for _, f := range fieldsOf(entity.Type()) {
		out[f.column] = valueAt(entity, f)
	}
	for key, value := range m.attributes {
		out[key] = value
	}
	return out
}

// SetRawAttributes answers Model::setRawAttributes: it replaces the row without
// checking anything, and optionally syncs the original.
//
// It is what Hydrate uses, and it is the only path that puts a value the caller
// did not declare on the model: a key with no field behind it is kept as a raw
// attribute rather than dropped, because a select the caller wrote has a reason
// for every column in it.
func (m *Model[T]) SetRawAttributes(attributes map[string]any, sync bool) error {
	m.Entity = *new(T)
	m.attributes = nil
	if err := m.setAttributes(attributes, true); err != nil {
		return err
	}
	if sync {
		m.SyncOriginal()
	}
	return nil
}

// setAttributes writes a map onto the entity. keepUnknown decides what happens
// to a key with no field behind it: Fill drops it, ForceFill and
// SetRawAttributes keep it.
func (m *Model[T]) setAttributes(attributes map[string]any, keepUnknown bool) error {
	var discarded []string
	for _, key := range sortedKeys(attributes) {
		value := attributes[key]
		known, err := m.setAttribute(key, value)
		if err != nil {
			return err
		}
		if known {
			continue
		}
		if keepUnknown {
			if m.attributes == nil {
				m.attributes = map[string]any{}
			}
			m.attributes[key] = value
			continue
		}
		discarded = append(discarded, key)
	}
	return m.handleDiscardedAttributeViolation(discarded)
}

// SetAttribute answers Model::setAttribute.
//
// The PHP runs mutators and casts on the way in; here the field's type is the
// cast, so the value is converted to it or refused. A column the entity does not
// declare is kept as a raw attribute, which is what assigning to an undeclared
// $attributes key does there.
func (m *Model[T]) SetAttribute(key string, value any) error {
	known, err := m.setAttribute(key, value)
	if err != nil {
		return err
	}
	if !known {
		if m.attributes == nil {
			m.attributes = map[string]any{}
		}
		m.attributes[key] = value
	}
	return nil
}

func (m *Model[T]) setAttribute(key string, value any) (bool, error) {
	entity := reflect.ValueOf(&m.Entity).Elem()
	f, ok := fieldByColumn(entity.Type(), key)
	if !ok {
		return false, nil
	}
	dst, settable := settableAt(entity, f)
	if !settable {
		return false, nil
	}
	if err := assign(dst, value); err != nil {
		return true, fmt.Errorf("eloquent: %s.%s: %w", m.GetTable(), key, err)
	}
	return true, nil
}

// GetAttribute answers Model::getAttribute.
//
// A key that is neither a column nor a raw attribute reads as nil, which is what
// PHP answers for a missing key. PreventAccessingMissingAttributes turns that
// read into a reported violation, as it does there -- and it catches much less
// here, because a row is a struct and found.Entity.Naem does not build.
func (m *Model[T]) GetAttribute(key string) any {
	entity := reflect.ValueOf(m.Entity)
	if f, ok := fieldByColumn(entity.Type(), key); ok {
		return valueAt(entity, f)
	}
	if value, ok := m.attributes[key]; ok {
		return value
	}
	if related, ok := m.relations[key]; ok {
		return related
	}
	m.handleMissingAttributeViolation(key)
	return nil
}

// AttributesToArray answers Model::attributesToArray: the row as it is
// serialised, with the hidden columns removed and the appended ones added.
func (m *Model[T]) AttributesToArray() map[string]any {
	attributes := m.GetAttributes()
	out := make(map[string]any, len(attributes))
	for key, value := range attributes {
		if !m.isVisible(key) {
			continue
		}
		out[key] = value
	}
	for _, key := range m.appends {
		if !m.isVisible(key) {
			continue
		}
		out[key] = m.GetAttribute(key)
	}
	return out
}

// isVisible answers getArrayableItems: $visible wins when it is set, $hidden
// removes otherwise.
func (m *Model[T]) isVisible(key string) bool {
	if len(m.visible) > 0 {
		return slices.Contains(m.visible, key)
	}
	return !slices.Contains(m.hidden, key)
}

// ToArray answers Model::toArray: the serialised row together with the loaded
// relations, which is what relationsToArray adds there.
func (m *Model[T]) ToArray() map[string]any {
	out := m.AttributesToArray()
	for name, related := range m.relations {
		if !m.isVisible(name) {
			continue
		}
		out[name] = related
	}
	return out
}

// ToJSON answers Model::toJson. The PHP spells it toJson and returns a string;
// Go initialisms are upper case and encoding/json returns bytes.
func (m *Model[T]) ToJSON() ([]byte, error) {
	out, err := json.Marshal(m.ToArray())
	if err != nil {
		return nil, fmt.Errorf("eloquent: encoding %s: %w", m.GetTable(), err)
	}
	return out, nil
}

// ToPrettyJSON answers Model::toPrettyJson. The PHP spells it toPrettyJson.
func (m *Model[T]) ToPrettyJSON() ([]byte, error) {
	out, err := json.MarshalIndent(m.ToArray(), "", "    ")
	if err != nil {
		return nil, fmt.Errorf("eloquent: encoding %s: %w", m.GetTable(), err)
	}
	return out, nil
}

// GetOriginal answers Model::getOriginal with no argument: the row as it was
// when it was last synced.
func (m *Model[T]) GetOriginal() map[string]any {
	return copyMap(m.original)
}

// GetRawOriginal answers Model::getRawOriginal for one key.
//
// The PHP distinguishes getOriginal from getRawOriginal by whether casts are
// applied on the way out. Here they are the same value, because the cast is the
// field's type and the original was cast when it was read; the two methods are
// kept apart anyway, because a caller that asks for the raw one is saying
// something about intent.
func (m *Model[T]) GetRawOriginal(key string) any { return m.original[key] }

// SyncOriginal answers Model::syncOriginal.
func (m *Model[T]) SyncOriginal() *Model[T] {
	m.original = m.GetAttributes()
	return m
}

// SyncOriginalAttribute answers Model::syncOriginalAttribute.
func (m *Model[T]) SyncOriginalAttribute(attribute string) *Model[T] {
	return m.SyncOriginalAttributes(attribute)
}

// SyncOriginalAttributes answers Model::syncOriginalAttributes.
func (m *Model[T]) SyncOriginalAttributes(attributes ...string) *Model[T] {
	current := m.GetAttributes()
	if m.original == nil {
		m.original = map[string]any{}
	}
	for _, key := range attributes {
		m.original[key] = current[key]
	}
	return m
}

// SyncChanges answers Model::syncChanges.
func (m *Model[T]) SyncChanges() *Model[T] {
	m.changes = m.GetDirty()
	m.previous = map[string]any{}
	for key := range m.changes {
		if original, ok := m.original[key]; ok {
			m.previous[key] = original
		}
	}
	return m
}

// GetDirty answers Model::getDirty: the columns that differ from the original.
func (m *Model[T]) GetDirty() map[string]any {
	dirty := map[string]any{}
	for key, value := range m.GetAttributes() {
		if !m.OriginalIsEquivalent(key) {
			dirty[key] = value
		}
	}
	return dirty
}

// GetChanges answers Model::getChanges: what changed on the last save.
func (m *Model[T]) GetChanges() map[string]any { return copyMap(m.changes) }

// GetPrevious answers Model::getPrevious: what those columns held before it.
func (m *Model[T]) GetPrevious() map[string]any { return copyMap(m.previous) }

// IsDirty answers Model::isDirty. With no argument it asks about the whole row.
func (m *Model[T]) IsDirty(attributes ...string) bool {
	return hasChanges(m.GetDirty(), attributes)
}

// IsClean answers Model::isClean.
func (m *Model[T]) IsClean(attributes ...string) bool { return !m.IsDirty(attributes...) }

// WasChanged answers Model::wasChanged: whether the last save touched these
// columns.
func (m *Model[T]) WasChanged(attributes ...string) bool {
	return hasChanges(m.changes, attributes)
}

// DiscardChanges answers Model::discardChanges.
func (m *Model[T]) DiscardChanges() error {
	original := copyMap(m.original)
	if err := m.SetRawAttributes(original, false); err != nil {
		return err
	}
	m.original = original
	m.changes = nil
	m.previous = nil
	return nil
}

// hasChanges answers Model::hasChanges.
func hasChanges(changes map[string]any, attributes []string) bool {
	if len(attributes) == 0 {
		return len(changes) > 0
	}
	for _, attribute := range attributes {
		if _, ok := changes[attribute]; ok {
			return true
		}
	}
	return false
}

// OriginalIsEquivalent answers Model::originalIsEquivalent.
//
// The PHP walks a ladder of cast types because a value read from PDO is a string
// and the one the developer assigned is not: "1" and 1 have to compare equal. Go
// has no such gap -- the field has one type, and both values went through assign
// to reach it -- so the whole ladder collapses into a comparison, and the one
// case Go adds is the uncomparable field (a slice, a map), which DeepEqual
// answers.
func (m *Model[T]) OriginalIsEquivalent(key string) bool {
	original, ok := m.original[key]
	if !ok {
		return false
	}
	current := m.GetAttribute(key)
	if current == nil || original == nil {
		return current == nil && original == nil
	}
	return reflect.DeepEqual(current, original)
}

// Only answers Model::only: a subset of the row, by column.
func (m *Model[T]) Only(attributes ...string) map[string]any {
	out := make(map[string]any, len(attributes))
	for _, attribute := range attributes {
		out[attribute] = m.GetAttribute(attribute)
	}
	return out
}

// Except answers Model::except: the row without the named columns.
func (m *Model[T]) Except(attributes ...string) map[string]any {
	out := map[string]any{}
	for key := range m.GetAttributes() {
		if slices.Contains(attributes, key) {
			continue
		}
		out[key] = m.GetAttribute(key)
	}
	return out
}

// GetHidden answers Model::getHidden.
func (m *Model[T]) GetHidden() []string { return slices.Clone(m.hidden) }

// SetHidden answers Model::setHidden.
func (m *Model[T]) SetHidden(hidden ...string) *Model[T] {
	m.hidden = slices.Clone(hidden)
	return m
}

// GetVisible answers Model::getVisible.
func (m *Model[T]) GetVisible() []string { return slices.Clone(m.visible) }

// SetVisible answers Model::setVisible.
func (m *Model[T]) SetVisible(visible ...string) *Model[T] {
	m.visible = slices.Clone(visible)
	return m
}

// MakeVisible answers Model::makeVisible: it takes the named columns out of
// $hidden, and adds them to $visible when that list is in use.
func (m *Model[T]) MakeVisible(attributes ...string) *Model[T] {
	m.hidden = slices.DeleteFunc(slices.Clone(m.hidden), func(key string) bool {
		return slices.Contains(attributes, key)
	})
	if len(m.visible) > 0 {
		m.visible = appendUnique(m.visible, attributes...)
	}
	return m
}

// MakeHidden answers Model::makeHidden.
func (m *Model[T]) MakeHidden(attributes ...string) *Model[T] {
	m.hidden = appendUnique(m.hidden, attributes...)
	return m
}

// Append answers Model::append: a name that is serialised with the row without
// being a column.
//
// In PHP the value comes from an accessor method. Here it comes from a raw
// attribute or a loaded relation -- the two things a Go model can hold that the
// struct does not declare.
func (m *Model[T]) Append(attributes ...string) *Model[T] {
	m.appends = appendUnique(m.appends, attributes...)
	return m
}

// GetAppends answers Model::getAppends.
func (m *Model[T]) GetAppends() []string { return slices.Clone(m.appends) }

// SetAppends answers Model::setAppends.
func (m *Model[T]) SetAppends(appends ...string) *Model[T] {
	m.appends = slices.Clone(appends)
	return m
}

// HasAppended answers Model::hasAppended.
func (m *Model[T]) HasAppended(attribute string) bool {
	return slices.Contains(m.appends, attribute)
}

func appendUnique(list []string, values ...string) []string {
	out := slices.Clone(list)
	for _, value := range values {
		if !slices.Contains(out, value) {
			out = append(out, value)
		}
	}
	return out
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// sortedKeys is the answer to a Go map having no order where a PHP array does.
//
// Illuminate ksorts the values of a batch insert for exactly this reason, so the
// same order is used everywhere a map becomes a column list: what is compiled
// and what is bound are then derived from the same sequence.
func sortedKeys(in map[string]any) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
