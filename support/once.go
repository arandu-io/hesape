package support

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strconv"
	"sync"
)

// onceRepository answers to Illuminate\Support\Once: the store that keeps the
// value a call site produced, so the second call gives back the first answer.
//
// The exported name Once belongs to the once() helper of helpers.php, because
// that is what a Laravel application types; the class behind it is reached
// through [Instance].
type onceRepository struct {
	mu     sync.Mutex
	values map[string]any
}

var (
	onceMu       sync.Mutex
	onceInstance *onceRepository
	onceEnabled  = true
)

// Instance answers to Once::instance.
func Instance() *onceRepository {
	onceMu.Lock()
	defer onceMu.Unlock()
	if onceInstance == nil {
		onceInstance = &onceRepository{values: map[string]any{}}
	}
	return onceInstance
}

// Value answers to Once::value: the value of the onceable, computed once and
// kept under its hash.
//
// The PHP keys a WeakMap by the object the call was made on, so the values die
// with it. Go has no weak map in the standard library, so the object is part of
// the hash instead and the store is flushed with [Flush].
func (o *onceRepository) Value(onceable *Onceable) any {
	if onceable == nil {
		return nil
	}
	onceMu.Lock()
	enabled := onceEnabled
	onceMu.Unlock()
	if !enabled {
		return onceable.Callable()
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.values == nil {
		o.values = map[string]any{}
	}
	if held, ok := o.values[onceable.Hash]; ok {
		return held
	}
	computed := onceable.Callable()
	o.values[onceable.Hash] = computed
	return computed
}

// Enable answers to Once::enable.
func Enable() {
	onceMu.Lock()
	defer onceMu.Unlock()
	onceEnabled = true
}

// Disable answers to Once::disable.
func Disable() {
	onceMu.Lock()
	defer onceMu.Unlock()
	onceEnabled = false
}

// Flush answers to Once::flush.
func Flush() {
	onceMu.Lock()
	defer onceMu.Unlock()
	onceInstance = nil
}

// Onceable answers to Illuminate\Support\Onceable: the call site, whatever it
// was called on, and the callback whose value is kept.
type Onceable struct {
	// Hash answers to the public $hash property.
	Hash string
	// Object answers to the public $object property.
	Object any
	// Callable answers to the public $callable property.
	Callable func() any
}

// NewOnceable answers to Onceable::__construct.
func NewOnceable(hash string, object any, callable func() any) *Onceable {
	return &Onceable{Hash: hash, Object: object, Callable: callable}
}

// TryFromTrace answers to Onceable::tryFromTrace.
//
// The PHP is handed a debug_backtrace and reads the file, the line, the calling
// class and the variables the closure captured. Go builds its own trace, so the
// PHP $trace argument becomes the number of frames to skip -- 0 is the caller
// of TryFromTrace -- and the hash is file and line, because a Go closure cannot
// be asked what it captured.
func TryFromTrace(skip int, callable func() any) *Onceable {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return nil
	}
	function := ""
	if fn := runtime.FuncForPC(pc); fn != nil {
		function = fn.Name()
	}
	sum := sha256.Sum256([]byte(file + "@" + function + ":" + strconv.Itoa(line)))
	return NewOnceable(hex.EncodeToString(sum[:16]), nil, callable)
}

// Once answers to the once() function of helpers.php: run the callback the
// first time this line of code is reached and give back that value every time
// after.
//
// The PHP is generic over mixed; this is generic over T, so the value comes
// back with the type it went in with. See [TryFromTrace] on what stands for the
// closure's captured variables, which Go cannot read.
func Once[T any](callback func() T) T {
	onceable := TryFromTrace(1, func() any { return callback() })
	if onceable == nil {
		return callback()
	}
	held := Instance().Value(onceable)
	if typed, ok := held.(T); ok {
		return typed
	}
	var zero T
	return zero
}
