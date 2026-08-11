package number

import "sync"

// The default locale and currency Illuminate keeps as Number::$locale and
// Number::$currency, with the same starting values.
var (
	defaultsMu sync.RWMutex
	locale     = "en"
	currency   = "USD"
)

// UseLocale answers for Number::useLocale. It sets the default locale.
//
// The locale is remembered and reported back by DefaultLocale, and it is what
// WithLocale swaps around a callback. It does not change how a number comes out:
// this package renders one set of conventions, the ones ICU calls en-US, and
// says so in the package comment. Setting another locale here does not make the
// output that locale's.
func UseLocale(l string) {
	defaultsMu.Lock()
	locale = l
	defaultsMu.Unlock()
}

// DefaultLocale answers for Number::defaultLocale. It is the locale UseLocale
// last set, and "en" until one is.
func DefaultLocale() string {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	return locale
}

// UseCurrency answers for Number::useCurrency. It sets the default currency,
// which is the one Currency renders an empty code in.
func UseCurrency(c string) {
	defaultsMu.Lock()
	currency = c
	defaultsMu.Unlock()
}

// DefaultCurrency answers for Number::defaultCurrency. It is the currency
// UseCurrency last set, and "USD" until one is.
func DefaultCurrency() string {
	defaultsMu.RLock()
	defer defaultsMu.RUnlock()
	return currency
}

// WithLocale answers for Number::withLocale. It runs the callback with the
// given locale in force and puts the previous one back afterwards, whatever the
// callback does.
//
// Illuminate returns mixed; this is generic over the callback's own result.
func WithLocale[T any](l string, callback func() T) T {
	previous := DefaultLocale()
	UseLocale(l)
	defer UseLocale(previous)
	return callback()
}

// WithCurrency answers for Number::withCurrency. It runs the callback with the
// given currency in force and puts the previous one back afterwards, whatever
// the callback does.
//
// Illuminate returns mixed; this is generic over the callback's own result.
func WithCurrency[T any](c string, callback func() T) T {
	previous := DefaultCurrency()
	UseCurrency(c)
	defer UseCurrency(previous)
	return callback()
}
