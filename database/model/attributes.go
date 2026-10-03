package model

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
)

// GetAttributes returns every column of the row, as the database sees it.
//
// The row lives in the entity struct, so the map is built from it -- plus the
// raw attributes a column with no field behind it left behind (a withCount
// alias, a column a migration added and the struct has not caught up with).
func (m *Model) GetAttributes() map[string]any {
	entity, ok := entityOf(m)
	if !ok {
		return map[string]any{}
	}
	fields := m.r.table.schema.fields
	out := make(map[string]any, len(fields)+len(m.r.attributes))
	for _, f := range fields {
		out[f.column] = valueAt(entity, f)
	}
	for key, value := range m.r.attributes {
		out[key] = value
	}
	return out
}

// SetRawAttributes replaces the row without checking anything, and optionally
// syncs the original.
//
// It is what hydration uses, and it is the only path that puts a value the
// caller did not declare on the model: a key with no field behind it is kept as
// a raw attribute rather than dropped, because a select the caller wrote has a
// reason for every column in it.
func (m *Model) SetRawAttributes(attributes map[string]any, sync bool) error {
	entity, ok := entityOf(m)
	if !ok {
		return ErrUnwired
	}
	resetEntity(m, entity)
	m.r.attributes = nil
	if err := setAttributes(m, attributes, true); err != nil {
		return err
	}
	if sync {
		m.SyncOriginal()
	}
	return nil
}

// Fill writes the columns the entity declares and drops the keys it does not
// know.
//
// It never writes the tenant column, and never the primary key of a row that
// already exists: a form posted back into Update or UpdateOrCreate carries
// whatever keys its sender added, and neither of those is the sender's to
// choose. Both are skipped without error, like an unknown key. ForceFill writes
// them.
//
// There is no allowlist to consult beyond the struct itself: an unexported
// field is unreachable to reflection, so the allowlist is the initial letter of
// the field and the compiler keeps it (see the package comment).
//
// The error is the other failure a typed model can have -- a value that does
// not fit the field.
func (m *Model) Fill(attributes map[string]any) error {
	return setAttributes(m, attributes, false)
}

// ForceFill writes the columns the entity declares, and keeps the keys it does
// not know as raw attributes instead of dropping them the way Fill does. It
// still cannot reach an unexported field, because nothing can.
func (m *Model) ForceFill(attributes map[string]any) error {
	return setAttributes(m, attributes, true)
}

// setAttributes writes a map onto the entity. keepUnknown decides what happens
// to a key with no field behind it: Fill drops it, ForceFill and
// SetRawAttributes keep it.
func setAttributes(m *Model, attributes map[string]any, keepUnknown bool) error {
	if len(attributes) == 0 {
		return nil
	}

	// The entity's reflect.Value is taken once rather than once per column, and
	// the walk is over the schema rather than over a sorted copy of the map's
	// keys. Sorting existed to make the first conversion error deterministic; so
	// does declaration order, and it costs no allocation and no sort per row.
	entity, ok := entityOf(m)
	if !ok {
		return ErrUnwired
	}
	schema := m.r.table.schema

	written := 0
	for i := range schema.fields {
		f := schema.fields[i]
		value, present := attributes[f.column]
		if !present {
			continue
		}
		written++
		if !keepUnknown && guarded(m, f.column) {
			continue
		}
		dst, settable := settableAt(entity, f)
		if !settable {
			continue
		}
		if err := assign(dst, value); err != nil {
			return fmt.Errorf("model: %s.%s: %w", m.r.table.name, f.column, err)
		}
	}

	// Whatever the schema did not claim. A key with no field behind it is kept
	// as a raw attribute or reported, and the order it is reported in is the
	// sorted one, because a violation message that changes between runs is a
	// message nobody can assert on.
	if written == len(attributes) {
		return nil
	}
	var discarded []string
	for _, key := range sortedKeys(attributes) {
		if _, ok := schema.byName[key]; ok {
			continue
		}
		if keepUnknown {
			if m.r.attributes == nil {
				m.r.attributes = map[string]any{}
			}
			m.r.attributes[key] = attributes[key]
			continue
		}
		discarded = append(discarded, key)
	}
	return handleDiscardedAttributeViolation(m, discarded)
}

// SetAttribute converts value to the field's type and assigns it, or reports
// the conversion error. A column the entity does not declare is kept as a raw
// attribute instead.
func (m *Model) SetAttribute(key string, value any) error {
	known, err := setAttribute(m, key, value)
	if err != nil {
		return err
	}
	if !known {
		if m.r.attributes == nil {
			m.r.attributes = map[string]any{}
		}
		m.r.attributes[key] = value
	}
	return nil
}

// setAttribute assigns value to the field behind key, and reports whether there
// is one.
func setAttribute(m *Model, key string, value any) (bool, error) {
	entity, ok := entityOf(m)
	if !ok {
		return false, ErrUnwired
	}
	f, ok := m.r.table.schema.field(key)
	if !ok {
		return false, nil
	}
	dst, settable := settableAt(entity, f)
	if !settable {
		return false, nil
	}
	if err := assign(dst, value); err != nil {
		return true, fmt.Errorf("model: %s.%s: %w", m.r.table.name, key, err)
	}
	return true, nil
}

// GetAttribute returns the value for key: a column value if key names a field,
// else a raw attribute, else a loaded relation.
//
// A key that matches none of those reads as nil. PreventAccessingMissingAttributes
// turns that into a reported violation, though it catches much less here than
// it would need to elsewhere: a typo like user.Naem fails to compile, so it
// never reaches this check at all.
func (m *Model) GetAttribute(key string) any {
	entity, live := entityOf(m)
	if !live {
		return nil
	}
	if f, ok := m.r.table.schema.field(key); ok {
		return valueAt(entity, f)
	}
	if value, ok := m.r.attributes[key]; ok {
		return value
	}
	if related, ok := m.r.relations[key]; ok {
		return related
	}
	handleMissingAttributeViolation(m, key)
	return nil
}

// unsetAttribute removes a raw attribute.
//
// It reaches only the attributes a column has no field behind: a struct field
// cannot be removed, and setting it to its zero value would be a different
// thing said with the same word. A pivot row, whose columns are not known until
// run time, is what needs this.
func unsetAttribute(m *Model, key string) { delete(m.r.attributes, key) }

// attributesToArray returns the row as it is serialised, with the hidden
// columns removed and the appended ones added.
func attributesToArray(m *Model) map[string]any {
	attributes := m.GetAttributes()
	out := make(map[string]any, len(attributes))
	for key, value := range attributes {
		if !isVisible(m, key) {
			continue
		}
		out[key] = value
	}
	for _, key := range appendsList(m) {
		if !isVisible(m, key) {
			continue
		}
		out[key] = m.GetAttribute(key)
	}
	return out
}

// isVisible reports whether key should be serialised: the visible list wins
// when it is set, otherwise the hidden list removes.
func isVisible(m *Model, key string) bool {
	if visible := visibleList(m); len(visible) > 0 {
		return slices.Contains(visible, key)
	}
	return !slices.Contains(hiddenList(m), key)
}

// ToArray returns the serialised row together with the loaded relations.
func (m *Model) ToArray() map[string]any {
	if !live(m) {
		return map[string]any{}
	}
	out := attributesToArray(m)
	for name, related := range m.r.relations {
		if !isVisible(m, name) {
			continue
		}
		out[name] = related
	}
	return out
}

// ToJSON encodes the serialised row as JSON. Go initialisms are upper case,
// hence ToJSON rather than ToJson, and it returns bytes rather than a string.
func (m *Model) ToJSON() ([]byte, error) {
	out, err := json.Marshal(m.ToArray())
	if err != nil {
		table := ""
		if live(m) {
			table = m.r.table.name
		}
		return nil, fmt.Errorf("model: encoding %s: %w", table, err)
	}
	return out, nil
}

// GetOriginal returns the row as it was when it was last synced.
func (m *Model) GetOriginal() map[string]any {
	if !live(m) {
		return map[string]any{}
	}
	return copyMap(m.r.original)
}

// SyncOriginal replaces the original snapshot with the row's current values.
func (m *Model) SyncOriginal() {
	if live(m) {
		m.r.original = m.GetAttributes()
	}
}

// syncOriginalAttributes replaces the original snapshot for the named columns
// with their current values.
func syncOriginalAttributes(m *Model, attributes ...string) {
	current := m.GetAttributes()
	if m.r.original == nil {
		m.r.original = map[string]any{}
	}
	for _, key := range attributes {
		m.r.original[key] = current[key]
	}
}

// syncChanges records the current dirty columns as the last save's changes,
// and captures what each one held before it.
func syncChanges(m *Model) {
	m.r.changes = m.GetDirty()
	m.r.previous = map[string]any{}
	for key := range m.r.changes {
		if original, ok := m.r.original[key]; ok {
			m.r.previous[key] = original
		}
	}
}

// GetDirty returns the columns that differ from the original.
func (m *Model) GetDirty() map[string]any {
	dirty := map[string]any{}
	if !live(m) {
		return dirty
	}
	for key, value := range m.GetAttributes() {
		if !originalIsEquivalent(m, key, value) {
			dirty[key] = value
		}
	}
	return dirty
}

// GetChanges returns what changed on the last save.
func (m *Model) GetChanges() map[string]any {
	if !live(m) {
		return map[string]any{}
	}
	return copyMap(m.r.changes)
}

// IsDirty reports whether the given columns differ from the original. With no
// argument it asks about the whole row.
func (m *Model) IsDirty(attributes ...string) bool {
	return hasChanges(m.GetDirty(), attributes)
}

// IsClean reports the opposite of IsDirty.
func (m *Model) IsClean(attributes ...string) bool { return !m.IsDirty(attributes...) }

// WasChanged reports whether the last save touched these columns.
func (m *Model) WasChanged(attributes ...string) bool {
	if !live(m) {
		return false
	}
	return hasChanges(m.r.changes, attributes)
}

// hasChanges reports whether changes contains any of attributes, or is
// non-empty when attributes is empty.
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

// originalIsEquivalent reports whether current, the value of key now, equals
// its original value.
//
// The field has one static type, and both the current and the original value
// went through assign to reach it, so this is a plain comparison rather than a
// ladder of type coercions. The one case a plain comparison cannot handle is an
// uncomparable field (a slice, a map), which reflect.DeepEqual handles instead.
func originalIsEquivalent(m *Model, key string, current any) bool {
	original, ok := m.r.original[key]
	if !ok {
		return false
	}
	if current == nil || original == nil {
		return current == nil && original == nil
	}
	return reflect.DeepEqual(current, original)
}

// MakeVisible takes the named columns out of the hidden list, and adds them to
// the visible list when that list is in use.
func (m *Model) MakeVisible(attributes ...string) {
	if !live(m) {
		return
	}
	ownLists(m)
	m.r.hidden = slices.DeleteFunc(m.r.hidden, func(key string) bool {
		return slices.Contains(attributes, key)
	})
	if len(m.r.visible) > 0 {
		m.r.visible = appendUnique(m.r.visible, attributes...)
	}
}

// MakeHidden adds the named columns to the hidden list.
func (m *Model) MakeHidden(attributes ...string) {
	if !live(m) {
		return
	}
	ownLists(m)
	m.r.hidden = appendUnique(m.r.hidden, attributes...)
}

// ownLists gives the row its own copy of the table's serialisation lists, the
// first time one of them changes.
func ownLists(m *Model) {
	if m.r.ownLists {
		return
	}
	m.r.hidden = slices.Clone(m.r.table.hidden)
	m.r.visible = slices.Clone(m.r.table.visible)
	m.r.appends = slices.Clone(m.r.table.appends)
	m.r.ownLists = true
}

// hiddenList, visibleList and appendsList are the lists this row serialises
// by: its own once it changed one, and the table's until then.
func hiddenList(m *Model) []string {
	if m.r.ownLists {
		return m.r.hidden
	}
	return m.r.table.hidden
}

func visibleList(m *Model) []string {
	if m.r.ownLists {
		return m.r.visible
	}
	return m.r.table.visible
}

func appendsList(m *Model) []string {
	if m.r.ownLists {
		return m.r.appends
	}
	return m.r.table.appends
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
	maps.Copy(out, in)
	return out
}

// sortedKeys is the answer to a Go map having no order.
//
// The same order is used everywhere a map becomes a column list, so what is
// compiled and what is bound are derived from the same sequence.
func sortedKeys(in map[string]any) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// resetEntity clears the entity's columns and puts the model back afterwards:
// the model is inside the entity, so zeroing the struct zeroes it too, and the
// row it points at is what the caller holds.
func resetEntity(m *Model, entity reflect.Value) {
	saved := *m
	entity.SetZero()
	*m = saved
}

// guarded reports whether Fill leaves column alone: the tenant column always,
// because the tenant comes from the Grant at the write and from nowhere else,
// and the primary key of a row that exists, because a map of request values
// that names the key would otherwise re-key the row it was meant to edit.
//
// A row that does not exist yet still takes its key from Fill, since for a key
// the database does not generate that map is where a new row's key comes from.
// ForceFill and SetRawAttributes guard nothing: they are the explicit paths, and
// the second is what a row read from the database is built with.
func guarded(m *Model, column string) bool {
	t := m.r.table
	if column == t.tenantColumn && column != "" {
		return true
	}
	return m.r.exists && column == t.keyName
}
