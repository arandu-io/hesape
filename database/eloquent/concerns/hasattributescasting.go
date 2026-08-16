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

// The other half of Illuminate\Database\Eloquent\Concerns\HasAttributes: what a
// column is declared to be, the accessor and mutator registry, and the four
// encoders a cast reaches on the way in and on the way out.
//
// The cast implementations are not here -- they are eloquent/casts, one place a
// column's meaning is written (RULE 9). What is here is the declaration and the
// lookups the PHP resolves by reflection.

// DefaultDateFormat is the format GetDateFormat falls back to.
//
// The PHP asks the connection's query grammar, which answers "Y-m-d H:i:s" for
// every engine this framework carries. Go writes a layout by example rather
// than by letter, so the same format is spelled with the reference time.
const DefaultDateFormat = "2006-01-02 15:04:05"

// modelEncrypter answers the PHP's static $encrypter property. A static is a
// package variable here, and it is behind a mutex because a model is read from
// more than one goroutine and the PHP's single request is not that.
var modelEncrypter struct {
	sync.RWMutex
	encrypter *encryption.Encrypter
}

// EncryptUsing answers HasAttributes::encryptUsing.
//
// It is a package function because the PHP declares it static and resolves it
// through late static binding, which Go does not have (ADR 0044, mechanical
// change). A nil encrypter clears it, which is the PHP's null.
func EncryptUsing(encrypter *encryption.Encrypter) {
	modelEncrypter.Lock()
	defer modelEncrypter.Unlock()
	modelEncrypter.encrypter = encrypter
}

// CurrentEncrypter answers HasAttributes::currentEncrypter.
//
// The PHP falls back to Crypt::getFacadeRoot(). There is no facade and no
// container here (ADR 0001, ADR 0002), so the fallback is nil and the caller
// gets an error rather than an encrypter conjured from a global registry.
func CurrentEncrypter() *encryption.Encrypter {
	modelEncrypter.RLock()
	defer modelEncrypter.RUnlock()
	return modelEncrypter.encrypter
}

// MergeCasts answers HasAttributes::mergeCasts.
//
// It is also how a model declares its casts in the first place: the PHP
// declares them in a $casts property or a casts() method, and Go has neither a
// property a subclass redeclares nor a method it overrides. Merging onto an
// empty map is that declaration, and there is one way to do it (RULE 9).
func (h *HasAttributes) MergeCasts(declared map[string]any) {
	if h.casts == nil {
		h.casts = map[string]any{}
	}
	for key, cast := range declared {
		h.casts[key] = cast
	}
}

// GetCasts answers HasAttributes::getCasts.
//
// The PHP prepends the primary key's own cast when the model is incrementing,
// which it reads from getKeyName and getKeyType. HasAttributes has neither
// here -- the key is a field on the entity, with a Go type of its own -- so
// what comes back is what was declared.
func (h *HasAttributes) GetCasts() map[string]any {
	if h.casts == nil {
		h.casts = map[string]any{}
	}
	return h.casts
}

// Casts answers HasAttributes::casts, the method a model overrides to declare
// its casts. It reads the same map GetCasts does.
func (h *HasAttributes) Casts() map[string]any { return h.GetCasts() }

// HasCast answers HasAttributes::hasCast.
//
// With no types it asks whether the column is cast at all; with types it asks
// whether the cast is one of them, which is the PHP's second argument.
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

// GetCastType answers HasAttributes::getCastType: the cast's name, with the
// three parameterised forms folded into one word as the PHP folds them.
//
// For a cast held as a value rather than a name -- the PHP's class-string
// branch -- it answers the Go type, which is what class_exists returns there.
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

// IsDateCastable answers HasAttributes::isDateCastable.
func (h *HasAttributes) IsDateCastable(key string) bool {
	return h.HasCast(key, "date", "datetime", "immutable_date", "immutable_datetime")
}

// IsJsonCastable answers HasAttributes::isJsonCastable. The PHP spells the
// middle word Json.
func (h *HasAttributes) IsJsonCastable(key string) bool {
	return h.HasCast(key,
		"array", "json", "json:unicode", "object", "collection",
		"encrypted:array", "encrypted:collection", "encrypted:json", "encrypted:object",
	)
}

// IsEncryptedCastable answers HasAttributes::isEncryptedCastable.
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
// It is the half of the PHP that is discovery rather than behaviour: there, a
// method whose return type is Attribute is that attribute's accessor and
// mutator, found with ReflectionMethod. Go cannot read a method's return type
// by name, so the model writes the same fact down (ADR 0056, motive 1), and
// every has*Mutator lookup below reads this registry. A nil attribute removes
// the registration.
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

// GetAttributeMutator answers the Attribute registered for a key, or nil. It is
// what the PHP reaches by calling the method it found by reflection.
func (h *HasAttributes) GetAttributeMutator(key string) *casts.Attribute {
	return h.mutators[key]
}

// HasAttributeMutator answers HasAttributes::hasAttributeMutator.
func (h *HasAttributes) HasAttributeMutator(key string) bool {
	return h.mutators[key] != nil
}

// HasAttributeGetMutator answers HasAttributes::hasAttributeGetMutator.
func (h *HasAttributes) HasAttributeGetMutator(key string) bool {
	return h.mutators[key].HasGet()
}

// HasAttributeSetMutator answers HasAttributes::hasAttributeSetMutator.
func (h *HasAttributes) HasAttributeSetMutator(key string) bool {
	return h.mutators[key].HasSet()
}

// HasGetMutator answers HasAttributes::hasGetMutator.
//
// The PHP has two ways to declare an accessor -- a getFooAttribute method and
// an Attribute return type -- and one method for each. Go has one registry, so
// both names read it: the question a Laravel developer asks under either name
// has the same answer here, and there is still only one way to declare the
// accessor (RULE 9).
func (h *HasAttributes) HasGetMutator(key string) bool { return h.HasAttributeGetMutator(key) }

// HasSetMutator answers HasAttributes::hasSetMutator, reading the same registry
// as HasAttributeSetMutator and for the same reason.
func (h *HasAttributes) HasSetMutator(key string) bool { return h.HasAttributeSetMutator(key) }

// HasAnyGetMutator answers HasAttributes::hasAnyGetMutator.
func (h *HasAttributes) HasAnyGetMutator(key string) bool {
	return h.HasGetMutator(key) || h.HasAttributeGetMutator(key)
}

// MutateAttribute answers HasAttributes::mutateAttribute: the stored value read
// through its accessor.
//
// It returns an error where the PHP lets the accessor throw, and it returns the
// value untouched when there is no accessor -- which is what the PHP's callers
// arrange by asking hasAnyGetMutator first.
func (h *HasAttributes) MutateAttribute(key string, value any) (any, error) {
	mutator := h.mutators[key]
	if !mutator.HasGet() {
		return value, nil
	}
	return mutator.Get(value, h.GetAttributes())
}

// SetMutatedAttributeValue answers HasAttributes::setMutatedAttributeValue: the
// columns the mutator writes, applied to the attribute bag.
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

// GetMutatedAttributes answers HasAttributes::getMutatedAttributes: the keys
// that have an accessor, computed once and kept.
func (h *HasAttributes) GetMutatedAttributes() []string {
	if !h.mutatedCached {
		h.CacheMutatedAttributes()
	}
	return h.mutated
}

// CacheMutatedAttributes answers HasAttributes::cacheMutatedAttributes.
//
// The PHP is static and takes a class or an instance, because what it caches is
// the result of reflecting over that class's methods, shared by every instance
// of it. Here the registry is per model and there is nothing to reflect over,
// so it is a method and it recomputes this model's list -- which is the same
// job with the reflection taken out (ADR 0056, motive 1).
//
// The list comes back sorted. The PHP's order is the order PHP happened to
// report the methods in, which is not an order anybody can rely on.
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

// GetDateFormat answers HasAttributes::getDateFormat.
func (h *HasAttributes) GetDateFormat() string {
	if h.dateFormat == "" {
		return DefaultDateFormat
	}
	return h.dateFormat
}

// SetDateFormat answers HasAttributes::setDateFormat.
//
// The format is a Go layout -- the reference time, "2006-01-02" -- and not the
// PHP's letters. The framework speaks one date format language and it is the
// standard library's.
func (h *HasAttributes) SetDateFormat(format string) { h.dateFormat = format }

// FromDateTime answers HasAttributes::fromDateTime: a value as the string the
// column stores.
//
// An empty value comes back empty, as the PHP's `empty($value) ? $value` does.
// It returns an error where the PHP lets Carbon throw on a value it cannot
// read (ADR 0044, mechanical change).
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

// asDateTime answers HasAttributes::asDateTime: whatever a column or a caller
// carries, as a time.
//
// The PHP accepts a Carbon, a DateTimeInterface, a unix timestamp, a Y-m-d
// string and its own storage format. The same list is read here, in the same
// order, minus the two Carbon branches -- there is no Carbon, and time.Time
// covers what they add.
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

// parseDateTime reads the formats a column comes back in. The order is the
// PHP's: the storage format first, then a bare date, then RFC 3339 -- which is
// what a JSON column holds and what serializeDate writes.
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

// FromJson answers HasAttributes::fromJson.
//
// The PHP's $asObject argument chooses between an array and a stdClass. Go has
// neither: a JSON object decodes to map[string]any either way, so the argument
// has nothing to select and is gone. A null or empty column decodes to nil,
// which is the PHP's early return.
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

// AsJson answers HasAttributes::asJson: a value as the string a JSON column
// stores.
//
// The PHP's $flags argument passes json_encode's flags through. encoding/json
// has no counterpart worth passing, and pretty printing a column would change
// what is stored, so there is nothing to select.
func (h *HasAttributes) AsJson(value any) (string, error) {
	encoded, err := casts.Encode(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// FromFloat answers HasAttributes::fromFloat.
//
// The three names the PHP matches on are the three a float column can come
// back as text for, and they are matched here in the same order before the
// value is parsed as a number.
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

// FromEncryptedString answers HasAttributes::fromEncryptedString.
//
// It returns an error where the PHP throws DecryptException, and where the PHP
// falls back to the Crypt facade it reports that no encrypter was set: a value
// that cannot be decrypted must not come back looking like plaintext.
func (h *HasAttributes) FromEncryptedString(value string) (string, error) {
	encrypter := CurrentEncrypter()
	if encrypter == nil {
		return "", fmt.Errorf("concerns: no encrypter is set; call EncryptUsing first")
	}
	return encrypter.DecryptString(value)
}

// CastAttributeAsEncryptedString answers
// HasAttributes::castAttributeAsEncryptedString.
func (h *HasAttributes) CastAttributeAsEncryptedString(key string, value string) (string, error) {
	encrypter := CurrentEncrypter()
	if encrypter == nil {
		return "", fmt.Errorf("concerns: no encrypter is set; call EncryptUsing first")
	}
	return encrypter.EncryptString(value)
}

// FillJsonAttribute answers HasAttributes::fillJsonAttribute: a write into one
// path of a JSON column, "options->notifications->email".
//
// The column is read, the path is written, and the whole column is encoded
// back -- which is the PHP's order and the reason this is not an update of one
// key in the database. An encrypted JSON column is re-encrypted on the way out,
// as the PHP does.
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

// getArrayAttributeByKey answers HasAttributes::getArrayAttributeByKey: the
// stored JSON column as a map, or an empty one when the column is empty.
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
