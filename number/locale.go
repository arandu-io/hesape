package number

import "sync"

// The process-wide default locale and currency, and the lock that guards them
// against concurrent readers and writers.
var (
	defaultsMu sync.RWMutex
	locale     = "en"
	currency   = "USD"
)

// UseLocale sets the default locale.
//
// The locale is remembered and reported back by DefaultLocale, and it is what
// WithLocale swaps around a callback. It does not change how a number comes
// out: this package renders one set of conventions, the ones of en-US, and says
// so in the package comment. Setting another locale here does not make the
// output that locale's.
func UseLocale(l string) {
	defaultsMu.Lock()
	locale = l
	defaultsMu.Unlock()
}

// DefaultLocale is the locale UseLocale last set, and "en" until one is.
func DefaultLocale() string {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	return locale
}

// UseCurrency sets the default currency, which is the one Currency renders an
// empty code in.
func UseCurrency(c string) {
	defaultsMu.Lock()
	currency = c
	defaultsMu.Unlock()
}

// DefaultCurrency is the currency UseCurrency last set, and "USD" until one is.
func DefaultCurrency() string {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	return currency
}

// WithLocale runs the callback with the given locale in force and puts the
// previous one back afterwards, whatever the callback does.
//
// The result is generic over the callback's own return type.
func WithLocale[T any](l string, callback func() T) T {
	previous := DefaultLocale()
	UseLocale(l)
	defer UseLocale(previous)
	return callback()
}

// WithCurrency runs the callback with the given currency in force and puts the
// previous one back afterwards, whatever the callback does.
//
// The result is generic over the callback's own return type.
func WithCurrency[T any](c string, callback func() T) T {
	previous := DefaultCurrency()
	UseCurrency(c)
	defer UseCurrency(previous)
	return callback()
}
