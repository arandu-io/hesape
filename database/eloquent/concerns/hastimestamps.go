package concerns

import (
	"sync"
	"time"
)

// CreatedAt is the default created_at column name.
const CreatedAt = "created_at"

// UpdatedAt is the default updated_at column name.
const UpdatedAt = "updated_at"

// HasTimestamps stamps created_at on an insert and updated_at on both.
//
// The interesting part is what it does not do: the values are taken from the
// process clock, which means two application servers with a drifting clock write
// rows whose order does not match the order they happened in. The alternative --
// the database's clock, through a function in the statement -- makes the value
// unreadable before the write and unavailable to the model that just saved.
type HasTimestamps struct {
	timestamps    bool
	initialized   bool
	createdColumn string
	updatedColumn string
}

// ignoringTimestamps answers HasTimestamps::$ignoreTimestampsOn, kept as a
// count rather than a class list: there are no class names here, and what the
// PHP asks it is only ever "are we inside withoutTimestamps".
var ignoringTimestamps = struct {
	sync.RWMutex
	depth int
}{}

// InitializeHasTimestamps answers HasTimestamps::initializeHasTimestamps.
//
// Go's zero value for a bool is false and the PHP's default is true, so the
// flag is initialized on first use. A model that wants them off calls
// SetTimestamps(false), which is the PHP's `public $timestamps = false`.
func (h *HasTimestamps) InitializeHasTimestamps() {
	if !h.initialized {
		h.timestamps = true
		h.initialized = true
	}
}

// SetTimestamps answers `public $timestamps` as a setter, because Go has no
// property a subclass can redeclare.
func (h *HasTimestamps) SetTimestamps(enabled bool) {
	h.timestamps = enabled
	h.initialized = true
}

// UsesTimestamps answers HasTimestamps::usesTimestamps.
func (h *HasTimestamps) UsesTimestamps() bool {
	h.InitializeHasTimestamps()
	return h.timestamps && !IsIgnoringTimestamps()
}

// UpdateTimestamps answers HasTimestamps::updateTimestamps.
//
// It takes the attribute bag rather than reaching for the model, because a
// trait in Go cannot reach the struct it was embedded in. exists decides
// whether created_at is stamped, which is the PHP's `! $this->exists`.
func (h *HasTimestamps) UpdateTimestamps(attributes map[string]any, exists bool) {
	if !h.UsesTimestamps() {
		return
	}

	now := h.FreshTimestamp()

	if updated := h.GetUpdatedAtColumn(); updated != "" {
		attributes[updated] = now
	}
	if created := h.GetCreatedAtColumn(); !exists && created != "" {
		attributes[created] = now
	}
}

// SetCreatedAt answers HasTimestamps::setCreatedAt.
func (h *HasTimestamps) SetCreatedAt(attributes map[string]any, value time.Time) {
	attributes[h.GetCreatedAtColumn()] = value
}

// SetUpdatedAt answers HasTimestamps::setUpdatedAt.
func (h *HasTimestamps) SetUpdatedAt(attributes map[string]any, value time.Time) {
	attributes[h.GetUpdatedAtColumn()] = value
}

// FreshTimestamp answers HasTimestamps::freshTimestamp.
//
// In UTC, always. A timestamp stored in the process's local zone is a row whose
// meaning changes when the container moves region, and the conversion back is
// guesswork because the zone was never written down.
func (h *HasTimestamps) FreshTimestamp() time.Time { return time.Now().UTC() }

// FreshTimestampString answers HasTimestamps::freshTimestampString.
func (h *HasTimestamps) FreshTimestampString() string {
	return h.FreshTimestamp().Format("2006-01-02 15:04:05")
}

// GetCreatedAtColumn answers HasTimestamps::getCreatedAtColumn.
func (h *HasTimestamps) GetCreatedAtColumn() string {
	if h.createdColumn != "" {
		return h.createdColumn
	}
	return CreatedAt
}

// GetUpdatedAtColumn answers HasTimestamps::getUpdatedAtColumn.
func (h *HasTimestamps) GetUpdatedAtColumn() string {
	if h.updatedColumn != "" {
		return h.updatedColumn
	}
	return UpdatedAt
}

// GetDates answers HasAttributes::getDates: the columns that hold a date
// without anybody having declared a cast for them.
//
// The PHP declares it on HasAttributes and reaches usesTimestamps and the two
// column names through $this. A trait here cannot reach the model it was
// embedded in, and all three live on HasTimestamps, so this is where the
// method is -- the same answer, asked of the struct that has the fields.
func (h *HasTimestamps) GetDates() []string {
	if !h.UsesTimestamps() {
		return []string{}
	}
	return []string{h.GetCreatedAtColumn(), h.GetUpdatedAtColumn()}
}

// SetTimestampColumns answers PHP's `const CREATED_AT` and `const UPDATED_AT`,
// which a model redefines. Go has no constant a type can override, so the names
// are fields.
func (h *HasTimestamps) SetTimestampColumns(created, updated string) {
	h.createdColumn, h.updatedColumn = created, updated
}

// WithoutTimestamps answers HasTimestamps::withoutTimestamps.
func WithoutTimestamps(callback func() error) error {
	ignoringTimestamps.Lock()
	ignoringTimestamps.depth++
	ignoringTimestamps.Unlock()

	defer func() {
		ignoringTimestamps.Lock()
		ignoringTimestamps.depth--
		ignoringTimestamps.Unlock()
	}()

	return callback()
}

// IsIgnoringTimestamps answers HasTimestamps::isIgnoringTimestamps.
func IsIgnoringTimestamps() bool {
	ignoringTimestamps.RLock()
	defer ignoringTimestamps.RUnlock()
	return ignoringTimestamps.depth > 0
}
