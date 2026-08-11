package events

// CacheEvent is what every keyed cache event carries: which store, which key,
// and under which tags.
//
// It answers Illuminate\Cache\Events\CacheEvent. In PHP it is an abstract class
// the others extend; here it is a struct the others embed, which is the same
// arrangement with the same fields under the same names -- a listener written
// against CacheHit reads e.Key and e.StoreName exactly as it does in Laravel.
//
// It is not an interface. A listener that wants "any cache event" switches on
// the concrete type, and a listener that wants the common part reads the
// embedded CacheEvent.
type CacheEvent struct {
	// StoreName is the name of the cache store the event came from. It answers
	// $storeName, and it is empty when the repository was built without one --
	// the null Laravel puts there.
	StoreName string

	// Key is the key the event is about. It answers $key.
	Key string

	// Tags are the tags assigned to the key. It answers $tags, and it is nil on
	// an untagged repository.
	Tags []string
}

// SetTags sets the tags for the cache event.
//
// It answers CacheEvent::setTags(). It is on a pointer receiver and returns the
// event so it reads in one line, which is what the PHP's "return $this" is
// there for.
func (e *CacheEvent) SetTags(tags []string) *CacheEvent {
	e.Tags = tags
	return e
}

// RetrievingKey is fired before a key is read.
//
// It answers Illuminate\Cache\Events\RetrievingKey. It is the "about to look"
// event, and it fires whether or not anything is there -- CacheHit or
// CacheMissed follows it.
type RetrievingKey struct{ CacheEvent }

// NewRetrievingKey returns the event.
func NewRetrievingKey(storeName, key string, tags []string) *RetrievingKey {
	return &RetrievingKey{CacheEvent{StoreName: storeName, Key: key, Tags: tags}}
}

// RetrievingManyKeys is fired before several keys are read at once.
//
// It answers Illuminate\Cache\Events\RetrievingManyKeys.
type RetrievingManyKeys struct {
	CacheEvent

	// Keys are the keys being retrieved. It answers $keys.
	Keys []string
}

// NewRetrievingManyKeys returns the event.
//
// Key is the first of keys, and the empty string when there are none, which is
// what the PHP's null-coalesced $keys[0] does.
func NewRetrievingManyKeys(storeName string, keys []string, tags []string) *RetrievingManyKeys {
	first := ""
	if len(keys) > 0 {
		first = keys[0]
	}
	return &RetrievingManyKeys{
		CacheEvent: CacheEvent{StoreName: storeName, Key: first, Tags: tags},
		Keys:       keys,
	}
}

// CacheHit is fired when a key was found.
//
// It answers Illuminate\Cache\Events\CacheHit.
type CacheHit struct {
	CacheEvent

	// Value is the value that was retrieved. It answers $value.
	Value any
}

// NewCacheHit returns the event.
func NewCacheHit(storeName, key string, value any, tags []string) *CacheHit {
	return &CacheHit{
		CacheEvent: CacheEvent{StoreName: storeName, Key: key, Tags: tags},
		Value:      value,
	}
}

// CacheMissed is fired when a key was not found.
//
// It answers Illuminate\Cache\Events\CacheMissed. It is the event a cache hit
// rate is computed from, together with CacheHit.
type CacheMissed struct{ CacheEvent }

// NewCacheMissed returns the event.
func NewCacheMissed(storeName, key string, tags []string) *CacheMissed {
	return &CacheMissed{CacheEvent{StoreName: storeName, Key: key, Tags: tags}}
}

// WritingKey is fired before a key is written.
//
// It answers Illuminate\Cache\Events\WritingKey. KeyWritten or KeyWriteFailed
// follows it.
type WritingKey struct {
	CacheEvent

	// Value is the value that will be written. It answers $value.
	Value any

	// Seconds is how long the key should be valid for. It answers $seconds, and
	// it is seconds and not a time.Duration because that is the unit the event
	// carries in Laravel and a listener that formats it should not have to
	// convert.
	Seconds int
}

// NewWritingKey returns the event.
func NewWritingKey(storeName, key string, value any, seconds int, tags []string) *WritingKey {
	return &WritingKey{
		CacheEvent: CacheEvent{StoreName: storeName, Key: key, Tags: tags},
		Value:      value,
		Seconds:    seconds,
	}
}

// KeyWritten is fired after a key was written.
//
// It answers Illuminate\Cache\Events\KeyWritten.
type KeyWritten struct {
	CacheEvent

	// Value is the value that was written. It answers $value.
	Value any

	// Seconds is how long the key is valid for. It answers $seconds.
	Seconds int
}

// NewKeyWritten returns the event.
func NewKeyWritten(storeName, key string, value any, seconds int, tags []string) *KeyWritten {
	return &KeyWritten{
		CacheEvent: CacheEvent{StoreName: storeName, Key: key, Tags: tags},
		Value:      value,
		Seconds:    seconds,
	}
}

// KeyWriteFailed is fired when a write did not happen.
//
// It answers Illuminate\Cache\Events\KeyWriteFailed. This is the event worth
// listening to: a cache that has quietly stopped accepting writes looks exactly
// like a cache with a very low hit rate, and only this tells the two apart.
type KeyWriteFailed struct {
	CacheEvent

	// Value is the value that would have been written. It answers $value.
	Value any

	// Seconds is how long the key should have been valid for. It answers
	// $seconds.
	Seconds int
}

// NewKeyWriteFailed returns the event.
func NewKeyWriteFailed(storeName, key string, value any, seconds int, tags []string) *KeyWriteFailed {
	return &KeyWriteFailed{
		CacheEvent: CacheEvent{StoreName: storeName, Key: key, Tags: tags},
		Value:      value,
		Seconds:    seconds,
	}
}

// WritingManyKeys is fired before several keys are written at once.
//
// It answers Illuminate\Cache\Events\WritingManyKeys.
type WritingManyKeys struct {
	CacheEvent

	// Keys are the keys being written. It answers $keys.
	Keys []string

	// Values are the values being written, in the order of Keys. It answers
	// $values.
	Values []any

	// Seconds is how long the keys should be valid for. It answers $seconds.
	Seconds int
}

// NewWritingManyKeys returns the event.
//
// Key is the first of keys, which is what the PHP's "$keys[0]" does -- except
// that an empty set is the empty string here rather than an undefined index.
func NewWritingManyKeys(storeName string, keys []string, values []any, seconds int, tags []string) *WritingManyKeys {
	first := ""
	if len(keys) > 0 {
		first = keys[0]
	}
	return &WritingManyKeys{
		CacheEvent: CacheEvent{StoreName: storeName, Key: first, Tags: tags},
		Keys:       keys,
		Values:     values,
		Seconds:    seconds,
	}
}

// ForgettingKey is fired before a key is removed.
//
// It answers Illuminate\Cache\Events\ForgettingKey. KeyForgotten or
// KeyForgetFailed follows it.
type ForgettingKey struct{ CacheEvent }

// NewForgettingKey returns the event.
func NewForgettingKey(storeName, key string, tags []string) *ForgettingKey {
	return &ForgettingKey{CacheEvent{StoreName: storeName, Key: key, Tags: tags}}
}

// KeyForgotten is fired after a key was removed.
//
// It answers Illuminate\Cache\Events\KeyForgotten.
type KeyForgotten struct{ CacheEvent }

// NewKeyForgotten returns the event.
func NewKeyForgotten(storeName, key string, tags []string) *KeyForgotten {
	return &KeyForgotten{CacheEvent{StoreName: storeName, Key: key, Tags: tags}}
}

// KeyForgetFailed is fired when a key could not be removed.
//
// It answers Illuminate\Cache\Events\KeyForgetFailed. It is the one that
// matters after a deploy: an invalidation that failed leaves the old value
// being served, and nothing else says so.
type KeyForgetFailed struct{ CacheEvent }

// NewKeyForgetFailed returns the event.
func NewKeyForgetFailed(storeName, key string, tags []string) *KeyForgetFailed {
	return &KeyForgetFailed{CacheEvent{StoreName: storeName, Key: key, Tags: tags}}
}

// CacheFlushing is fired before a store is flushed.
//
// It answers Illuminate\Cache\Events\CacheFlushing. It carries no key, because
// a flush is about all of them.
type CacheFlushing struct {
	// StoreName is the name of the cache store. It answers $storeName.
	StoreName string

	// Tags are the tags being flushed, and nil when the whole namespace is. It
	// answers $tags.
	Tags []string
}

// NewCacheFlushing returns the event.
func NewCacheFlushing(storeName string, tags []string) *CacheFlushing {
	return &CacheFlushing{StoreName: storeName, Tags: tags}
}

// SetTags sets the tags for the cache event. It answers
// CacheFlushing::setTags().
func (e *CacheFlushing) SetTags(tags []string) *CacheFlushing {
	e.Tags = tags
	return e
}

// CacheFlushed is fired after a store was flushed.
//
// It answers Illuminate\Cache\Events\CacheFlushed.
type CacheFlushed struct {
	// StoreName is the name of the cache store. It answers $storeName.
	StoreName string

	// Tags are the tags that were flushed. It answers $tags.
	Tags []string
}

// NewCacheFlushed returns the event.
func NewCacheFlushed(storeName string, tags []string) *CacheFlushed {
	return &CacheFlushed{StoreName: storeName, Tags: tags}
}

// SetTags sets the tags for the cache event. It answers
// CacheFlushed::setTags().
func (e *CacheFlushed) SetTags(tags []string) *CacheFlushed {
	e.Tags = tags
	return e
}

// CacheFlushFailed is fired when a flush did not happen.
//
// It answers Illuminate\Cache\Events\CacheFlushFailed.
type CacheFlushFailed struct {
	// StoreName is the name of the cache store. It answers $storeName.
	StoreName string

	// Tags are the tags that were being flushed. It answers $tags.
	Tags []string
}

// NewCacheFlushFailed returns the event.
func NewCacheFlushFailed(storeName string, tags []string) *CacheFlushFailed {
	return &CacheFlushFailed{StoreName: storeName, Tags: tags}
}

// SetTags sets the tags for the cache event. It answers
// CacheFlushFailed::setTags().
func (e *CacheFlushFailed) SetTags(tags []string) *CacheFlushFailed {
	e.Tags = tags
	return e
}

// CacheLocksFlushing is fired before every lock in a store is released.
//
// It answers Illuminate\Cache\Events\CacheLocksFlushing.
type CacheLocksFlushing struct {
	// StoreName is the name of the cache store. It answers $storeName.
	StoreName string
}

// NewCacheLocksFlushing returns the event.
func NewCacheLocksFlushing(storeName string) *CacheLocksFlushing {
	return &CacheLocksFlushing{StoreName: storeName}
}

// CacheLocksFlushed is fired after every lock in a store was released.
//
// It answers Illuminate\Cache\Events\CacheLocksFlushed.
type CacheLocksFlushed struct {
	// StoreName is the name of the cache store. It answers $storeName.
	StoreName string
}

// NewCacheLocksFlushed returns the event.
func NewCacheLocksFlushed(storeName string) *CacheLocksFlushed {
	return &CacheLocksFlushed{StoreName: storeName}
}

// CacheLocksFlushFailed is fired when the locks could not be released.
//
// It answers Illuminate\Cache\Events\CacheLocksFlushFailed.
type CacheLocksFlushFailed struct {
	// StoreName is the name of the cache store. It answers $storeName.
	StoreName string
}

// NewCacheLocksFlushFailed returns the event.
func NewCacheLocksFlushFailed(storeName string) *CacheLocksFlushFailed {
	return &CacheLocksFlushFailed{StoreName: storeName}
}

// CacheFailedOver is fired when one store of a failover set refused an
// operation and the next one was tried.
//
// It answers Illuminate\Cache\Events\CacheFailedOver. It fires once per store
// per failure, and only when that store was not already failing -- so a cache
// that has been down for an hour produces one event, not one per request.
type CacheFailedOver struct {
	// StoreName is the name of the cache store that failed. It answers
	// $storeName.
	StoreName string

	// Err is what it failed with. It answers $exception.
	Err error
}

// NewCacheFailedOver returns the event.
func NewCacheFailedOver(storeName string, err error) *CacheFailedOver {
	return &CacheFailedOver{StoreName: storeName, Err: err}
}
