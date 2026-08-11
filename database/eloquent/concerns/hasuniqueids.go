package concerns

import "github.com/arandu-io/hesape/str"

// HasUniqueIDs answers
// Illuminate\Database\Eloquent\Concerns\HasUniqueIds. The PHP spells the last
// word Ids.
//
// A model that generates its own key before the insert rather than reading one
// back from the engine. That matters beyond tidiness: a key the application
// chose can be written into the parent and the children in the same
// transaction, and it does not need the round trip that `insert ... returning
// id` costs on every row.
type HasUniqueIDs struct {
	// Generator is what NewUniqueID answers with. HasUUIDs and HasULIDs set it;
	// a model with its own scheme sets its own. It is a field and not the
	// method, because Go cannot have both under one name.
	Generator func() string

	// Columns is what UniqueIDs answers: the columns to fill. Empty means the
	// primary key alone.
	Columns []string
}

// NewUniqueID answers HasUniqueIds::newUniqueId. The PHP spells it newUniqueId.
func (h *HasUniqueIDs) NewUniqueID() string {
	if h.Generator == nil {
		return ""
	}
	return h.Generator()
}

// UniqueIDs answers HasUniqueIds::uniqueIds.
func (h *HasUniqueIDs) UniqueIDs() []string { return h.Columns }

// GetKeyType answers HasUniqueStringIds::getKeyType: a generated key is a
// string, whatever the model would otherwise have said.
func (h *HasUniqueIDs) GetKeyType() string {
	if h.UsesUniqueIDs() {
		return "string"
	}
	return "int"
}

// GetIncrementing answers HasUniqueStringIds::getIncrementing: a model that
// generates its own key does not ask the engine for one.
func (h *HasUniqueIDs) GetIncrementing() bool { return !h.UsesUniqueIDs() }

// UsesUniqueIDs answers HasUniqueIds::usesUniqueIds.
func (h *HasUniqueIDs) UsesUniqueIDs() bool { return h.Generator != nil }

// SetUniqueIDs answers HasUniqueIds::setUniqueIds: it fills the columns that
// are still empty, and leaves the ones somebody set.
func (h *HasUniqueIDs) SetUniqueIDs(attributes map[string]any, keyName string) {
	if !h.UsesUniqueIDs() {
		return
	}

	columns := h.Columns
	if len(columns) == 0 {
		columns = []string{keyName}
	}

	for _, column := range columns {
		if value, ok := attributes[column]; ok && value != nil && value != "" {
			continue
		}
		attributes[column] = h.NewUniqueID()
	}
}

// HasUUIDs answers Illuminate\Database\Eloquent\Concerns\HasUuids: keys that
// are UUIDs.
//
// The generator is hesape/str, which the collection already has, rather than a
// second implementation here -- and str is where the test seams live
// (FreezeUUIDs), so a test that needs a predictable key has one place to ask.
func HasUUIDs() HasUniqueIDs {
	return HasUniqueIDs{Generator: str.UUID}
}

// HasULIDs answers Illuminate\Database\Eloquent\Concerns\HasUlids: keys that
// are ULIDs.
//
// A ULID sorts by creation time as a string, which is what makes it the better
// default for a primary key: a random UUID as a clustered index scatters every
// insert across the tree, and the write amplification shows up as a slow table
// long before anybody suspects the key.
func HasULIDs() HasUniqueIDs {
	return HasUniqueIDs{Generator: str.ULID}
}

// IsValidUniqueID answers HasUuids::isValidUniqueId and
// HasUlids::isValidUniqueId, which route binding uses to refuse a key that
// cannot be one before it reaches the database.
func IsValidUniqueID(value string) bool {
	return str.IsUUID(value) || str.IsULID(value)
}
