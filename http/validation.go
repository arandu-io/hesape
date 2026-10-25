package http

import (
	"context"
	"errors"
	"sync"

	"github.com/arandu-io/hesape/translation"
	"github.com/arandu-io/hesape/validation"
)

// This file is how a request is validated by what it declares, in the language of
// the person who sent it.
//
// A request type that has a Rules method -- validation.RequestRules -- declares
// the rules of its fields, in either form a rule set is written in, and Bind runs
// them over the raw input before it converts anything. The rules are compiled
// once per type by the application's Factory, which carries the translator, the
// modules' rules and the settings of the application; the locale is the
// request's.
//
//	type StorePerson struct {
//		CPF   string `form:"cpf"`
//		Email string `form:"email"`
//	}
//
//	func (StorePerson) Rules() validation.Rules {
//		return validation.Rules{
//			"cpf":   "required|cpf",
//			"email": []any{"required", "email", "max:255"},
//		}
//	}
//
// Bind answers a failure with validation.Errors, which the adapter turns into a
// 422 problem document for a client that wants JSON and into a redirect back with
// the messages and the old input for a page. A request without Rules binds as it
// always did.
//
// Rules is the first of two steps and Validate, which the service calls, is the
// second: Rules says what each field may be, Validate says what the fields may be
// together, or against the data. Bind never calls Validate.

// validationKey is the context key of the Factory validation runs through.
type validationKey struct{}

// WithValidation returns a context carrying the Factory that Bind and
// Request.Validate validate through. The router puts it on every request, so that
// the translator, the modules' rules and the settings the application wired once
// are the ones every request is validated with.
func WithValidation(parent context.Context, f *validation.Factory) context.Context {
	if f == nil {
		return parent
	}
	return context.WithValue(parent, validationKey{}, f)
}

// ValidationFrom returns the Factory on a request context, and whether there is
// one.
func ValidationFrom(ctx context.Context) (*validation.Factory, bool) {
	f, ok := ctx.Value(validationKey{}).(*validation.Factory)
	return f, ok && f != nil
}

// AdapterOption adjusts what NewAdapter puts on every request it runs.
type AdapterOption func(*adapterSettings)

type adapterSettings struct {
	validation *validation.Factory
}

// WithValidator makes every request the adapter runs validate through the
// Factory. It is the same as putting the Factory on the request context with
// WithValidation, once, where the actions are adapted.
func WithValidator(f *validation.Factory) AdapterOption {
	return func(s *adapterSettings) { s.validation = f }
}

// fallbackFactories are the Factories made for a context that carries a
// translator and no Factory, one per translator, so that the rules of a request
// type are compiled once and not once per request.
var fallbackFactories sync.Map

// bundledFactory is the Factory for a context that carries neither: it speaks the
// sentences that ship with the framework, in the locale of the request.
var bundledFactory = validation.NewFactory(validation.FromTranslation(translation.New(nil, "en", "en")))

// factoryFor returns the Factory a request validates through: the application's,
// and otherwise one over its translator, and otherwise one over the sentences
// that ship with the framework.
func factoryFor(ctx context.Context) *validation.Factory {
	if f, ok := ValidationFrom(ctx); ok {
		return f
	}
	tr := translation.TranslatorFrom(ctx)
	if tr == nil {
		return bundledFactory
	}
	if held, ok := fallbackFactories.Load(tr); ok {
		return held.(*validation.Factory)
	}
	made, _ := fallbackFactories.LoadOrStore(tr, validation.NewFactory(validation.FromTranslation(tr)))
	return made.(*validation.Factory)
}

// validationOptions are what a run over this request's input is given beyond the
// rules: the request's context, for the deadline of a rule that leaves the
// process, and its locale.
func validationOptions(ctx context.Context) []validation.ValidatorOption {
	opts := []validation.ValidatorOption{validation.WithContext(ctx)}
	if locale := translation.Locale(ctx); locale != "" {
		opts = append(opts, validation.WithLocale(locale))
	}
	return opts
}

// configured reports whether the application wired anything to say validation
// sentences with. A request on a context that has neither a Factory nor a
// translator keeps the short sentences it has always had.
func configured(ctx context.Context) bool {
	if _, ok := ValidationFrom(ctx); ok {
		return true
	}
	return translation.TranslatorFrom(ctx) != nil
}

// runRules validates the raw input of the request by the rules its type
// declares, and returns what passed. A failure is validation.Errors, the type
// Bind answers a failed conversion with.
func (c *Context) runRules(input *Request, declaring validation.RequestRules) error {
	factory := factoryFor(c.Request.Context())

	validated, err := factory.ValidateRequest(declaring, input.validationData(), validationOptions(c.Request.Context())...)
	if err != nil {
		var invalid *validation.ValidationException
		if errors.As(err, &invalid) {
			return validation.Errors(invalid.Errors())
		}
		return err
	}
	c.validated = validated
	return nil
}

// Validated returns what the rules the bound request declares let through: the
// fields that were declared and passed, nested the way their names declare, and
// nothing else the request sent. It is empty before Bind has run the rules of a
// request that declares some.
func (c *Context) Validated() validation.Input { return c.validated }

// localizeConversion says a failure of Bind's conversion in the language the
// application configured. The conversion has its own short sentences, which are
// kept for an application that wired no translator; with one, each failure is the
// sentence of the rule that means the same: integer, numeric, boolean or date.
func (c *Context) localizeConversion(errs validation.Errors, attributes map[string]string) validation.Errors {
	ctx := c.Request.Context()
	if !configured(ctx) {
		return errs
	}
	factory := factoryFor(ctx)

	out := validation.Errors{}
	for field, messages := range errs {
		for _, message := range messages {
			rule, known := conversionRules[message]
			if !known {
				out.Add(field, message)
				continue
			}
			set, err := factory.Compile(validation.Rules{field: []any{rule}})
			if err != nil {
				out.Add(field, message)
				continue
			}
			opts := append(validationOptions(ctx), validation.WithCustomAttributes(attributes))
			// The value is not the text that failed to convert, but the rule's
			// own sentence is all that is wanted of the run.
			sentences := factory.Make(validation.Data{field: "\x00"}, set, opts...).Errors().Get(field)
			if len(sentences) == 0 {
				out.Add(field, message)
				continue
			}
			out.Add(field, sentences[0])
		}
	}
	return out
}

// conversionRules maps the short sentences Bind's conversion says to the rule
// whose sentence says the same thing.
var conversionRules = map[string]string{
	"must be a whole number":                 "integer",
	"must be a whole number of zero or more": "integer",
	"must be a number":                       "numeric",
	"must be true or false":                  "boolean",
	"is not a valid date":                    "date",
	"is out of range":                        "numeric",
}

// requestAttributes reads how a request names its fields, when it says.
func requestAttributes(dst any) map[string]string {
	if named, ok := dst.(validation.RequestAttributes); ok {
		return named.Attributes()
	}
	return nil
}
