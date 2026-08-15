package support

import (
	"encoding/json"
	"sort"
	"strings"
)

// DefaultMessageFormat is what a [MessageBag] wraps its messages in until
// [MessageBag.SetFormat] says otherwise: nothing at all, the message on its own.
//
// A format is a template read on the way out, in [MessageBag.Get],
// [MessageBag.First] and [MessageBag.All]: ":message" is replaced by the
// message and ":key" by the key it is filed under, so a bag set to
// "<li>:message</li>" hands every message back already wrapped. The messages
// held in the bag are never changed, only the copies it returns.
//
// Answers the $format property of Illuminate\Support\MessageBag.
const DefaultMessageFormat = ":message"

// MessageProvider is implemented by a value that carries validation messages
// and can hand them over as a [MessageBag]. Taking the interface instead of the
// bag lets a caller pass whatever produced the errors and leave it to decide
// which messages to expose.
//
// *MessageBag implements it by returning itself, so any type that holds a bag
// satisfies it by delegating one method.
//
// Answers Illuminate\Contracts\Support\MessageProvider.
type MessageProvider interface {
	// GetMessageBag answers to MessageProvider::getMessageBag.
	GetMessageBag() *MessageBag
}

// MessageBag answers to Illuminate\Support\MessageBag: the keyed list of
// messages a validator hands to a view, which is what $errors is.
//
// PHP arrays keep insertion order, so this carries the key order beside the
// map: All, Keys and Unique walk the keys in the order they were first added.
type MessageBag struct {
	messages map[string][]string
	order    []string
	format   string
}

// NewMessageBag answers to MessageBag::__construct. Each value is deduplicated
// the way array_unique does, keeping the first occurrence.
//
// PHP takes an ordered array; a Go map has no order, so the keys of messages
// are sorted to make the bag deterministic. Keys added later with Add keep
// their insertion order.
func NewMessageBag(messages map[string][]string) *MessageBag {
	b := &MessageBag{
		messages: make(map[string][]string, len(messages)),
		format:   DefaultMessageFormat,
	}
	keys := make([]string, 0, len(messages))
	for k := range messages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.messages[k] = unique(messages[k])
		b.order = append(b.order, k)
	}
	return b
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Keys answers to MessageBag::keys.
func (b *MessageBag) Keys() []string {
	out := make([]string, len(b.order))
	copy(out, b.order)
	return out
}

// Add answers to MessageBag::add. A key and message pair already in the bag is
// not added twice.
func (b *MessageBag) Add(key, message string) *MessageBag {
	if !b.isUnique(key, message) {
		return b
	}
	if b.messages == nil {
		b.messages = map[string][]string{}
	}
	if _, ok := b.messages[key]; !ok {
		b.order = append(b.order, key)
	}
	b.messages[key] = append(b.messages[key], message)
	return b
}

// AddIf answers to MessageBag::addIf.
func (b *MessageBag) AddIf(boolean bool, key, message string) *MessageBag {
	if boolean {
		return b.Add(key, message)
	}
	return b
}

func (b *MessageBag) isUnique(key, message string) bool {
	existing, ok := b.messages[key]
	if !ok {
		return true
	}
	for _, m := range existing {
		if m == message {
			return false
		}
	}
	return true
}

// Merge answers to MessageBag::merge. Like array_merge_recursive it appends,
// so a key present on both sides ends up with both lists, duplicates included.
//
// PHP also accepts a MessageProvider; pass provider.GetMessageBag().GetMessages()
// for that case.
func (b *MessageBag) Merge(messages map[string][]string) *MessageBag {
	if b.messages == nil {
		b.messages = map[string][]string{}
	}
	keys := make([]string, 0, len(messages))
	for k := range messages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := b.messages[k]; !ok {
			b.order = append(b.order, k)
		}
		b.messages[k] = append(b.messages[k], messages[k]...)
	}
	return b
}

// Has answers to MessageBag::has. With no key it answers Any, the way the PHP
// answers a null key. With several keys every one of them must have a message.
func (b *MessageBag) Has(keys ...string) bool {
	if b.IsEmpty() {
		return false
	}
	if len(keys) == 0 {
		return b.Any()
	}
	for _, key := range keys {
		if b.First(key) == "" {
			return false
		}
	}
	return true
}

// HasAny answers to MessageBag::hasAny. With no key it is false, the way the
// PHP default of an empty array is.
func (b *MessageBag) HasAny(keys ...string) bool {
	if b.IsEmpty() {
		return false
	}
	for _, key := range keys {
		if b.Has(key) {
			return true
		}
	}
	return false
}

// Missing answers to MessageBag::missing.
func (b *MessageBag) Missing(keys ...string) bool {
	return !b.HasAny(keys...)
}

// First answers to MessageBag::first. An empty key stands for the PHP null and
// takes the first message in the whole bag. The variadic format stands for the
// optional $format argument; only the first is read.
func (b *MessageBag) First(key string, format ...string) string {
	var messages []string
	if key == "" {
		messages = b.All(format...)
	} else {
		messages = b.Get(key, format...)
	}
	if len(messages) == 0 {
		return ""
	}
	return messages[0]
}

// Get answers to MessageBag::get. A key holding a * matches every key of the
// bag the way Str::is matches there: * stands for any run of characters,
// including none, and every other rune is literal.
//
// The PHP returns a nested array for a wildcard key because its arrays are
// untyped; this flattens the matches in key order.
func (b *MessageBag) Get(key string, format ...string) []string {
	f := b.checkFormat(format...)
	if messages, ok := b.messages[key]; ok {
		return b.transform(messages, f, key)
	}
	if strings.Contains(key, "*") {
		return b.getMessagesForWildcardKey(key, f)
	}
	return []string{}
}

func (b *MessageBag) getMessagesForWildcardKey(key, format string) []string {
	out := []string{}
	for _, k := range b.order {
		if wildcardIs(key, k) {
			out = append(out, b.transform(b.messages[k], format, k)...)
		}
	}
	return out
}

// wildcardIs is what Str::is does, kept unexported here so the message bag has
// no dependency of its own: * stands for any run of characters, and every
// other rune is literal.
func wildcardIs(pattern, value string) bool {
	if pattern == value {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return false
	}
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}
	rest := value[len(parts[0]):]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(rest, part)
		if i < 0 {
			return false
		}
		rest = rest[i+len(part):]
	}
	return strings.HasSuffix(rest, parts[len(parts)-1])
}

// All answers to MessageBag::all.
func (b *MessageBag) All(format ...string) []string {
	f := b.checkFormat(format...)
	all := []string{}
	for _, k := range b.order {
		all = append(all, b.transform(b.messages[k], f, k)...)
	}
	return all
}

// Unique answers to MessageBag::unique.
func (b *MessageBag) Unique(format ...string) []string {
	return unique(b.All(format...))
}

// Forget answers to MessageBag::forget.
func (b *MessageBag) Forget(key string) *MessageBag {
	if _, ok := b.messages[key]; !ok {
		return b
	}
	delete(b.messages, key)
	for i, k := range b.order {
		if k == key {
			b.order = append(b.order[:i], b.order[i+1:]...)
			break
		}
	}
	return b
}

func (b *MessageBag) transform(messages []string, format, messageKey string) []string {
	if format == DefaultMessageFormat {
		out := make([]string, len(messages))
		copy(out, messages)
		return out
	}
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		s := strings.ReplaceAll(format, ":message", message)
		out = append(out, strings.ReplaceAll(s, ":key", messageKey))
	}
	return out
}

func (b *MessageBag) checkFormat(format ...string) string {
	if len(format) > 0 && format[0] != "" {
		return format[0]
	}
	return b.GetFormat()
}

// Messages answers to MessageBag::messages.
func (b *MessageBag) Messages() map[string][]string {
	out := make(map[string][]string, len(b.messages))
	for k, v := range b.messages {
		copied := make([]string, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// GetMessages answers to MessageBag::getMessages.
func (b *MessageBag) GetMessages() map[string][]string { return b.Messages() }

// GetMessageBag answers to MessageBag::getMessageBag.
func (b *MessageBag) GetMessageBag() *MessageBag { return b }

// GetFormat answers to MessageBag::getFormat.
func (b *MessageBag) GetFormat() string {
	if b.format == "" {
		return DefaultMessageFormat
	}
	return b.format
}

// SetFormat answers to MessageBag::setFormat. The PHP default of ":message" is
// what an empty format means here.
func (b *MessageBag) SetFormat(format string) *MessageBag {
	if format == "" {
		format = DefaultMessageFormat
	}
	b.format = format
	return b
}

// IsEmpty answers to MessageBag::isEmpty.
func (b *MessageBag) IsEmpty() bool { return !b.Any() }

// IsNotEmpty answers to MessageBag::isNotEmpty.
func (b *MessageBag) IsNotEmpty() bool { return b.Any() }

// Any answers to MessageBag::any.
func (b *MessageBag) Any() bool { return b.Count() > 0 }

// Count answers to MessageBag::count: every message, not every key.
func (b *MessageBag) Count() int {
	n := 0
	for _, v := range b.messages {
		n += len(v)
	}
	return n
}

// ToArray answers to MessageBag::toArray.
func (b *MessageBag) ToArray() map[string][]string { return b.GetMessages() }

// ToJson answers to MessageBag::toJson. The PHP throws on a bad encode, so
// this returns (string, error).
func (b *MessageBag) ToJson() (string, error) {
	raw, err := json.Marshal(b.jsonValue())
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// MarshalJSON answers to MessageBag::jsonSerialize, under the name the Go
// encoder looks for.
func (b *MessageBag) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.jsonValue())
}

func (b *MessageBag) jsonValue() map[string][]string {
	out := b.ToArray()
	if out == nil {
		return map[string][]string{}
	}
	return out
}

// String answers to MessageBag::__toString.
func (b *MessageBag) String() string {
	s, err := b.ToJson()
	if err != nil {
		return "{}"
	}
	return s
}
