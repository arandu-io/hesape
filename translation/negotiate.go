package translation

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

type ctxLocaleKey struct{}

// WithLocale returns a context carrying the locale of the request.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, ctxLocaleKey{}, locale)
}

// Locale reads the locale [Middleware] negotiated for the request, and returns
// the empty string when there is none.
//
// The empty string is what [Translator.T] reads as "the default locale", so a
// caller passes this straight through without checking it.
func Locale(ctx context.Context) string {
	locale, _ := ctx.Value(ctxLocaleKey{}).(string)
	return locale
}

// Middleware negotiates the locale of every request from its Accept-Language
// header and puts it in the context, where [Locale] reads it.
//
// The header is the only input. A locale in the path, in a query parameter or
// in a cookie is a second way to say the same thing, and each of them is a
// separate cache key for one page.
//
// It answers with Content-Language, and adds Accept-Language to Vary: a page
// whose text depends on a request header and does not say so is a page a shared
// cache will serve in the wrong language.
func Middleware(supported []string, fallback string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := Negotiate(r.Header.Get("Accept-Language"), supported, fallback)
			w.Header().Add("Vary", "Accept-Language")
			w.Header().Set("Content-Language", locale)
			next.ServeHTTP(w, r.WithContext(WithLocale(r.Context(), locale)))
		})
	}
}

// Negotiate picks the locale of an Accept-Language header out of the ones the
// application supports, and returns fallback when none of them fit.
//
// Tags are read in the order of their quality value, highest first, and ties
// keep the order the client wrote. A tag matches a supported locale exactly,
// case insensitively and whichever separator either spells it with; failing
// that it matches by language, so a client asking for pt-BR is served pt and a
// client asking for pt is served the first pt-* on offer. "*" takes the first
// supported locale. A tag at q=0 is refused rather than matched.
//
// The answer is always spelled the way the application spells it, so it indexes
// the catalogue directly.
func Negotiate(header string, supported []string, fallback string) string {
	if len(supported) == 0 {
		return fallback
	}

	for _, tag := range accepted(header) {
		if tag == "*" {
			return supported[0]
		}
		for _, locale := range supported {
			if strings.EqualFold(canonical(tag), canonical(locale)) {
				return locale
			}
		}
		for _, locale := range supported {
			if language(tag) == language(locale) {
				return locale
			}
		}
	}
	return fallback
}

// canonical spells a tag with one separator, so pt_BR and pt-BR compare equal.
func canonical(tag string) string { return strings.ReplaceAll(tag, "_", "-") }

// accepted reads the tags of an Accept-Language header, best first.
func accepted(header string) []string {
	type candidate struct {
		tag     string
		quality float64
	}

	var candidates []candidate
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(part, ";")
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		quality := 1.0
		for _, param := range strings.Split(params, ";") {
			name, value, ok := strings.Cut(param, "=")
			if !ok || strings.ToLower(strings.TrimSpace(name)) != "q" {
				continue
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				quality = 0
				break
			}
			quality = q
		}
		if quality <= 0 {
			continue
		}
		candidates = append(candidates, candidate{tag, quality})
	}

	slices.SortStableFunc(candidates, func(a, b candidate) int {
		return cmp.Compare(b.quality, a.quality)
	})

	tags := make([]string, len(candidates))
	for i, c := range candidates {
		tags[i] = c.tag
	}
	return tags
}
