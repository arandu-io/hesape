package validation

import (
	"github.com/arandu-io/hesape/str"
	"sync"

	"github.com/arandu-io/hesape/auth"
)

// Factory makes Validators already wired to the translator, the presence
// verifier and the message overrides an application registered once.
//
// It is a value an application builds at boot and hands where it is needed.
// There is no global one, and nothing here resolves an extension by name: an
// extension is a function, and Extend takes it directly.
type Factory struct {
	// translator is the catalogue every Validator this makes reads its messages
	// out of.
	translator Translator

	// grant and verifier are what `unique` and `exists` count through. There is
	// no way to hand a verifier without a Grant: a read is authorized like any
	// other.
	grant    auth.Grant
	verifier PresenceVerifier

	// replacers are the placeholder fillers registered for a rule of the
	// application's own.
	replacers map[string]ReplacerFunc

	// fallbackMessages are the sentences those rules say.
	fallbackMessages map[string]any

	// customMessages and customAttributes are the overrides every Validator this
	// Factory makes starts with. Keeping them here is what makes "the whole
	// application spells this field this way" one line at boot.
	customMessages   map[string]any
	customAttributes map[string]string

	// excludeUnvalidatedArrayKeys drops the keys of an array that no rule
	// declared.
	excludeUnvalidatedArrayKeys bool

	// resolved remembers the sets Resolve compiled again, keyed by the set they
	// came from.
	resolved sync.Map

	// requests is the request types compiled by ForRequest.
	requests requestCache

	// registry is the rules the modules of the application declared.
	registry *Registry

	// locale is the locale a Validator is made for when its request names none.
	locale string

	// resolver is what Resolver registered: another way of building the
	// Validator itself.
	resolver func(data Data, rules *Set, opts []ValidatorOption) *Validator
}

// NewFactory returns a Factory over a translator. The translator may be nil, and
// then every Validator it makes falls back to the compiled rule set's own
// sentences -- see getMessage.
func NewFactory(translator Translator) *Factory {
	return &Factory{
		translator:                  translator,
		replacers:                   map[string]ReplacerFunc{},
		fallbackMessages:            map[string]any{},
		customMessages:              map[string]any{},
		customAttributes:            map[string]string{},
		excludeUnvalidatedArrayKeys: true,
		registry:                    NewRegistry(),
	}
}

// Use installs the rules of the modules the application uses:
//
//	factory.Use(br.Rules())
//
// Each module hands over a [Provider] that declares its rules, the sentences they
// say in each locale and how they fill the placeholders of those sentences. This
// is the one place a module's rules enter validation, and it is called where the
// application is assembled. It returns the Factory, so calls chain.
func (f *Factory) Use(providers ...Provider) *Factory {
	f.registry.Use(providers...)
	return f
}

// Registry returns the registry of the rules the Factory's modules declared.
func (f *Factory) Registry() *Registry { return f.registry }

// SetLocale sets the locale a Validator speaks when its request names none. The
// empty locale, the default, is the translator's own.
func (f *Factory) SetLocale(locale string) *Factory {
	f.locale = locale
	return f
}

// Make returns a Validator over the data and the rules, with everything this
// Factory carries already on it.
//
// The rules are a compiled Set rather than an array of strings, because the
// strings are parsed and checked at boot -- see MustCompile. Per-call overrides
// are the WithCustomMessages and WithCustomAttributes options.
func (f *Factory) Make(data Data, rules *Set, opts ...ValidatorOption) *Validator {
	resolved, err := f.Resolve(rules)
	if err != nil {
		panic(err)
	}
	v := f.resolve(data, resolved, opts)

	// The presence verifier is responsible for checking the unique and exists
	// data for the validator. It is behind an interface so that more than one
	// version of it may be written besides a database.
	if f.verifier != nil && v.presence == nil {
		v.SetPresenceVerifier(f.grant, f.verifier)
	}

	f.addExtensions(v)

	return v
}

// Validate is Make and Validate in one call.
func (f *Factory) Validate(data Data, rules *Set, opts ...ValidatorOption) (Input, error) {
	return f.Make(data, rules, opts...).Validate()
}

// resolve builds the Validator, through whatever Resolver registered.
func (f *Factory) resolve(data Data, rules *Set, opts []ValidatorOption) *Validator {
	if f.resolver != nil {
		return f.resolver(data, rules, opts)
	}

	base := []ValidatorOption{WithTranslator(f.translator), WithRegistryOf(f.registry), WithLocale(f.locale)}
	if !f.excludeUnvalidatedArrayKeys {
		base = append(base, WithUnvalidatedArrayKeys())
	}
	if len(f.customMessages) > 0 {
		base = append(base, WithCustomMessages(f.customMessages))
	}
	if len(f.customAttributes) > 0 {
		base = append(base, WithCustomAttributes(f.customAttributes))
	}

	return Make(data, rules, append(base, opts...)...)
}

// addExtensions puts the per-validator half of what this Factory registered onto
// one Validator. The rules themselves are in the one catalogue every set is
// compiled against -- see Extend -- so what is left is the replacers and the
// fallback sentences.
func (f *Factory) addExtensions(v *Validator) {
	v.AddReplacers(f.replacers)

	v.SetFallbackMessages(f.fallbackMessages)
}

// Extend registers a rule this application adds, and the sentence it says when
// it fails.
//
// Deprecated: see the package-level Extend. Use declares a rule in a Registry.
//
// It registers into the one catalogue MustCompile checks against, so the name is
// real for every rule set compiled after this call -- which is why it belongs at
// start-up, before any set is compiled.
func (f *Factory) Extend(rule string, extension ExtensionFunc, message ...string) {
	Extend(rule, extension, first(message))

	f.rememberFallback(rule, message)
}

// ExtendImplicit registers a rule that runs even when the attribute is blank, the
// way `required` does.
//
// Deprecated: see Extend. RuleDef.Implicit is the same thing.
func (f *Factory) ExtendImplicit(rule string, extension ExtensionFunc, message ...string) {
	ExtendImplicit(rule, extension, first(message))

	f.rememberFallback(rule, message)
}

// ExtendDependent registers a rule whose first parameter names another field.
//
// Deprecated: see Extend. RuleDef.Dependent is the same thing.
func (f *Factory) ExtendDependent(rule string, extension ExtensionFunc, message ...string) {
	ExtendDependent(rule, extension, first(message))

	f.rememberFallback(rule, message)
}

func (f *Factory) rememberFallback(rule string, message []string) {
	if sentence := first(message); sentence != "" {
		f.fallbackMessages[str.Snake(rule, "_")] = sentence
	}
}

// Replacer registers how one rule fills the placeholders of its own message.
func (f *Factory) Replacer(rule string, replacer ReplacerFunc) {
	f.replacers[str.Snake(rule, "_")] = replacer
}

// IncludeUnvalidatedArrayKeys keeps the keys of an array that no rule
// declared.
func (f *Factory) IncludeUnvalidatedArrayKeys() { f.excludeUnvalidatedArrayKeys = false }

// ExcludeUnvalidatedArrayKeys drops the keys of an array that no rule
// declared.
func (f *Factory) ExcludeUnvalidatedArrayKeys() { f.excludeUnvalidatedArrayKeys = true }

// Resolver registers another way of building the Validator itself, which is how
// an application ships one of its own.
func (f *Factory) Resolver(resolver func(data Data, rules *Set, opts []ValidatorOption) *Validator) {
	f.resolver = resolver
}

// GetTranslator returns the catalogue this Factory hands its Validators.
func (f *Factory) GetTranslator() Translator { return f.translator }

// GetPresenceVerifier returns the verifier this Factory hands its Validators.
func (f *Factory) GetPresenceVerifier() PresenceVerifier { return f.verifier }

// SetPresenceVerifier sets the Grant and the verifier this Factory hands its
// Validators.
func (f *Factory) SetPresenceVerifier(g auth.Grant, presenceVerifier PresenceVerifier) {
	f.grant, f.verifier = g, presenceVerifier
}

// SetCustomMessages sets the inline messages every Validator this Factory makes
// starts with.
func (f *Factory) SetCustomMessages(messages map[string]any) *Factory {
	f.customMessages = messages

	return f
}

// SetAttributeNames sets the field names every Validator this Factory makes
// starts with.
func (f *Factory) SetAttributeNames(attributes map[string]string) *Factory {
	f.customAttributes = attributes

	return f
}

// first reads an optional argument, answering the empty string when none was
// given.
func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
