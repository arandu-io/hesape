package support

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/support/deferpkg"
)

// The helpers of helpers.php whose PHP name carries an underscore keep it. The
// only change ADR 0044 allows is the initial capital that exports the name, so
// throw_if is Throw_if: a person searching for what they type in Laravel finds
// it, which is the whole point of the rule.

// exit is os.Exit behind a name a test can replace, so that Dd and
// ValidatedInput.Dd can be exercised without taking the test process down.
var exit = os.Exit

// Append_config answers to the append_config() helper: numeric keys pushed past
// 9999 so a merge appends them instead of writing over what is there.
//
// A PHP array mixes numeric and string keys; a Go map is keyed by string, so a
// numeric key is one whose text is a number.
func Append_config(array map[string]any) map[string]any {
	start := 9999
	out := make(map[string]any, len(array))
	numeric := make([]string, 0, len(array))
	for key, held := range array {
		if _, err := strconv.Atoi(key); err == nil {
			numeric = append(numeric, key)
			continue
		}
		out[key] = held
	}
	sort.Strings(numeric)
	for _, key := range numeric {
		start++
		out[strconv.Itoa(start)] = array[key]
	}
	return out
}

// Blank answers to the blank() helper: nothing there at all. A string of spaces
// is blank, a number and a bool never are, and an empty list or map is.
func Blank(v any) bool {
	switch typed := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return false
	case fmtStringer:
		return strings.TrimSpace(typed.String()) == ""
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return true
		}
		return Blank(rv.Elem().Interface())
	case reflect.Slice, reflect.Map, reflect.Array, reflect.Chan:
		return rv.Len() == 0
	case reflect.Func:
		return rv.IsNil()
	default:
		return false
	}
}

// Filled answers to the filled() helper.
func Filled(v any) bool { return !Blank(v) }

// Class_basename answers to the class_basename() helper: the class name with
// its namespace off. In Go it is the type name with its package path off, and a
// pointer is read through, the way the PHP reads an object.
func Class_basename(class any) string {
	name, ok := class.(string)
	if !ok {
		rt := reflect.TypeOf(class)
		if rt == nil {
			return ""
		}
		for rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		name = rt.String()
	}
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndexAny(name, "/."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// htmlEscapes is htmlspecialchars with ENT_QUOTES: the five characters that can
// end an attribute or open a tag, and nothing else.
var htmlEscapes = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#039;",
)

// htmlEscapesKeepingEntities is the same with $doubleEncode false: an ampersand
// that already opens an entity is left alone.
var alreadyAnEntity = regexp.MustCompile(`&(#[0-9]+|#[xX][0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);`)

// E answers to the e() helper: the HTML-special characters of a value written
// as entities, so it cannot escape the attribute it is put in.
//
// An [Htmlable] says its own HTML and is not escaped again, which is what the
// PHP does with Htmlable. The variadic argument stands for the PHP
// $doubleEncode, whose default is true.
func E(v any, doubleEncode ...bool) string {
	if htmlable, ok := v.(Htmlable); ok {
		return htmlable.ToHtml()
	}
	raw := toString(v)
	if firstOr(doubleEncode, true) {
		return htmlEscapes.Replace(raw)
	}

	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); {
		if raw[i] == '&' {
			if match := alreadyAnEntity.FindString(raw[i:]); match != "" && strings.HasPrefix(raw[i:], match) {
				b.WriteString(match)
				i += len(match)
				continue
			}
		}
		b.WriteString(htmlEscapes.Replace(raw[i : i+1]))
		i++
	}
	return b.String()
}

// Laravel_cloud answers to the laravel_cloud() helper: whether the application
// is running on Laravel Cloud, which is one environment variable set to "1".
func Laravel_cloud() bool {
	return os.Getenv("LARAVEL_CLOUD") == "1"
}

// Object_get answers to the object_get() helper: a value read out of a struct
// by dotted key. An empty key gives the object itself, and a missing name gives
// the default.
//
// The PHP reads a property; Go reads an exported field of a struct, or a key of
// a map, which is what a PHP property access reaches.
func Object_get(object any, key string, def ...any) any {
	if strings.TrimSpace(key) == "" {
		return object
	}
	current := object
	for _, segment := range strings.Split(key, ".") {
		next := NewOptional(current).Get(segment)
		if next == nil {
			return value(firstOr(def, nil))
		}
		current = next
	}
	return current
}

// Preg_replace_array answers to the preg_replace_array() helper: each match of
// the pattern replaced by the next value of the list, in order.
//
// The pattern is a Go regular expression, not a PCRE one; the PHP delimiters
// are not part of it.
func Preg_replace_array(pattern string, replacements []string, subject string) string {
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return subject
	}
	next := 0
	return expression.ReplaceAllStringFunc(subject, func(match string) string {
		if next >= len(replacements) {
			return ""
		}
		replacement := replacements[next]
		next++
		return replacement
	})
}

// Retry answers to the retry() helper: run the callback again while it fails,
// up to the given number of attempts.
//
// The PHP takes an int or an array of backoff milliseconds for $times, and an
// int or a Closure for $sleepMilliseconds; so does this. The variadic argument
// stands for $sleepMilliseconds and $when, in that order, whose defaults are 0
// and null. The PHP rethrows the last error, so this returns (any, error).
func Retry(times any, callback func(attempt int) (any, error), options ...any) (any, error) {
	var backoff []int
	remaining := 0
	switch t := times.(type) {
	case []int:
		backoff = t
		remaining = len(t) + 1
	default:
		remaining = toInt(times)
	}

	sleepMilliseconds := any(0)
	if len(options) > 0 {
		sleepMilliseconds = options[0]
	}
	var when func(err error) bool
	if len(options) > 1 {
		if test, ok := options[1].(func(err error) bool); ok {
			when = test
		}
	}

	attempts := 0
	for {
		attempts++
		remaining--

		result, err := callback(attempts)
		if err == nil {
			return result, nil
		}
		if remaining < 1 || (when != nil && !when(err)) {
			return nil, err
		}

		if attempts-1 < len(backoff) {
			sleepMilliseconds = backoff[attempts-1]
		}
		milliseconds := sleepMilliseconds
		switch fn := sleepMilliseconds.(type) {
		case func(attempt int, err error) int:
			milliseconds = fn(attempts, err)
		case func() int:
			milliseconds = fn()
		}
		if pause := toInt(milliseconds); pause > 0 {
			_ = Usleep(pause * 1000).Goodnight()
		}
	}
}

// Tap answers to the tap() helper: hand the value to the callback, then hand
// the value back.
//
// The PHP is generic over mixed; this is generic over T. A null callback gives
// back a HigherOrderTapProxy there, which is __call and has no Go counterpart,
// so a nil callback here simply returns the value.
func Tap[T any](v T, callback func(T)) T {
	if callback != nil {
		callback(v)
	}
	return v
}

// Throw_if answers to the throw_if() helper. The PHP throws; Go has no
// exceptions, so this hands the error back when the condition holds and nil
// when it does not.
func Throw_if(condition bool, err error) error {
	if condition {
		return err
	}
	return nil
}

// Throw_unless answers to the throw_unless() helper. See [Throw_if] on the
// returned error.
func Throw_unless(condition bool, err error) error {
	return Throw_if(!condition, err)
}

// Transform answers to the transform() helper: the callback's value when the
// value is filled, and the default when it is blank.
//
// The variadic argument stands for the PHP $default, which is null.
func Transform[T, R any](v T, callback func(T) R, def ...R) R {
	if Filled(any(v)) {
		return callback(v)
	}
	var zero R
	return firstOr(def, zero)
}

// Windows_os answers to the windows_os() helper.
func Windows_os() bool { return runtime.GOOS == "windows" }

// With answers to the with() helper: the value passed through the callback.
//
// The PHP hands back the value untouched when $callback is null; in Go that is
// the value itself, with nothing to call, so the callback is required.
func With[T, R any](v T, callback func(T) R) R { return callback(v) }

// Dump answers to the dump() call the Dumpable trait makes, and to
// Dumpable::dump: write the values out where the process can see them, then
// hand the first one back.
//
// The PHP uses Symfony's VarDumper; the standard library has no such thing, so
// this writes Go's own %#v to standard error, which is where a dump belongs
// when standard output is the response.
func Dump(values ...any) any {
	for _, v := range values {
		fmt.Fprintf(os.Stderr, "%#v\n", v)
	}
	if len(values) == 0 {
		return nil
	}
	return values[0]
}

// Dd answers to Dumpable::dd: dump the values and end the process.
func Dd(values ...any) {
	Dump(values...)
	exit(1)
}

// Application answers to the part of
// Illuminate\Contracts\Foundation\Application that Localizable::withLocale
// touches. The PHP reaches it through the container, which ADR 0001 rejected,
// so the caller hands it in.
type Application interface {
	// GetLocale answers to Application::getLocale.
	GetLocale() string
	// SetLocale answers to Application::setLocale.
	SetLocale(locale string)
}

// WithLocale answers to Localizable::withLocale: run the callback under the
// given locale and put the old one back afterwards, whatever happens.
//
// An empty locale stands for the PHP falsy locale and runs the callback as it
// is. The application is the first argument because the PHP reads it off the
// container.
func WithLocale(app Application, locale string, callback func() any) any {
	if locale == "" || app == nil {
		return callback()
	}
	original := app.GetLocale()
	defer app.SetLocale(original)
	app.SetLocale(locale)
	return callback()
}

// ErrBadMethodCall answers to the BadMethodCallException that
// ForwardsCalls::throwBadMethodCallException raises.
type ErrBadMethodCall struct {
	// Class is the type the call was forwarded to.
	Class string
	// Method is the name that was called on it.
	Method string
}

// Error writes the PHP message: Call to undefined method Class::method().
func (e *ErrBadMethodCall) Error() string {
	return fmt.Sprintf("Call to undefined method %s::%s()", e.Class, e.Method)
}

// ThrowBadMethodCallException answers to the protected static
// ForwardsCalls::throwBadMethodCallException. The PHP throws, so this builds
// the error and hands it back.
func ThrowBadMethodCallException(object any, method string) error {
	return &ErrBadMethodCall{Class: Class_basename(object), Method: method}
}

// ForwardCallTo answers to the protected ForwardsCalls::forwardCallTo: call the
// named method on the object and give back what it returned.
//
// The PHP forwards through __call, which Go has no counterpart for, so the call
// is made through reflection. A method the object does not carry is
// [ErrBadMethodCall], which is the BadMethodCallException the PHP throws.
func ForwardCallTo(object any, method string, parameters ...any) ([]any, error) {
	rv := reflect.ValueOf(object)
	if !rv.IsValid() {
		return nil, ThrowBadMethodCallException(object, method)
	}
	fn := rv.MethodByName(method)
	if !fn.IsValid() {
		return nil, ThrowBadMethodCallException(object, method)
	}

	rt := fn.Type()
	if !rt.IsVariadic() && rt.NumIn() != len(parameters) {
		return nil, ThrowBadMethodCallException(object, method)
	}

	in := make([]reflect.Value, 0, len(parameters))
	for i, parameter := range parameters {
		if parameter == nil {
			at := rt.In(min(i, rt.NumIn()-1))
			in = append(in, reflect.Zero(at))
			continue
		}
		in = append(in, reflect.ValueOf(parameter))
	}

	returned := fn.Call(in)
	out := make([]any, 0, len(returned))
	for _, v := range returned {
		out = append(out, v.Interface())
	}
	return out, nil
}

// ForwardDecoratedCallTo answers to the protected
// ForwardsCalls::forwardDecoratedCallTo: the same call, except that an object
// handing itself back becomes the decorator handing itself back.
func ForwardDecoratedCallTo(decorator, object any, method string, parameters ...any) ([]any, error) {
	returned, err := ForwardCallTo(object, method, parameters...)
	if err != nil {
		return nil, err
	}
	for i, v := range returned {
		if v == object {
			returned[i] = decorator
		}
	}
	return returned, nil
}

var (
	deferredMu         sync.Mutex
	deferredCollection *deferpkg.DeferredCallbackCollection
)

// DeferredCallbackCollection answers to the
// app(DeferredCallbackCollection::class) the defer() helper resolves: the one
// collection every deferred callback of this process lands in.
//
// The PHP reads it off the container, which ADR 0001 rejected; here it is the
// package's own, built once.
func DeferredCallbackCollection() *deferpkg.DeferredCallbackCollection {
	deferredMu.Lock()
	defer deferredMu.Unlock()
	if deferredCollection == nil {
		deferredCollection = deferpkg.NewDeferredCallbackCollection()
	}
	return deferredCollection
}

// Defer answers to the defer() function of functions.php: put the callback off
// until the response has been sent, and hand back the handle it can be called
// off by.
//
// The variadic argument stands for the PHP $name and $always, in that order,
// whose defaults are null and false. The PHP also answers the collection itself
// when $callback is null; that is [DeferredCallbackCollection], because Go
// cannot return two types from one call.
func Defer(callback func(), options ...any) *deferpkg.DeferredCallback {
	name := ""
	if len(options) > 0 {
		name = toString(options[0])
	}
	always := false
	if len(options) > 1 {
		always = toBool(options[1])
	}
	deferred := deferpkg.NewDeferredCallback(callback, name, always)
	DeferredCallbackCollection().OffsetSet(-1, deferred)
	return deferred
}
