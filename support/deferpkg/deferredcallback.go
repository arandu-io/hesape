package deferpkg

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// DeferredCallback answers to
// Illuminate\Support\Defer\DeferredCallback: work put off until the response
// has been sent, named so it can be called off before it runs.
//
// The PHP holds $callback, $name and $always as public properties and has a
// method for two of the three. Go has one namespace for a field and a method,
// so the properties are read through [DeferredCallback.GetName],
// [DeferredCallback.GetAlways] and [DeferredCallback.GetCallback], and the
// PHP's own names stay on the writers.
type DeferredCallback struct {
	callback func()
	name     string
	always   bool
}

// NewDeferredCallback answers to DeferredCallback::__construct. An empty name
// stands for the PHP null and is filled with a random one, which is the
// Str::uuid the PHP falls back to.
func NewDeferredCallback(callback func(), name string, always bool) *DeferredCallback {
	if name == "" {
		name = randomName()
	}
	return &DeferredCallback{callback: callback, name: name, always: always}
}

// randomName is the (string) Str::uuid() of the PHP constructor, narrowed to
// what it is used for: a name no other callback will have.
func randomName() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw[0:4]) + "-" + hex.EncodeToString(raw[4:6]) + "-" +
		hex.EncodeToString(raw[6:8]) + "-" + hex.EncodeToString(raw[8:10]) + "-" +
		hex.EncodeToString(raw[10:16])
}

// Name answers to DeferredCallback::name: the name the callback can be called
// off by.
func (d *DeferredCallback) Name(name string) *DeferredCallback {
	d.name = name
	return d
}

// Always answers to DeferredCallback::always: run even when the request or the
// job failed. The variadic argument stands for the PHP $always, whose default
// is true.
func (d *DeferredCallback) Always(always ...bool) *DeferredCallback {
	d.always = true
	if len(always) > 0 {
		d.always = always[0]
	}
	return d
}

// GetName reads the public $name property, which [DeferredCallback.Name]
// already answers to as a writer.
func (d *DeferredCallback) GetName() string { return d.name }

// GetAlways reads the public $always property, which
// [DeferredCallback.Always] already answers to as a writer.
func (d *DeferredCallback) GetAlways() bool { return d.always }

// GetCallback reads the public $callback property.
func (d *DeferredCallback) GetCallback() func() { return d.callback }

// Invoke answers to DeferredCallback::__invoke: run the callback.
//
// The PHP name is a language interface Go has no counterpart for, so the call
// is spelled out.
func (d *DeferredCallback) Invoke() {
	if d == nil || d.callback == nil {
		return
	}
	d.callback()
}

// DeferredCallbackCollection answers to
// Illuminate\Support\Defer\DeferredCallbackCollection: every callback put off
// during one request, in the order they were deferred.
type DeferredCallbackCollection struct {
	mu        sync.Mutex
	callbacks []*DeferredCallback
}

// NewDeferredCallbackCollection is the `new DeferredCallbackCollection` the PHP
// writes, which has no constructor of its own.
func NewDeferredCallbackCollection() *DeferredCallbackCollection {
	return &DeferredCallbackCollection{}
}

// First answers to DeferredCallbackCollection::first. An empty collection has
// no first callback, so this is nil, where the PHP raises an undefined index.
func (c *DeferredCallbackCollection) First() *DeferredCallback {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.callbacks) == 0 {
		return nil
	}
	return c.callbacks[0]
}

// Invoke answers to DeferredCallbackCollection::invoke: run every callback and
// empty the collection.
func (c *DeferredCallbackCollection) Invoke() { c.InvokeWhen(nil) }

// InvokeWhen answers to DeferredCallbackCollection::invokeWhen. A nil test
// stands for the PHP null, which runs everything.
//
// A callback that panics is caught and dropped, which is the rescue() the PHP
// wraps every call in: one deferred callback must not take the others down.
func (c *DeferredCallbackCollection) InvokeWhen(when func(callback *DeferredCallback) bool) {
	if when == nil {
		when = func(*DeferredCallback) bool { return true }
	}

	c.mu.Lock()
	c.forgetDuplicates()
	pending := c.callbacks
	c.callbacks = nil
	c.mu.Unlock()

	for _, callback := range pending {
		if when(callback) {
			rescue(callback)
		}
	}
}

// rescue is the rescue() helper the PHP calls: run it, and swallow whatever it
// raises.
func rescue(callback *DeferredCallback) {
	defer func() { _ = recover() }()
	callback.Invoke()
}

// Forget answers to DeferredCallbackCollection::forget: drop every callback
// deferred under the given name.
func (c *DeferredCallbackCollection) Forget(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := make([]*DeferredCallback, 0, len(c.callbacks))
	for _, callback := range c.callbacks {
		if callback.name != name {
			kept = append(kept, callback)
		}
	}
	c.callbacks = kept
}

// forgetDuplicates answers to the protected
// DeferredCallbackCollection::forgetDuplicates: of two callbacks under one
// name, the later one stands.
//
// The caller holds the lock.
func (c *DeferredCallbackCollection) forgetDuplicates() {
	seen := map[string]struct{}{}
	kept := make([]*DeferredCallback, 0, len(c.callbacks))
	for i := len(c.callbacks) - 1; i >= 0; i-- {
		callback := c.callbacks[i]
		if _, held := seen[callback.name]; held {
			continue
		}
		seen[callback.name] = struct{}{}
		kept = append(kept, callback)
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	c.callbacks = kept
}

// OffsetSet answers to DeferredCallbackCollection::offsetSet: with a negative
// offset, which stands for the PHP null, the callback is appended, and that is
// what $collection[] = $callback does.
func (c *DeferredCallbackCollection) OffsetSet(offset int, callback *DeferredCallback) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if offset < 0 || offset >= len(c.callbacks) {
		c.callbacks = append(c.callbacks, callback)
		return
	}
	c.callbacks[offset] = callback
}

// OffsetExists answers to DeferredCallbackCollection::offsetExists.
func (c *DeferredCallbackCollection) OffsetExists(offset int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetDuplicates()
	return offset >= 0 && offset < len(c.callbacks)
}

// OffsetGet answers to DeferredCallbackCollection::offsetGet. An offset the
// collection does not hold is nil, where the PHP raises an undefined index.
func (c *DeferredCallbackCollection) OffsetGet(offset int) *DeferredCallback {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetDuplicates()
	if offset < 0 || offset >= len(c.callbacks) {
		return nil
	}
	return c.callbacks[offset]
}

// OffsetUnset answers to DeferredCallbackCollection::offsetUnset.
func (c *DeferredCallbackCollection) OffsetUnset(offset int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetDuplicates()
	if offset < 0 || offset >= len(c.callbacks) {
		return
	}
	c.callbacks = append(c.callbacks[:offset], c.callbacks[offset+1:]...)
}

// Count answers to DeferredCallbackCollection::count.
func (c *DeferredCallbackCollection) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetDuplicates()
	return len(c.callbacks)
}
