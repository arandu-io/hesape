package validation

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/arandu-io/hesape/str"
)

// This file is how a module adds rules to validation, and how an application
// installs them.
//
// A module that has rules of its own -- the Brazilian documents, a tax number, a
// product code -- returns a [Provider]. The application hands it to the [Factory]
// where it wires the rest, in bootstrap/app.go, and nothing happens before that:
//
//	factory := validation.NewFactory(validation.FromTranslation(tr))
//	factory.Use(br.Rules())
//
// A rule is declared whole: the check, whether it runs on a blank value, how many
// parameters it takes, the sentence it says in each locale, and the function that
// fills the placeholders of that sentence. A rule that has to be registered in
// three places is a rule somebody forgets to register in one.
//
// # Why this is not an init function
//
// Extend, ExtendImplicit and ExtendDependent register into a table that every
// rule set in the process is compiled against. The only place that table can be
// filled before a package-level rule set is compiled is an init function of a
// package that set imports, and so a module that wants its rules known had to
// register them as a side effect of being imported. That is hidden wiring: the
// rules exist because of an import nobody wrote on purpose, in an order the
// compiler picks, in a table nothing can ask which module filled. A [Registry]
// is a value, installed where the application is assembled, visible in the one
// file that wires it, and held by the Factory that uses it. New code does not
// register in init; Extend stays so that code which does keeps working.

// Provider is implemented by a module that adds rules to validation. The
// application calls Use on its Factory, which hands the Provider the Registry to
// declare them in.
type Provider interface {
	// RegisterValidation declares the module's rules in the registry.
	RegisterValidation(registry *Registry)
}

// Params bounds how many parameters a rule takes, the ones written after its
// colon. A rule whose Params are the zero value takes none, and a rule string
// that gives it one does not compile.
type Params struct {
	// Min is how many parameters the rule needs.
	Min int
	// Max is how many it accepts at most. A negative Max accepts any number.
	Max int
}

// AnyParams is the Params of a rule that takes any number of parameters.
var AnyParams = Params{Max: -1}

// RuleDef declares one rule a module adds.
type RuleDef struct {
	// Name is the rule as it is written in a rule string. It is lower case, and
	// it may not be one the catalogue has, one this package refuses, or one
	// already declared.
	Name string

	// Aliases are other names for the same rule.
	Aliases []string

	// Check reports whether the value passes. It is called with the validator,
	// the attribute, the value and the parameters written after the colon.
	Check ExtensionFunc

	// Implicit makes the rule run when the value is blank, as required does.
	Implicit bool

	// Dependent says that the first parameter names another field of the same
	// set, which is then checked at boot like every cross-field reference.
	Dependent bool

	// Params bounds the parameters. The zero value is none.
	Params Params

	// Args checks the parameters when a rule set is compiled, so that a malformed
	// one is a boot failure and not a failure on the first request. It may be nil.
	Args func(parameters []string) error

	// Messages is the sentence the rule says, by locale: "pt-BR" and "en". The
	// sentence holds :attribute and the placeholders Replace fills. The
	// application's own language file overrides a line by defining validation.<name>.
	Messages map[string]string

	// Replace fills the placeholders of the sentence that are particular to the
	// rule. It is called after :attribute has been filled. It may be nil.
	Replace ReplacerFunc
}

// Registry holds the rules the modules of an application declared. A Factory has
// one, and Use is how a module fills it.
//
// It is safe for concurrent use. It is written at boot and read by every request.
type Registry struct {
	mu        sync.RWMutex
	specs     map[string]*spec
	providers []string
	replacers map[string]ReplacerFunc
	fallback  string
}

// NewRegistry returns an empty Registry. Its fallback locale is "en".
func NewRegistry() *Registry {
	return &Registry{
		specs:     map[string]*spec{},
		replacers: map[string]ReplacerFunc{},
		fallback:  "en",
	}
}

// Use installs the rules of the providers. It returns the registry, so calls
// chain.
func (r *Registry) Use(providers ...Provider) *Registry {
	for _, provider := range providers {
		provider.RegisterValidation(r)
		r.mu.Lock()
		r.providers = append(r.providers, fmt.Sprintf("%T", provider))
		r.mu.Unlock()
	}
	return r
}

// Fallback sets the locale a sentence is read in when it has none for the locale
// of the request. The default is "en".
func (r *Registry) Fallback(locale string) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = locale
	return r
}

// Add declares a rule. It panics when the declaration cannot be used -- no name,
// no check, a name already taken -- because the program is wrong and the place to
// find out is the boot that declared it.
func (r *Registry) Add(def RuleDef) *Registry {
	names := append([]string{def.Name}, def.Aliases...)
	if def.Check == nil {
		panic(fmt.Sprintf("validation: rule %q was declared with no check", def.Name))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, name := range names {
		if name == "" || name != strings.ToLower(name) || strings.ContainsAny(name, "|: ") {
			panic(fmt.Sprintf("validation: %q is not a name a rule can have: it is lower case, with no pipe, colon or space", name))
		}
		if _, taken := specs[name]; taken {
			panic(fmt.Sprintf("validation: rule %q is in the catalogue, and a module cannot answer for it", name))
		}
		if _, deliberate := refused[name]; deliberate {
			panic(fmt.Sprintf("validation: rule %q is refused on purpose -- see refused.go", name))
		}
		if _, taken := r.specs[name]; taken {
			panic(fmt.Sprintf("validation: rule %q is already declared; two answers to one rule name is what boot refuses", name))
		}
	}

	lines := maps.Clone(def.Messages)
	for _, name := range names {
		built := &spec{
			minArgs:  def.Params.Min,
			maxArgs:  def.Params.Max,
			implicit: def.Implicit,
			eval:     evaluator(def.Check),
			lines:    lines,
		}
		if def.Dependent {
			built.refs = []int{0}
		}
		if def.Args != nil {
			args := def.Args
			built.check = func(c *checkCtx) error { return args(c.r.args) }
		}
		built.message = func(*field, *rule) string { return defaultLine(lines) }
		r.specs[name] = built
		if def.Replace != nil {
			r.replacers[name] = def.Replace
		}
	}
	return r
}

// Replacer registers how a rule fills the placeholders of its own sentence,
// whether the rule is a module's or one of the catalogue's. A rule declared with
// Replace already has one.
func (r *Registry) Replacer(rule string, replacer ReplacerFunc) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replacers[str.Snake(rule, "_")] = replacer
	return r
}

// Names lists the rules the registry declares, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Sorted(maps.Keys(r.specs))
}

// Has reports whether the registry declares a rule by this name.
func (r *Registry) Has(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.spec(name)
	return ok
}

// spec returns the declaration of a rule.
func (r *Registry) spec(name string) (*spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sp, ok := r.specs[name]
	return sp, ok
}

// replacer returns how a rule fills its placeholders.
func (r *Registry) replacer(rule string) (ReplacerFunc, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	fn, ok := r.replacers[rule]
	return fn, ok
}

// line returns the sentence a module rule says in a locale: the locale itself,
// its language, the registry's fallback, and then any the rule has, in the order
// of their names.
func (r *Registry) line(sp *spec, locale string) (string, bool) {
	if sp == nil || len(sp.lines) == 0 {
		return "", false
	}
	fallback := "en"
	if r != nil {
		r.mu.RLock()
		fallback = r.fallback
		r.mu.RUnlock()
	}
	return pickLine(sp.lines, locale, fallback)
}

// pickLine chooses among the sentences of a rule: the locale asked for, its
// language ("pt" for "pt-BR"), the fallback locale.
func pickLine(lines map[string]string, locale, fallback string) (string, bool) {
	for _, candidate := range []string{locale, languageOf(locale), fallback, languageOf(fallback)} {
		if candidate == "" {
			continue
		}
		if sentence, ok := lines[candidate]; ok && sentence != "" {
			return sentence, true
		}
		// A locale is written pt-BR and pt_BR by different tools.
		if sentence, ok := lines[strings.ReplaceAll(candidate, "_", "-")]; ok && sentence != "" {
			return sentence, true
		}
	}
	return "", false
}

func languageOf(locale string) string {
	language, _, _ := strings.Cut(strings.ReplaceAll(locale, "_", "-"), "-")
	return language
}

// defaultLine is the sentence a rule says when nothing names a locale: English,
// and then whichever there is.
func defaultLine(lines map[string]string) string {
	if sentence, ok := lines["en"]; ok {
		return sentence
	}
	for _, locale := range slices.Sorted(maps.Keys(lines)) {
		return lines[locale]
	}
	return "is not valid"
}
