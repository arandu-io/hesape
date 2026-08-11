// Package validation answers to Illuminate\Validation: it checks a submitted
// request against a set of rules.
//
// The surface is Laravel's, string rules included, so that somebody arriving
// from it recognises the whole thing without reading anything:
//
//	var Register = validation.MustCompile(validation.Rules{
//		"name":     "required|max:255",
//		"email":    "required|email",
//		"password": "required|min:12|confirmed",
//	})
//
//	in, err := ctx.Validate(requests.Register)
//	if err != nil {
//		return err
//	}
//
// A rule set is compiled ONCE, in a package-level variable, and every rule
// string is parsed and checked there: an unknown rule, a missing or unparseable
// argument, a pattern that does not compile, a cross-field reference naming a
// field that does not exist -- each of those fails at boot, naming the field,
// the rule and the file, and all of them are reported together. A rule set that
// boots is a rule set whose names are all real.
//
// # What answers to what
//
// The clone the bodies were read from is laravel_illuminate/validation.
//
//	Concerns\ValidatesAttributes  attributes.go, one ValidateX per rule
//	Validator                     validator.go
//	Support\MessageBag            messages.go, as Errors
//	Rules\*                       rules_objects.go
//	ValidationRuleParser          compile.go, at boot instead of per request
//	Concerns\FormatsMessages      the message func of each rule in rules.go
//	ValidationException           CompileErrors, at boot rather than at runtime
//
// Illuminate\Validation\Concerns and Illuminate\Validation\Rules do not become
// packages of their own: a rule here is one entry in one table, and splitting
// the arity, the check and the message across three files is how a rule ends up
// accepting an argument nobody validates.
//
// # Where it differs, and why
//
// Four differences are worth knowing before writing anything, and each is said
// again at the symbol it belongs to (ADR 0044):
//
//   - date_format takes a GO layout -- date_format:2006-01-02, never
//     date_format:Y-m-d. It is the one Laravel string that cannot be copied,
//     because the string means something else in the host language.
//   - regex takes a GO pattern: RE2, without delimiters, without backreferences
//     and without lookaround. It is compiled at boot.
//   - `unique` and `exists` take an auth Grant and a PresenceVerifier, given
//     with WithPresence, and fail closed without one. RULE 17 does not except a
//     read: "the validator only counts rows" is how a count of rows becomes a
//     way to ask whether another tenant has a user with a given address.
//   - `current_password` takes a CurrentPasswordChecker rather than resolving a
//     guard and a hasher out of a container, which ADR 0002 refuses.
//
// # Reading what passed
//
// There is no reflection and there are no struct tags. The rule set is data,
// the request struct is written by hand or generated, and the values that
// passed are read out of Input one at a time -- so a field nobody declared a
// rule for cannot reach a repository through here.
package validation
