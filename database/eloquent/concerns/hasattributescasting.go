package concerns

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/database/eloquent/casts"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/support/arr"
)

// The other half of HasAttributes: what a column is declared to be, the accessor
// and mutator registry, and the four encoders a cast reaches on the way in and
// on the way out.
//
// The cast implementations are not here -- they are eloquent/casts, one place a
// column's meaning is written. What is here is the declaration and the lookups
// over it.

// DefaultDateFormat is the format GetDateFormat falls back to: "Y-m-d H:i:s"
// in spelling, for every engine this framework carries.
//
// Go writes a layout by example rather than by letter, so the same format
// is spelled with the reference time.
const DefaultDateFormat = "2006-01-02 15:04:05"

// modelEncrypter holds the process-wide encrypter, behind a mutex because a
// model is read from more than one goroutine.
var modelEncrypter struct {
	sync.RWMutex
	encrypter *encryption.Encrypter
}

// EncryptUsing sets the encrypter the encrypted casts reach for.
//
// It is a package function because there is one encrypter per process rather
// than one per model. A nil encrypter clears it.
func EncryptUsing(encrypter *encryption.Encrypter) {
	modelEncrypter.Lock()
	defer modelEncrypter.Unlock()
	modelEncrypter.encrypter = encrypter
}

// CurrentEncrypter returns the encrypter EncryptUsing set.
//
// There is no fallback: with none set it returns nil, and the caller gets
// an error rather than an encrypter conjured from a global registry.
func CurrentEncrypter() *encryption.Encrypter {
	modelEncrypter.RLock()
	defer modelEncrypter.RUnlock()
	return modelEncrypter.encrypter
}

// MergeCasts adds cast declarations to the ones already there.
//
// It is also how a model declares its casts in the first place: merging onto an
// empty map is that declaration, and there is one way to do it.
func (h *HasAttributes) MergeCasts(declared map[string]any) {
	if h.casts == nil {
		h.casts = map[string]any{}
	}
	for key, cast := range declared {
		h.casts[key] = cast
	}
}

// GetCasts returns the declared casts.
//
// The primary key is not prepended with a cast of its own: the key is a
// field on the entity, with a Go type of its own, so what comes back is
// exactly what was declared.
func (h *HasAttributes) GetCasts() map[string]any {
	if h.casts == nil {
		h.casts = map[string]any{}
	}
	return h.casts
}

// Casts is the method a model overrides to declare its casts. It reads the
// same map GetCasts does.
func (h *HasAttributes) Casts() map[string]any { return h.GetCasts() }

// HasCast reports whether key is cast.
//
// With no types it asks whether the column is cast at all; with types it
// asks whether the cast is one of them.
func (h *HasAttributes) HasCast(key string, types ...string) bool {
	if _, ok := h.GetCasts()[key]; !ok {
		return false
	}
	if len(types) == 0 {
		return true
	}

	cast := h.GetCastType(key)
	for _, want := range types {
		if cast == want {
			return true
		}
	}
	return false
}

// GetCastType returns the cast's name, with the three parameterised forms
// folded into one word: "date:Y" is "custom_datetime", and so on.
//
// For a cast held as a value rather than a name, it returns the Go type's
// name instead.
func (h *HasAttributes) GetCastType(key string) string {
	cast, ok := h.GetCasts()[key]
	if !ok {
		return ""
	}

	name, ok := cast.(string)
	if !ok {
		return fmt.Sprintf("%T", cast)
	}

	switch {
	case isCustomDateTimeCast(name):
		return "custom_datetime"
	case isImmutableCustomDateTimeCast(name):
		return "immutable_custom_datetime"
	case isDecimalCast(name):
		return "decimal"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// IsDateCastable reports whether key is cast to a date or datetime.
func (h *HasAttributes) IsDateCastable(key string) bool {
	return h.HasCast(key, "date", "datetime", "immutable_date", "immutable_datetime")
}

// IsJsonCastable reports whether key is cast to a JSON-backed type.
func (h *HasAttributes) IsJsonCastable(key string) bool {
	return h.HasCast(key,
		"array", "json", "json:unicode", "object", "collection",
		"encrypted:array", "encrypted:collection", "encrypted:json", "encrypted:object",
	)
}

// IsEncryptedCastable reports whether key is cast to an encrypted type.
func (h *HasAttributes) IsEncryptedCastable(key string) bool {
	return h.HasCast(key,
		"encrypted", "encrypted:array", "encrypted:collection",
		"encrypted:json", "encrypted:object",
	)
}

func isCustomDateTimeCast(cast string) bool {
	return strings.HasPrefix(cast, "date:") || strings.HasPrefix(cast, "datetime:")
}

func isImmutableCustomDateTimeCast(cast string) bool {
	return strings.HasPrefix(cast, "immutable_date:") || strings.HasPrefix(cast, "immutable_datetime:")
}

func isDecimalCast(cast string) bool { return strings.HasPrefix(cast, "decimal:") }

// SetAttributeMutator registers the Attribute for a key.
//
// Go cannot read a method's return type by name, so the model writes down which
// key an Attribute belongs to, and every has*Mutator lookup below reads this
// registry. A nil attribute removes the registration.
func (h *HasAttributes) SetAttributeMutator(key string, attribute *casts.Attribute) {
	if attribute == nil {
		delete(h.mutators, key)
		h.mutatedCached = false
		return
	}
	if h.mutators == nil {
		h.mutators = map[string]*casts.Attribute{}
	}
	h.mutators[key] = attribute
	h.mutatedCached = false
}

// GetAttributeMutator returns the Attribute registered for a key, or nil.
func (h *HasAttributes) GetAttributeMutator(key string) *casts.Attribute {
	return h.mutators[key]
}

// HasAttributeMutator reports whether key has an Attribute registered.
func (h *HasAttributes) HasAttributeMutator(key string) bool {
	return h.mutators[key] != nil
}

// HasAttributeGetMutator reports whether key's Attribute has a getter.
func (h *HasAttributes) HasAttributeGetMutator(key string) bool {
	return h.mutators[key].HasGet()
}

// HasAttributeSetMutator reports whether key's Attribute has a setter.
func (h *HasAttributes) HasAttributeSetMutator(key string) bool {
	return h.mutators[key].HasSet()
}

// HasGetMutator reports whether the key has an accessor registered.
//
// There is one registry, so this and HasAttributeGetMutator read the same thing
// and there is still only one way to declare the accessor.
func (h *HasAttributes) HasGetMutator(key string) bool { return h.HasAttributeGetMutator(key) }

// HasSetMutator reports whether the key has a mutator registered, reading
// the same registry as HasAttributeSetMutator and for the same reason.
func (h *HasAttributes) HasSetMutator(key string) bool { return h.HasAttributeSetMutator(key) }

// HasAnyGetMutator reports whether key has a getter under either name.
func (h *HasAttributes) HasAnyGetMutator(key string) bool {
	return h.HasGetMutator(key) || h.HasAttributeGetMutator(key)
}

// MutateAttribute returns the stored value read through its accessor.
//
// It returns the value untouched when there is no accessor.
func (h *HasAttributes) MutateAttribute(key string, value any) (any, error) {
	mutator := h.mutators[key]
	if !mutator.HasGet() {
		return value, nil
	}
	return mutator.Get(value, h.GetAttributes())
}

// SetMutatedAttributeValue writes value through key's setter, applying the
// columns it returns to the attribute bag. With no setter it sets key
// directly.
func (h *HasAttributes) SetMutatedAttributeValue(key string, value any) error {
	mutator := h.mutators[key]
	if !mutator.HasSet() {
		h.SetAttribute(key, value)
		return nil
	}

	written, err := mutator.Set(value, h.GetAttributes())
	if err != nil {
		return err
	}
	for column, mutated := range written {
		h.SetAttribute(column, mutated)
	}
	return nil
}

// GetMutatedAttributes returns the keys that have an accessor, computed
// once and kept.
func (h *HasAttributes) GetMutatedAttributes() []string {
	if !h.mutatedCached {
		h.CacheMutatedAttributes()
	}
	return h.mutated
}

// CacheMutatedAttributes recomputes the list of keys that have a mutator.
//
// The registry is per model, so this is a method rather than something shared by
// every model of a type. The list comes back sorted, so it is the same on every
// run.
func (h *HasAttributes) CacheMutatedAttributes() []string {
	mutated := make([]string, 0, len(h.mutators))
	for key, attribute := range h.mutators {
		if attribute.HasGet() {
			mutated = append(mutated, key)
		}
	}
	sort.Strings(mutated)

	h.mutated = mutated
	h.mutatedCached = true
	return h.mutated
}

// GetDateFormat returns the layout dates are read and written in, defaulting
// to DefaultDateFormat.
func (h *HasAttributes) GetDateFormat() string {
	if h.dateFormat == "" {
		return DefaultDateFormat
	}
	return h.dateFormat
}

// SetDateFormat replaces the layout dates are read and written in.
//
// The format is a Go layout -- the reference time, "2006-01-02" -- rather
// than letter codes. The framework speaks one date format language, and it
// is the standard library's.
func (h *HasAttributes) SetDateFormat(format string) { h.dateFormat = format }

// FromDateTime returns value as the string the column stores.
//
// An empty value comes back empty, and a value that cannot be read as a time is
// an error.
func (h *HasAttributes) FromDateTime(value any) (string, error) {
	if value == nil || value == "" {
		return "", nil
	}

	moment, err := asDateTime(value)
	if err != nil {
		return "", err
	}
	return moment.Format(h.GetDateFormat()), nil
}

// asDateTime returns whatever a column or a caller carries, as a time.
//
// It accepts a time.Time, a unix timestamp, or a string in one of the
// layouts parseDateTime tries.
func asDateTime(value any) (time.Time, error) {
	switch moment := value.(type) {
	case nil:
		return time.Time{}, fmt.Errorf("concerns: cannot read a date from nil")
	case time.Time:
		return moment, nil
	case *time.Time:
		if moment == nil {
			return time.Time{}, fmt.Errorf("concerns: cannot read a date from nil")
		}
		return *moment, nil
	case int:
		return time.Unix(int64(moment), 0), nil
	case int64:
		return time.Unix(moment, 0), nil
	case float64:
		return time.Unix(int64(moment), 0), nil
	case []byte:
		return parseDateTime(string(moment))
	case string:
		return parseDateTime(moment)
	default:
		return time.Time{}, fmt.Errorf("concerns: cannot read a date from %T", value)
	}
}

// parseDateTime reads the formats a column comes back in: the storage
// format first, then a bare date, then RFC 3339, which is what a JSON
// column holds.
func parseDateTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("concerns: cannot read a date from an empty string")
	}

	layouts := []string{
		DefaultDateFormat,
		"2006-01-02",
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999",
	}
	for _, layout := range layouts {
		if moment, err := time.Parse(layout, value); err == nil {
			return moment, nil
		}
	}
	return time.Time{}, fmt.Errorf("concerns: %q is not a date this model can read", value)
}

// FromJson decodes value: a JSON object decodes to map[string]any. A nil or
// empty column decodes to nil.
func (h *HasAttributes) FromJson(value any) (any, error) {
	switch stored := value.(type) {
	case nil:
		return nil, nil
	case string:
		if stored == "" {
			return nil, nil
		}
		return casts.Decode([]byte(stored))
	case []byte:
		if len(stored) == 0 {
			return nil, nil
		}
		return casts.Decode(stored)
	default:
		return nil, fmt.Errorf("concerns: cannot read JSON from %T", value)
	}
}

// AsJson returns value as the string a JSON column stores.
func (h *HasAttributes) AsJson(value any) (string, error) {
	encoded, err := casts.Encode(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// FromFloat returns value as a float64.
//
// "Infinity", "-Infinity" and "NaN" are matched as text before the value is
// parsed as a number: the three ways a float column can come back
// unparseable by strconv.ParseFloat alone.
func (h *HasAttributes) FromFloat(value any) (float64, error) {
	switch number := value.(type) {
	case nil:
		return 0, fmt.Errorf("concerns: cannot read a float from nil")
	case float64:
		return number, nil
	case float32:
		return float64(number), nil
	case int:
		return float64(number), nil
	case int64:
		return float64(number), nil
	}

	text := strings.TrimSpace(fmt.Sprint(value))
	switch text {
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}

	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("concerns: %q is not a float: %w", text, err)
	}
	return number, nil
}

// FromEncryptedString decrypts value with the current encrypter.
//
// It reports an error when no encrypter is set, rather than falling back to
// one conjured from a global registry: a value that cannot be decrypted
// must not come back looking like plaintext.
func (h *HasAttributes) FromEncryptedString(value string) (string, error) {
	encrypter := CurrentEncrypter()
	if encrypter == nil {
		return "", fmt.Errorf("concerns: no encrypter is set; call EncryptUsing first")
	}
	return encrypter.DecryptString(value)
}

// CastAttributeAsEncryptedString encrypts value with the current encrypter.
func (h *HasAttributes) CastAttributeAsEncryptedString(key string, value string) (string, error) {
	encrypter := CurrentEncrypter()
	if encrypter == nil {
		return "", fmt.Errorf("concerns: no encrypter is set; call EncryptUsing first")
	}
	return encrypter.EncryptString(value)
}

// FillJsonAttribute writes into one path of a JSON column, such as
// "options->notifications->email".
//
// The column is read, the path is written, and the whole column is encoded
// back, which is why this is not an update of one key in the database. An
// encrypted JSON column is re-encrypted on the way out.
func (h *HasAttributes) FillJsonAttribute(key string, value any) error {
	column, path, found := strings.Cut(key, "->")
	if !found {
		return fmt.Errorf("concerns: %q is not a JSON path; it needs a -> segment", key)
	}

	current, err := h.getArrayAttributeByKey(column)
	if err != nil {
		return err
	}
	arr.Set(current, strings.ReplaceAll(path, "->", "."), value)

	encoded, err := h.AsJson(current)
	if err != nil {
		return err
	}

	if h.IsEncryptedCastable(column) {
		encrypted, err := h.CastAttributeAsEncryptedString(column, encoded)
		if err != nil {
			return err
		}
		h.SetAttribute(column, encrypted)
		return nil
	}

	h.SetAttribute(column, encoded)
	return nil
}

// getArrayAttributeByKey returns the stored JSON column as a map, or an
// empty one when the column is empty.
func (h *HasAttributes) getArrayAttributeByKey(key string) (map[string]any, error) {
	stored := h.GetAttribute(key)
	if stored == nil {
		return map[string]any{}, nil
	}

	if h.IsEncryptedCastable(key) {
		text, ok := stored.(string)
		if !ok {
			text = fmt.Sprint(stored)
		}
		plain, err := h.FromEncryptedString(text)
		if err != nil {
			return nil, err
		}
		stored = plain
	}

	decoded, err := h.FromJson(stored)
	if err != nil {
		return nil, err
	}
	if decoded == nil {
		return map[string]any{}, nil
	}

	bag, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("concerns: %s does not hold a JSON object", key)
	}
	return bag, nil
}
