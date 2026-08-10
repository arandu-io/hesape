package translation

import (
	"fmt"
	"maps"
	"strings"
)

// Replace holds the arguments a line interpolates, keyed by the placeholder
// name without its colon: Replace{"attribute": "email"} fills ":attribute".
//
// Values are rendered with fmt.Sprint, so a count may be given as an int and a
// duration as a time.Duration without the caller formatting it first.
type Replace map[string]any

// Translator resolves a key into a sentence.
//
// It is immutable once [New] returns and safe for concurrent use. Build one
// where the application is wired and pass it to whatever renders text.
type Translator struct {
	loaders    []Loader
	namespaces map[string]Loader
	locale     string
	fallback   string
	plural     Plural
	missing    func(locale, key string)
}

// Option configures a [Translator] at construction.
type Option func(*Translator)

// WithPlural replaces the table that decides which segment of a line a count
// selects. [DefaultPlural] is used when this is not given.
func WithPlural(p Plural) Option {
	return func(t *Translator) {
		if p != nil {
			t.plural = p
		}
	}
}

// WithNamespace registers a catalogue reached by a prefixed key: the loader
// registered as "shop" answers "shop::orders.title".
//
// A namespace is how a module ships its own lines without writing into the
// application catalogue. Its keys are resolved in that loader only -- there is
// no fall through to the application, because a module that can be shadowed by
// an unrelated key of the same name is a module whose text is not its own.
func WithNamespace(name string, l Loader) Option {
	return func(t *Translator) {
		if name == "" || l == nil {
			return
		}
		if t.namespaces == nil {
			t.namespaces = make(map[string]Loader)
		}
		t.namespaces[name] = l
	}
}

// WithMissingKey registers what to do when no catalogue carries a key. The
// translator still returns the key itself; this is the hook that reports it, to
// a logger in development or to a counter in production.
func WithMissingKey(f func(locale, key string)) Option {
	return func(t *Translator) { t.missing = f }
}

// New builds a translator over an application catalogue.
//
// locale is the locale used when a caller passes none, and fallback is the one
// consulted when a key is missing from the locale asked for. The English lines
// that ship with this package are answered after both, so auth, validation,
// passwords and pagination resolve with no catalogue at all -- l may be nil.
func New(l Loader, locale, fallback string, opts ...Option) *Translator {
	t := &Translator{
		locale:   locale,
		fallback: fallback,
		plural:   DefaultPlural,
	}
	if l != nil {
		t.loaders = append(t.loaders, l)
	}
	t.loaders = append(t.loaders, bundled)
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Locale reports the locale used when a caller passes none.
func (t *Translator) Locale() string { return t.locale }

// Fallback reports the locale consulted when a key is missing from the one
// asked for.
func (t *Translator) Fallback() string { return t.fallback }

// T returns the line stored under key, with its placeholders replaced.
//
// An empty locale means the translator's own. A key no catalogue carries is
// returned unchanged, after the [WithMissingKey] callback has seen it, so a
// wrong key shows as itself on the page instead of as a blank.
func (t *Translator) T(locale, key string, replace Replace) string {
	line, _, ok := t.line(locale, key)
	if !ok {
		t.reportMissing(locale, key)
		line = key
	}
	return interpolate(line, replace)
}

// Choice returns the segment of a line that count selects, with its
// placeholders replaced.
//
// The segments of a line are divided by "|" and may open with an explicit
// condition, "{0}" for a single count and "[1,19]" or "[20,*]" for a range. The
// first condition that matches wins; with no condition, the plural rule of the
// locale chooses.
//
// ":count" is filled with count unless replace already carries it.
func (t *Translator) Choice(locale, key string, count int, replace Replace) string {
	line, at, ok := t.line(locale, key)
	if !ok {
		t.reportMissing(locale, key)
		line, at = key, t.localeOr(locale)
	}
	if _, taken := replace["count"]; !taken {
		replace = maps.Clone(replace)
		if replace == nil {
			replace = make(Replace, 1)
		}
		replace["count"] = count
	}
	return interpolate(choose(line, count, at, t.plural), replace)
}

// Has reports whether a line exists for key, in the given locale or in the
// fallback.
func (t *Translator) Has(locale, key string) bool {
	_, _, ok := t.line(locale, key)
	return ok
}

// line finds a key, and reports which locale answered -- Choice needs it,
// because a line that only exists in the fallback pluralizes by the rule of the
// fallback and not of the locale that was asked for.
func (t *Translator) line(locale, key string) (string, string, bool) {
	namespace, group, item := parseKey(key)
	if item == "" {
		return "", "", false
	}

	loaders := t.loaders
	if namespace != "" {
		l, registered := t.namespaces[namespace]
		if !registered {
			return "", "", false
		}
		loaders = []Loader{l}
	}

	for _, at := range t.localeChain(locale) {
		for _, l := range loaders {
			if line, ok := l.Load(at, group)[item]; ok {
				return line, at, true
			}
		}
	}
	return "", "", false
}

// localeChain is the locale asked for and then the fallback, without repeating
// one that is both.
func (t *Translator) localeChain(locale string) []string {
	locale = t.localeOr(locale)
	if locale == t.fallback || t.fallback == "" {
		return []string{locale}
	}
	return []string{locale, t.fallback}
}

func (t *Translator) localeOr(locale string) string {
	if locale == "" {
		return t.locale
	}
	return locale
}

func (t *Translator) reportMissing(locale, key string) {
	if t.missing != nil {
		t.missing(t.localeOr(locale), key)
	}
}

// parseKey splits "namespace::group.item" into its three parts. The item keeps
// every dot after the first, so "validation.min.string" is the item "min.string"
// of the group "validation".
func parseKey(key string) (namespace, group, item string) {
	if i := strings.Index(key, "::"); i >= 0 {
		namespace, key = key[:i], key[i+2:]
	}
	group, item, _ = strings.Cut(key, ".")
	return namespace, group, item
}

// interpolate replaces every ":name" for which replace carries a name.
//
// A placeholder with no argument is left as it stands. Illuminate blanks it,
// which turns a forgotten argument into a sentence that reads as finished; here
// it stays visible, and the missing colon is the report.
func interpolate(line string, replace Replace) string {
	if len(replace) == 0 || !strings.ContainsRune(line, ':') {
		return line
	}

	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != ':' {
			b.WriteByte(line[i])
			i++
			continue
		}
		name := placeholder(line[i+1:])
		value, ok := replace[name]
		if !ok {
			b.WriteByte(':')
			i++
			continue
		}
		b.WriteString(fmt.Sprint(value))
		i += 1 + len(name)
	}
	return b.String()
}

// placeholder reads the name that follows a colon: letters, digits and
// underscores, which is every placeholder the catalogue uses.
func placeholder(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return s[:i]
	}
	return s
}
