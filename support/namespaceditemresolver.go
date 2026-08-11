package support

import (
	"strings"
	"sync"
)

// NamespacedItemResolver answers to Illuminate\Support\NamespacedItemResolver:
// the reader that turns "package::group.item" into its three parts, which is
// how a configuration key and a translation key are both read.
type NamespacedItemResolver struct {
	mu     sync.Mutex
	parsed map[string]parsedKey
}

// parsedKey is the [namespace, group, item] array the PHP returns.
type parsedKey struct {
	namespace string
	group     string
	item      string
}

// NewNamespacedItemResolver is the `new NamespacedItemResolver` the PHP writes,
// which has no constructor of its own.
func NewNamespacedItemResolver() *NamespacedItemResolver {
	return &NamespacedItemResolver{parsed: map[string]parsedKey{}}
}

// ParseKey answers to NamespacedItemResolver::parseKey: the namespace, the
// group and the item of a key, in that order.
//
// The PHP returns null for a key with no namespace and for a group asked for
// whole; the empty string stands for that null here. Every key parsed is kept,
// as the PHP $parsed cache keeps it.
func (r *NamespacedItemResolver) ParseKey(key string) (namespace, group, item string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.parsed == nil {
		r.parsed = map[string]parsedKey{}
	}
	if held, ok := r.parsed[key]; ok {
		return held.namespace, held.group, held.item
	}

	var parsed parsedKey
	if !strings.Contains(key, "::") {
		parsed = parseBasicSegments(strings.Split(key, "."))
	} else {
		parsed = parseNamespacedSegments(key)
	}

	r.parsed[key] = parsed
	return parsed.namespace, parsed.group, parsed.item
}

// parseBasicSegments answers to the protected
// NamespacedItemResolver::parseBasicSegments.
func parseBasicSegments(segments []string) parsedKey {
	parsed := parsedKey{group: segments[0]}
	if len(segments) > 1 {
		parsed.item = strings.Join(segments[1:], ".")
	}
	return parsed
}

// parseNamespacedSegments answers to the protected
// NamespacedItemResolver::parseNamespacedSegments.
func parseNamespacedSegments(key string) parsedKey {
	namespace, item, _ := strings.Cut(key, "::")
	parsed := parseBasicSegments(strings.Split(item, "."))
	parsed.namespace = namespace
	return parsed
}

// SetParsedKey answers to NamespacedItemResolver::setParsedKey. The PHP takes
// the [namespace, group, item] array; Go takes the three parts, because they
// are three strings and not one list.
func (r *NamespacedItemResolver) SetParsedKey(key, namespace, group, item string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.parsed == nil {
		r.parsed = map[string]parsedKey{}
	}
	r.parsed[key] = parsedKey{namespace: namespace, group: group, item: item}
}

// FlushParsedKeys answers to NamespacedItemResolver::flushParsedKeys.
func (r *NamespacedItemResolver) FlushParsedKeys() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parsed = map[string]parsedKey{}
}
