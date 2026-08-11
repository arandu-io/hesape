package validation

import (
	"github.com/arandu-io/hesape/str"

	"github.com/arandu-io/hesape/auth"
)

// Factory answers to Illuminate\Validation\Factory: the thing that makes
// Validators already wired to the translator, the presence verifier and the
// message overrides an application registered once.
//
// The PHP's Factory is what the Validator facade calls. There is no facade here
// (ADR 0002), so a Factory is a value an application builds at boot and hands
// where it is needed -- which is the same object, without the global.
//
// What is not here is setContainer and getContainer: they exist to resolve a
// class based extension or replacer out of the service locator, and ADR 0001
// refuses one. An extension is a function, and Extend takes it directly.
type Factory struct {
	// translator answers to $translator.
	translator Translator

	// grant and verifier answer to $verifier, plus the Grant RULE 17 adds: a
	// read is authorized too, so there is no way to hand a verifier without one.
	grant    auth.Grant
	verifier PresenceVerifier

	// replacers answers to $replacers.
	replacers map[string]ReplacerFunc

	// fallbackMessages answers to $fallbackMessages.
	fallbackMessages map[string]any

	// customMessages and customAttributes are the overrides every Validator this
	// Factory makes starts with. The PHP passes them per call; keeping them here
	// as well is what makes "the whole application spells this field this way"
	// one line at boot.
	customMessages   map[string]any
	customAttributes map[string]string

	// excludeUnvalidatedArrayKeys answers to the property of the same name.
	excludeUnvalidatedArrayKeys bool

	// resolver answers to $resolver.
	resolver func(data Data, rules *Set, opts []ValidatorOption) *Validator
}

// NewFactory answers to the Factory constructor. The translator may be nil, and
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
	}
}

// Make answers to Factory::make: a Validator over the data and the rules, with
// everything this Factory carries already on it.
//
// The rules are a compiled Set rather than an array of strings, because the
// strings are parsed and checked at boot -- see MustCompile. The $messages and
// $attributes arguments of the PHP are the WithCustomMessages and
// WithCustomAttributes options.
func (f *Factory) Make(data Data, rules *Set, opts ...ValidatorOption) *Validator {
	v := f.resolve(data, rules, opts)

	// The presence verifier is responsible for checking the unique and exists
	// data for the validator. It is behind an interface so that more than one
	// version of it may be written besides a database.
	if f.verifier != nil && v.presence == nil {
		v.SetPresenceVerifier(f.grant, f.verifier)
	}

	f.addExtensions(v)

	return v
}

// Validate answers to Factory::validate: Make and Validate in one call.
func (f *Factory) Validate(data Data, rules *Set, opts ...ValidatorOption) (Input, error) {
	return f.Make(data, rules, opts...).Validate()
}

// resolve answers to Factory::resolve.
func (f *Factory) resolve(data Data, rules *Set, opts []ValidatorOption) *Validator {
	if f.resolver != nil {
		return f.resolver(data, rules, opts)
	}

	base := []ValidatorOption{WithTranslator(f.translator)}
	if len(f.customMessages) > 0 {
		base = append(base, WithCustomMessages(f.customMessages))
	}
	if len(f.customAttributes) > 0 {
		base = append(base, WithCustomAttributes(f.customAttributes))
	}

	return Make(data, rules, append(base, opts...)...)
}

// addExtensions answers to Factory::addExtensions: the per-validator half of
// what this Factory registered. The rules themselves are in the one catalogue
// every set is compiled against -- see Extend -- so what is left is the
// replacers and the fallback sentences.
func (f *Factory) addExtensions(v *Validator) {
	v.AddReplacers(f.replacers)

	v.SetFallbackMessages(f.fallbackMessages)
}

// Extend answers to Factory::extend: a rule this application adds, and the
// sentence it says when it fails.
//
// It registers into the one catalogue MustCompile checks against, so the name is
// real for every rule set compiled after this call -- which is why it belongs in
// boot, before any set is compiled, exactly where a Laravel provider puts it.
func (f *Factory) Extend(rule string, extension ExtensionFunc, message ...string) {
	Extend(rule, extension, first(message))

	f.rememberFallback(rule, message)
}

// ExtendImplicit answers to Factory::extendImplicit: an extension that runs even
// when the attribute is blank, the way `required` does.
func (f *Factory) ExtendImplicit(rule string, extension ExtensionFunc, message ...string) {
	ExtendImplicit(rule, extension, first(message))

	f.rememberFallback(rule, message)
}

// ExtendDependent answers to Factory::extendDependent: an extension whose first
// parameter names another field.
func (f *Factory) ExtendDependent(rule string, extension ExtensionFunc, message ...string) {
	ExtendDependent(rule, extension, first(message))

	f.rememberFallback(rule, message)
}

func (f *Factory) rememberFallback(rule string, message []string) {
	if sentence := first(message); sentence != "" {
		f.fallbackMessages[str.Snake(rule, "_")] = sentence
	}
}

// Replacer answers to Factory::replacer: how one rule fills the placeholders of
// its own message.
func (f *Factory) Replacer(rule string, replacer ReplacerFunc) {
	f.replacers[str.Snake(rule, "_")] = replacer
}

// IncludeUnvalidatedArrayKeys answers to
// Factory::includeUnvalidatedArrayKeys.
func (f *Factory) IncludeUnvalidatedArrayKeys() { f.excludeUnvalidatedArrayKeys = false }

// ExcludeUnvalidatedArrayKeys answers to
// Factory::excludeUnvalidatedArrayKeys.
func (f *Factory) ExcludeUnvalidatedArrayKeys() { f.excludeUnvalidatedArrayKeys = true }

// Resolver answers to Factory::resolver: build the Validator some other way,
// which is how an application ships its own subclass of it.
func (f *Factory) Resolver(resolver func(data Data, rules *Set, opts []ValidatorOption) *Validator) {
	f.resolver = resolver
}

// GetTranslator answers to Factory::getTranslator.
func (f *Factory) GetTranslator() Translator { return f.translator }

// GetPresenceVerifier answers to Factory::getPresenceVerifier.
func (f *Factory) GetPresenceVerifier() PresenceVerifier { return f.verifier }

// SetPresenceVerifier answers to Factory::setPresenceVerifier, with the Grant
// RULE 17 adds.
func (f *Factory) SetPresenceVerifier(g auth.Grant, presenceVerifier PresenceVerifier) {
	f.grant, f.verifier = g, presenceVerifier
}

// SetCustomMessages sets the overrides every Validator this Factory makes starts
// with. It answers to the $messages argument the PHP takes on every make.
func (f *Factory) SetCustomMessages(messages map[string]any) *Factory {
	f.customMessages = messages

	return f
}

// SetAttributeNames sets the field names every Validator this Factory makes
// starts with. It answers to the $attributes argument the PHP takes on every
// make, and to Validator::setAttributeNames.
func (f *Factory) SetAttributeNames(attributes map[string]string) *Factory {
	f.customAttributes = attributes

	return f
}

// first is how Go spells a PHP argument with a default of null.
func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
