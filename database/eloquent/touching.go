package eloquent

import (
	"reflect"
	"slices"
	"sync"
)

// ignoreOnTouch holds what PHP keeps in Model::$ignoreOnTouch.
//
// It is a package variable behind a mutex for the reason the strict switches
// are: PHP resolves Model::withoutTouching() through late static binding, which
// Go does not have. The entries are types rather than class names, because a
// type is what a Go program has to name a model with.
var ignoreOnTouch struct {
	mu    sync.RWMutex
	types []reflect.Type
}

// WithoutTouching is Model::withoutTouching: a relation whose owner would have
// had its updated_at bumped is left alone for the length of the callback.
//
// It is a function and not a method because the PHP is static, and it is generic
// because the class it is static on is the model's own type. WithoutTouching[Post]
// is Post::withoutTouching.
func WithoutTouching[T any](callback func() error) error {
	return WithoutTouchingOn([]reflect.Type{reflect.TypeFor[T]()}, callback)
}

// WithoutTouchingOn is Model::withoutTouchingOn.
//
// The PHP takes class names and puts them back however the callback ends, which
// is what the finally block does; here that is a defer. It takes types where the
// PHP takes strings, because a string naming a type is a name Go cannot resolve.
func WithoutTouchingOn(models []reflect.Type, callback func() error) error {
	ignoreOnTouch.mu.Lock()
	ignoreOnTouch.types = append(ignoreOnTouch.types, models...)
	ignoreOnTouch.mu.Unlock()

	defer func() {
		ignoreOnTouch.mu.Lock()
		defer ignoreOnTouch.mu.Unlock()
		ignoreOnTouch.types = slices.DeleteFunc(ignoreOnTouch.types, func(t reflect.Type) bool {
			return slices.Contains(models, t)
		})
	}()

	return callback()
}

// IsIgnoringTouch is Model::isIgnoringTouch.
//
// The PHP takes the class and defaults it to static::class; a method on the
// model is that default, and the argument was only ever a way to ask about
// another class from a static.
//
// A model with no updated_at column, and a model with timestamps switched off,
// answer true without consulting the list, exactly as there. What is not carried
// over is is_subclass_of: Go has no subclass, so a type is ignored when it is
// itself in the list and not otherwise.
func (m *Model[T]) IsIgnoringTouch() bool {
	if m.GetUpdatedAtColumn() == "" || !m.UsesTimestamps() {
		return true
	}
	ignoreOnTouch.mu.RLock()
	defer ignoreOnTouch.mu.RUnlock()
	return slices.Contains(ignoreOnTouch.types, reflect.TypeFor[T]())
}
