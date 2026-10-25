package validation

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"sync"
)

// This file is what a Factory does with the rules of the modules installed on it:
// compile a rule set that names them, resolve a set that was compiled before they
// were installed, check at boot that every rule any set names has an owner, and
// compile the rules a request declares once per type.

// Compile is the package-level Compile against the rules the Factory's modules
// declared as well as the catalogue.
func (f *Factory) Compile(r Rules, opts ...Option) (*Set, error) {
	file, line := callerAt(2)
	return compile(r, file, line, append([]Option{WithRegistry(f.registry)}, opts...))
}

// MustCompile is Compile for a set that must be right, and panics when it is not.
func (f *Factory) MustCompile(r Rules, opts ...Option) *Set {
	file, line := callerAt(2)
	set, err := compile(r, file, line, append([]Option{WithRegistry(f.registry)}, opts...))
	if err != nil {
		panic(err)
	}
	return set
}

// callerAt reports the source file and line of the function skip frames above the
// one that called it.
func callerAt(skip int) (string, int) {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?", 0
	}
	return file, line
}

// Resolve returns the set compiled against the rules the Factory's modules
// declared. A set that names nothing a module declares is returned as it is, and
// one that does is compiled again from the rules it was written with, once, and
// remembered.
//
// The error names every rule the set uses that nothing declares.
func (f *Factory) Resolve(set *Set) (*Set, error) {
	if set == nil || len(set.pending) == 0 {
		return set, nil
	}
	if cached, held := f.resolved.Load(set); held {
		return cached.(*Set), nil
	}
	again, err := compile(set.source, set.file, set.line,
		append(slices.Clone(set.opts), WithRegistry(f.registry), func(s *settings) { s.deferred = false }))
	if err != nil {
		return nil, err
	}
	f.resolved.Store(set, again)
	return again, nil
}

// Check is the boot check of the rules the modules declare: it resolves every set
// and fails, naming the field, the rule and the file of the set, for any rule a
// set uses that nothing declares. Call it where the application is wired, after
// Use, with the sets that are compiled in package-level variables.
//
// Every failure is reported, not the first.
func (f *Factory) Check(sets ...*Set) error {
	var failures []error
	for _, set := range sets {
		if _, err := f.Resolve(set); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// RequestRules is implemented by a request that declares its own rules, which is
// the equivalent of the rules method of a form request. The rules are the ones
// of the type and not of one request: they are read once, from the zero value,
// and compiled once, so Rules must not look at the request.
//
// A request that needs a rule decided by the data writes it with When, Unless or
// ForEach.
type RequestRules interface {
	// Rules returns the rules of each field.
	Rules() Rules
}

// RequestMessages is implemented by a request that words some failures itself.
// The keys are "field.rule", and a key naming a field or a rule the request does
// not declare is a boot failure.
type RequestMessages interface {
	// Messages returns the sentences, by "field.rule".
	Messages() Messages
}

// RequestAttributes is implemented by a request that names its fields for a
// person: {"cpf": "CPF", "birth_date": "data de nascimento"}.
type RequestAttributes interface {
	// Attributes returns how each field is named inside a sentence.
	Attributes() map[string]string
}

// RequestSet is the rules a request type declared, compiled.
type RequestSet struct {
	// Set is the compiled rules.
	Set *Set
	// Attributes is how the request names its fields, or nil.
	Attributes map[string]string
}

// requestCache is what the Factory keeps of the request types it compiled.
type requestCache struct {
	mu    sync.Mutex
	known map[reflect.Type]*compiledRequest
}

type compiledRequest struct {
	once sync.Once
	set  *RequestSet
	err  error
}

// ForRequest returns the compiled rules of a request, compiling them the first
// time the type is asked for and never again. It is safe for concurrent use.
//
// The failure of a type that does not compile is remembered with it: the same
// error answers every request, until the program is fixed and started again.
func (f *Factory) ForRequest(request RequestRules) (*RequestSet, error) {
	t := reflect.TypeOf(request)

	f.requests.mu.Lock()
	if f.requests.known == nil {
		f.requests.known = map[reflect.Type]*compiledRequest{}
	}
	entry, held := f.requests.known[t]
	if !held {
		entry = &compiledRequest{}
		f.requests.known[t] = entry
	}
	f.requests.mu.Unlock()

	entry.once.Do(func() { entry.set, entry.err = f.compileRequest(t, request) })
	return entry.set, entry.err
}

// compileRequest reads the rules of a request type, off a value that holds
// nothing of any request.
func (f *Factory) compileRequest(t reflect.Type, request RequestRules) (*RequestSet, error) {
	var zero RequestRules
	if t.Kind() == reflect.Pointer {
		zero = reflect.New(t.Elem()).Interface().(RequestRules)
	} else {
		zero = reflect.New(t).Elem().Interface().(RequestRules)
	}

	file, line := sourceOf(t, "Rules")
	var opts []Option
	if messages, ok := zero.(RequestMessages); ok {
		opts = append(opts, WithMessageOverrides(messages.Messages()))
	}
	set, err := compile(zero.Rules(), file, line, append([]Option{WithRegistry(f.registry)}, opts...))
	if err != nil {
		return nil, fmt.Errorf("%s.Rules: %w", t, err)
	}

	compiled := &RequestSet{Set: set}
	if attributes, ok := zero.(RequestAttributes); ok {
		compiled.Attributes = attributes.Attributes()
	}
	return compiled, nil
}

// sourceOf locates the declaration of a method of a type, for the message that
// names where a rule set was written.
func sourceOf(t reflect.Type, method string) (string, int) {
	m, found := t.MethodByName(method)
	if !found {
		return "?", 0
	}
	fn := runtime.FuncForPC(m.Func.Pointer())
	if fn == nil {
		return "?", 0
	}
	return fn.FileLine(m.Func.Pointer())
}

// CheckRequests is the boot check for the requests of an application: it
// compiles the rules every one of them declares, with the modules installed, and
// fails naming the type, the field, the rule and the file for any rule nothing
// declares. Every failure is reported.
//
//	if err := factory.CheckRequests(&StorePerson{}, &UpdatePerson{}); err != nil {
//		return err
//	}
func (f *Factory) CheckRequests(requests ...RequestRules) error {
	var failures []error
	for _, request := range requests {
		if _, err := f.ForRequest(request); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// ValidateRequest runs the rules a request declares over the data, with the
// attribute names it declares, and returns what passed or the error the failures
// make.
func (f *Factory) ValidateRequest(request RequestRules, data Data, opts ...ValidatorOption) (Input, error) {
	compiled, err := f.ForRequest(request)
	if err != nil {
		return Input{}, err
	}
	if len(compiled.Attributes) > 0 {
		opts = append([]ValidatorOption{WithCustomAttributes(compiled.Attributes)}, opts...)
	}
	return f.Make(data, compiled.Set, opts...).Validate()
}
