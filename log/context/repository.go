package context

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"sync"

	"github.com/arandu-io/hesape/log/context/events"
)

// Dispatcher is the slice of Illuminate\Contracts\Events\Dispatcher this package
// needs, declared on the side that consumes it so that one concrete dispatcher
// can serve every package that fires an event.
type Dispatcher interface {
	// Dispatch fires the event.
	Dispatch(event any)

	// Listen registers a listener that receives every event; Dehydrating and
	// Hydrated wrap it with the type assertion that selects theirs.
	Listen(listener func(event any))
}

// repositoryKey is the context key the repository travels under.
type repositoryKey struct{}

// Repository answers Illuminate\Log\Context\Repository: the context that crosses
// a whole request and lands in every log line it produces.
//
// A Repository is safe for concurrent use.
type Repository struct {
	mu     sync.RWMutex
	events Dispatcher
	data   map[string]any
	hidden map[string]any
}

// handleUnserializeExceptions answers the static property behind
// handleUnserializeExceptionsUsing. It is static in PHP, so it is package level
// here, with a lock because Go says so and PHP does not have to.
var (
	handleUnserializeMu         sync.RWMutex
	handleUnserializeExceptions func(err error, key string, value any, hidden bool) any
)

// New answers Illuminate\Log\Context\Repository::__construct.
//
// The dispatcher may be nil: a repository without one still holds context, and
// Dehydrating, Hydrated, Dehydrate and Hydrate simply have nobody to tell.
func New(dispatcher Dispatcher) *Repository {
	return &Repository{
		events: dispatcher,
		data:   map[string]any{},
		hidden: map[string]any{},
	}
}

// Into stores the repository in ctx and returns the new context.
//
// It has no counterpart in Illuminate, where the repository is one object per
// process resolved through the container and reached through the Context facade.
// Both of those are rejected here (ADR 0001 and ADR 0002), and the carrier is the
// context.Context instead -- which is also the difference that makes this safe
// under concurrency, where one shared repository is not.
func Into(ctx context.Context, repository *Repository) context.Context {
	return context.WithValue(ctx, repositoryKey{}, repository)
}

// For returns the repository ctx carries, or nil when it carries none.
//
// It is the read side of Into, and stands where Illuminate writes Context::.
// Every method is safe on a nil receiver, so a caller may use the result without
// checking: reading gives the zero value and writing goes nowhere.
func For(ctx context.Context) *Repository {
	if ctx == nil {
		return nil
	}
	repository, _ := ctx.Value(repositoryKey{}).(*Repository)
	return repository
}

// Has answers Illuminate\Log\Context\Repository::has: whether the key exists.
//
// It is array_key_exists, so a key that was added with a nil value is here --
// even though Get returns the default for it, which is the same split PHP has
// between array_key_exists and the ?? operator.
func (r *Repository) Has(key string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.data[key]
	return ok
}

// Missing answers Illuminate\Log\Context\Repository::missing.
func (r *Repository) Missing(key string) bool {
	return !r.Has(key)
}

// HasHidden answers Illuminate\Log\Context\Repository::hasHidden.
func (r *Repository) HasHidden(key string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.hidden[key]
	return ok
}

// MissingHidden answers Illuminate\Log\Context\Repository::missingHidden.
func (r *Repository) MissingHidden(key string) bool {
	return !r.HasHidden(key)
}

// All answers Illuminate\Log\Context\Repository::all: all the context data.
//
// It is a copy, because a PHP array getter hands back a copy and because a live
// map would be read while another goroutine writes it.
func (r *Repository) All() map[string]any {
	if r == nil {
		return map[string]any{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.data)
}

// AllHidden answers Illuminate\Log\Context\Repository::allHidden.
func (r *Repository) AllHidden() map[string]any {
	if r == nil {
		return map[string]any{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return maps.Clone(r.hidden)
}

// Get answers Illuminate\Log\Context\Repository::get: the key's value, or the
// default.
//
// PHP's `$default = null` is the variadic here. A func() any default is called
// only when it is needed, which is what value() does in PHP. A key holding nil
// takes the default too, because PHP reads it with ?? and ?? does not tell a
// missing key from a null one.
func (r *Repository) Get(key string, def ...any) any {
	if r == nil {
		return defaultValue(def)
	}
	r.mu.RLock()
	found := r.data[key]
	r.mu.RUnlock()

	if found != nil {
		return found
	}
	return defaultValue(def)
}

// GetHidden answers Illuminate\Log\Context\Repository::getHidden.
func (r *Repository) GetHidden(key string, def ...any) any {
	if r == nil {
		return defaultValue(def)
	}
	r.mu.RLock()
	found := r.hidden[key]
	r.mu.RUnlock()

	if found != nil {
		return found
	}
	return defaultValue(def)
}

// Pull answers Illuminate\Log\Context\Repository::pull: the key's value, and
// then the key is forgotten.
func (r *Repository) Pull(key string, def ...any) any {
	found := r.Get(key, def...)
	r.Forget(key)
	return found
}

// PullHidden answers Illuminate\Log\Context\Repository::pullHidden.
func (r *Repository) PullHidden(key string, def ...any) any {
	found := r.GetHidden(key, def...)
	r.ForgetHidden(key)
	return found
}

// Only answers Illuminate\Log\Context\Repository::only: only the values of the
// given keys.
//
// A key that is not there is simply absent from the result, which is what
// array_intersect_key does.
func (r *Repository) Only(keys []string) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return only(r.data, keys)
}

// OnlyHidden answers Illuminate\Log\Context\Repository::onlyHidden.
func (r *Repository) OnlyHidden(keys []string) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return only(r.hidden, keys)
}

// Except answers Illuminate\Log\Context\Repository::except: everything but the
// given keys.
func (r *Repository) Except(keys []string) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return except(r.data, keys)
}

// ExceptHidden answers Illuminate\Log\Context\Repository::exceptHidden.
func (r *Repository) ExceptHidden(keys []string) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return except(r.hidden, keys)
}

// Add answers Illuminate\Log\Context\Repository::add: add a context value.
//
// key is Illuminate's `string|array<string, mixed>`, which is why it is any: a
// string with a value, or a map[string]any on its own, merged the way
// array_merge merges. A string key with no value adds nil, which is PHP's
// `$value = null` default.
func (r *Repository) Add(key any, value ...any) *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	merge(r.data, key, value)
	return r
}

// AddHidden answers Illuminate\Log\Context\Repository::addHidden: the same, into
// the hidden half, which is the half that travels with a queued job but never
// reaches a log line.
func (r *Repository) AddHidden(key any, value ...any) *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	merge(r.hidden, key, value)
	return r
}

// AddIf answers Illuminate\Log\Context\Repository::addIf: add only when the key
// is not there yet.
func (r *Repository) AddIf(key string, value any) *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[key]; !ok {
		r.data[key] = value
	}
	return r
}

// AddHiddenIf answers Illuminate\Log\Context\Repository::addHiddenIf.
func (r *Repository) AddHiddenIf(key string, value any) *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.hidden[key]; !ok {
		r.hidden[key] = value
	}
	return r
}

// Remember answers Illuminate\Log\Context\Repository::remember: add the value
// when the key is missing, and return the value either way.
//
// A func() any value is only called when the key is missing, which is what
// value() does in PHP.
func (r *Repository) Remember(key string, value any) any {
	if r == nil {
		return unwrap(value)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.data[key]; ok {
		return existing
	}
	resolved := unwrap(value)
	r.data[key] = resolved
	return resolved
}

// RememberHidden answers Illuminate\Log\Context\Repository::rememberHidden.
func (r *Repository) RememberHidden(key string, value any) any {
	if r == nil {
		return unwrap(value)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.hidden[key]; ok {
		return existing
	}
	resolved := unwrap(value)
	r.hidden[key] = resolved
	return resolved
}

// Forget answers Illuminate\Log\Context\Repository::forget: drop the keys.
//
// PHP's `string|array<int, string>` is the variadic here, and a key that is not
// there is not an error, because unset is not either.
func (r *Repository) Forget(key ...string) *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range key {
		delete(r.data, k)
	}
	return r
}

// ForgetHidden answers Illuminate\Log\Context\Repository::forgetHidden.
func (r *Repository) ForgetHidden(key ...string) *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, k := range key {
		delete(r.hidden, k)
	}
	return r
}

// Push answers Illuminate\Log\Context\Repository::push: append the values to the
// key's stack.
//
// A key that holds something which is not a list is not stackable, and Illuminate
// throws RuntimeException for it; here that is the error, which is the shape a
// thrown exception takes in this ecosystem, and it matches ErrUnableToPush. A
// key that holds nothing is stackable, and pushing creates the stack.
func (r *Repository) Push(key string, values ...any) (*Repository, error) {
	if r == nil {
		return nil, errUnableToPush(key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r, push(r.data, key, values, errUnableToPush)
}

// PushHidden answers Illuminate\Log\Context\Repository::pushHidden.
func (r *Repository) PushHidden(key string, values ...any) (*Repository, error) {
	if r == nil {
		return nil, errUnableToPushHidden(key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r, push(r.hidden, key, values, errUnableToPushHidden)
}

// Pop answers Illuminate\Log\Context\Repository::pop: take the latest value off
// the key's stack.
//
// A key that is not a stack, and a stack that is empty, are both the
// RuntimeException Illuminate throws, and both are this error, which matches
// ErrUnableToPop.
func (r *Repository) Pop(key string) (any, error) {
	if r == nil {
		return nil, errUnableToPop(key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return pop(r.data, key, errUnableToPop)
}

// PopHidden answers Illuminate\Log\Context\Repository::popHidden.
func (r *Repository) PopHidden(key string) (any, error) {
	if r == nil {
		return nil, errUnableToPopHidden(key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return pop(r.hidden, key, errUnableToPopHidden)
}

// Increment answers Illuminate\Log\Context\Repository::increment: add to a
// counter, starting it at zero.
//
// PHP's `int $amount = 1` is the variadic. A key holding something that is not a
// number restarts from zero, which is what PHP's (int) cast does to it.
func (r *Repository) Increment(key string, amount ...int) *Repository {
	if r == nil {
		return nil
	}
	by := 1
	if len(amount) > 0 {
		by = amount[0]
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[key] = toInt(r.data[key]) + by
	return r
}

// Decrement answers Illuminate\Log\Context\Repository::decrement, which is
// Increment with the amount negated.
func (r *Repository) Decrement(key string, amount ...int) *Repository {
	by := 1
	if len(amount) > 0 {
		by = amount[0]
	}
	return r.Increment(key, -by)
}

// StackContains answers Illuminate\Log\Context\Repository::stackContains:
// whether the value is in the key's stack.
//
// A key that is not a stack is the RuntimeException Illuminate throws, and is
// this error; a key that holds nothing is a stack, and the answer is false.
// value may be a func(any) bool, which is Illuminate's Closure form. PHP's
// `$strict = false` is the variadic: strict compares type and value, loose
// compares two numbers numerically and anything else by its printed form, which
// is as close as Go gets to ==.
func (r *Repository) StackContains(key string, value any, strict ...bool) (bool, error) {
	if r == nil {
		return false, errNotAStack(key)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return stackContains(r.data, key, value, strict)
}

// HiddenStackContains answers
// Illuminate\Log\Context\Repository::hiddenStackContains.
func (r *Repository) HiddenStackContains(key string, value any, strict ...bool) (bool, error) {
	if r == nil {
		return false, errNotAStack(key)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return stackContains(r.hidden, key, value, strict)
}

// Scope answers Illuminate\Log\Context\Repository::scope: run the callback with
// the given values added, and put the context back the way it was afterwards --
// including when the callback fails or panics, which is PHP's finally.
//
// PHP returns whatever the callback returns. A Go method cannot take a type
// parameter, so the callback returns only an error and a caller who wants a
// value closes over it.
func (r *Repository) Scope(callback func() error, data map[string]any, hidden map[string]any) error {
	if r == nil {
		return callback()
	}

	r.mu.Lock()
	dataBefore, hiddenBefore := maps.Clone(r.data), maps.Clone(r.hidden)
	maps.Copy(r.data, data)
	maps.Copy(r.hidden, hidden)
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.data, r.hidden = dataBefore, hiddenBefore
		r.mu.Unlock()
	}()

	return callback()
}

// When answers Illuminate\Support\Traits\Conditionable::when, which the
// repository uses the trait for: run the callback when the condition holds, and
// the fallback when it does not.
//
// condition is a bool, a func(*Repository) bool, or any value read for PHP
// truthiness. PHP's `$default = null` third argument is the variadic.
func (r *Repository) When(condition any, callback func(*Repository), otherwise ...func(*Repository)) *Repository {
	if truthy(condition, r) {
		if callback != nil {
			callback(r)
		}
		return r
	}
	if len(otherwise) > 0 && otherwise[0] != nil {
		otherwise[0](r)
	}
	return r
}

// IsEmpty answers Illuminate\Log\Context\Repository::isEmpty: nothing visible
// and nothing hidden.
func (r *Repository) IsEmpty() bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.data) == 0 && len(r.hidden) == 0
}

// Flush answers Illuminate\Log\Context\Repository::flush: drop everything, both
// halves.
func (r *Repository) Flush() *Repository {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data, r.hidden = map[string]any{}, map[string]any{}
	return r
}

// Dehydrating answers Illuminate\Log\Context\Repository::dehydrating: run the
// callback when the context is about to be written down for a queued job.
//
// The callback receives the repository the event carries, which is the copy made
// for the dehydration, not this one. Without a dispatcher there is nothing to
// listen on and the call does nothing.
func (r *Repository) Dehydrating(callback func(*Repository)) *Repository {
	if r == nil || r.events == nil || callback == nil {
		return r
	}
	r.events.Listen(func(event any) {
		if dehydrating, ok := event.(events.ContextDehydrating); ok {
			if repository, ok := dehydrating.Context.(*Repository); ok {
				callback(repository)
			}
		}
	})
	return r
}

// Hydrated answers Illuminate\Log\Context\Repository::hydrated: run the callback
// once a dehydrated context has been read back.
func (r *Repository) Hydrated(callback func(*Repository)) *Repository {
	if r == nil || r.events == nil || callback == nil {
		return r
	}
	r.events.Listen(func(event any) {
		if hydrated, ok := event.(events.ContextHydrated); ok {
			if repository, ok := hydrated.Context.(*Repository); ok {
				callback(repository)
			}
		}
	})
	return r
}

// HandleUnserializeExceptionsUsing answers
// Illuminate\Log\Context\Repository::handleUnserializeExceptionsUsing: what to
// do with a value Hydrate cannot read back.
//
// It is static in PHP and package level here, so it is set once for the process
// and not per repository -- the same reach the static has. The callback returns
// the value to use in place of the broken one; nil callback restores the default,
// which is to fail.
func (r *Repository) HandleUnserializeExceptionsUsing(callback func(err error, key string, value any, hidden bool) any) *Repository {
	handleUnserializeMu.Lock()
	handleUnserializeExceptions = callback
	handleUnserializeMu.Unlock()
	return r
}

// Dehydrate answers Illuminate\Log\Context\Repository::dehydrate: write the
// context down so it can travel with a queued job.
//
// It dispatches ContextDehydrating with a copy first, so a listener can still
// change what travels, and returns nil when there is nothing to carry -- which
// is PHP's null and the signal that no context needs to be attached.
//
// Illuminate serialises each value with PHP's serialize(). Here each value is
// JSON, because that is the serialisation this ecosystem has and because it is
// the one that survives crossing into a process built from different code.
func (r *Repository) Dehydrate() (map[string]any, error) {
	if r == nil {
		return nil, nil
	}

	instance := New(r.events).Add(r.All()).AddHidden(r.AllHidden())
	if r.events != nil {
		r.events.Dispatch(events.ContextDehydrating{Context: instance})
	}
	if instance.IsEmpty() {
		return nil, nil
	}

	data, err := serialize(instance.All())
	if err != nil {
		return nil, err
	}
	hidden, err := serialize(instance.AllHidden())
	if err != nil {
		return nil, err
	}
	return map[string]any{"data": data, "hidden": hidden}, nil
}

// Hydrate answers Illuminate\Log\Context\Repository::hydrate: read a dehydrated
// context back, replacing whatever this repository held.
//
// It accepts what Dehydrate produced, and nil, which is PHP's null and leaves an
// empty context. A value that will not read back goes to the callback
// HandleUnserializeExceptionsUsing set, and without one it is returned as an
// error, which is where PHP rethrows.
func (r *Repository) Hydrate(dehydrated map[string]any) error {
	if r == nil {
		return nil
	}

	data, err := unserialize(dehydrated["data"], false)
	if err != nil {
		return err
	}
	hidden, err := unserialize(dehydrated["hidden"], true)
	if err != nil {
		return err
	}

	r.Flush().Add(data).AddHidden(hidden)
	if r.events != nil {
		r.events.Dispatch(events.ContextHydrated{Context: r})
	}
	return nil
}

// only is array_intersect_key with array_flip.
func only(source map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if found, ok := source[key]; ok {
			out[key] = found
		}
	}
	return out
}

// except is array_diff_key with array_flip.
func except(source map[string]any, keys []string) map[string]any {
	out := maps.Clone(source)
	if out == nil {
		return map[string]any{}
	}
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

// merge is `is_array($key) ? $key : [$key => $value]`, merged in.
func merge(into map[string]any, key any, value []any) {
	switch typed := key.(type) {
	case map[string]any:
		maps.Copy(into, typed)
	case string:
		if len(value) > 0 {
			into[typed] = value[0]
			return
		}
		into[typed] = nil
	default:
		var single any
		if len(value) > 0 {
			single = value[0]
		}
		into[fmt.Sprint(key)] = single
	}
}

// push is the body Push and PushHidden share, including isStackable.
func push(into map[string]any, key string, values []any, fail func(string) error) error {
	existing, ok := into[key]
	if ok {
		stack, isList := existing.([]any)
		if !isList {
			return fail(key)
		}
		into[key] = append(slices.Clone(stack), values...)
		return nil
	}
	into[key] = append([]any{}, values...)
	return nil
}

// pop is the body Pop and PopHidden share.
func pop(from map[string]any, key string, fail func(string) error) (any, error) {
	stack, ok := from[key].([]any)
	if !ok || len(stack) == 0 {
		return nil, fail(key)
	}
	last := stack[len(stack)-1]
	from[key] = stack[:len(stack)-1]
	return last, nil
}

// stackContains is the body StackContains and HiddenStackContains share.
func stackContains(source map[string]any, key string, value any, strict []bool) (bool, error) {
	found, ok := source[key]
	if !ok {
		return false, nil
	}
	stack, isList := found.([]any)
	if !isList {
		return false, errNotAStack(key)
	}
	if match, isClosure := value.(func(any) bool); isClosure {
		return slices.ContainsFunc(stack, match), nil
	}

	exact := len(strict) > 0 && strict[0]
	for _, item := range stack {
		if exact && reflect.DeepEqual(item, value) {
			return true, nil
		}
		if !exact && looseEqual(item, value) {
			return true, nil
		}
	}
	return false, nil
}

// serialize is PHP's serialize() over every value of an array.
func serialize(source map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(source))
	for key, value := range source {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("log/context: unable to dehydrate key [%s]: %w", key, err)
		}
		out[key] = string(encoded)
	}
	return out, nil
}

// unserialize is PHP's unserialize() over every value, with the callback
// handleUnserializeExceptionsUsing set standing in for the try/catch.
func unserialize(source any, hidden bool) (map[string]any, error) {
	encoded, ok := source.(map[string]any)
	if !ok {
		return map[string]any{}, nil
	}

	handleUnserializeMu.RLock()
	handle := handleUnserializeExceptions
	handleUnserializeMu.RUnlock()

	out := make(map[string]any, len(encoded))
	for key, value := range encoded {
		text, isText := value.(string)
		if !isText {
			out[key] = value
			continue
		}
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			wrapped := fmt.Errorf("log/context: unable to hydrate key [%s]: %w", key, err)
			if handle == nil {
				return nil, wrapped
			}
			out[key] = handle(wrapped, key, value, hidden)
			continue
		}
		out[key] = decoded
	}
	return out, nil
}

// defaultValue is PHP's value($default) over the variadic default.
func defaultValue(def []any) any {
	if len(def) == 0 {
		return nil
	}
	return unwrap(def[0])
}

// unwrap is PHP's value(): a closure is called, anything else is itself.
func unwrap(value any) any {
	if resolve, ok := value.(func() any); ok {
		return resolve()
	}
	return value
}

// truthy reads a condition the way PHP reads one for Conditionable::when.
func truthy(condition any, repository *Repository) bool {
	switch typed := condition.(type) {
	case nil:
		return false
	case bool:
		return typed
	case func(*Repository) bool:
		return typed(repository)
	case func() bool:
		return typed()
	case string:
		return typed != "" && typed != "0"
	case int:
		return typed != 0
	case float64:
		return typed != 0
	}
	return true
}

// toInt is PHP's (int) cast: a number becomes itself, a numeric string becomes
// its number, and anything else becomes zero.
func toInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return int(parsed)
	}
	return 0
}

// looseEqual is in_array without the strict flag: two numbers compare
// numerically whatever their Go types, and everything else compares by the form
// it prints as.
func looseEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	an, aok := toFloat(a)
	bn, bok := toFloat(b)
	if aok && bok {
		return an == bn
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// toFloat reports the numeric value of a number, and whether it was one.
func toFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	}
	return 0, false
}

// The three failures Illuminate throws RuntimeException for. Each error names
// the key the way Illuminate's message does, and wraps the sentinel so that a
// caller can tell the three apart with errors.Is instead of reading the text.
//
// PHP has one class for all three and the message is the only distinction; Go
// has somewhere better to put it.
var (
	// ErrUnableToPush is what Push and PushHidden report for a key that holds
	// something other than a list, which is Illuminate's "Unable to push value
	// onto context stack for key [...]".
	ErrUnableToPush = errors.New("log/context: unable to push value onto context stack")

	// ErrUnableToPop is what Pop and PopHidden report for a key that is not a
	// stack or is an empty one, which is Illuminate's "Unable to pop value from
	// context stack for key [...]".
	ErrUnableToPop = errors.New("log/context: unable to pop value from context stack")

	// ErrNotAStack is what StackContains and HiddenStackContains report for a
	// key that holds something other than a list, which is Illuminate's "Given
	// key [...] is not a stack.".
	ErrNotAStack = errors.New("log/context: key is not a stack")
)

func errUnableToPush(key string) error {
	return fmt.Errorf("log/context: unable to push value onto context stack for key [%s]: %w", key, ErrUnableToPush)
}

func errUnableToPushHidden(key string) error {
	return fmt.Errorf("log/context: unable to push value onto hidden context stack for key [%s]: %w", key, ErrUnableToPush)
}

func errUnableToPop(key string) error {
	return fmt.Errorf("log/context: unable to pop value from context stack for key [%s]: %w", key, ErrUnableToPop)
}

func errUnableToPopHidden(key string) error {
	return fmt.Errorf("log/context: unable to pop value from hidden context stack for key [%s]: %w", key, ErrUnableToPop)
}

func errNotAStack(key string) error {
	return fmt.Errorf("log/context: given key [%s] is not a stack: %w", key, ErrNotAStack)
}
