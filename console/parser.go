package console

import (
	"fmt"
	"regexp"
	"strings"
)

// ArgumentMode is what a positional argument accepts.
//
// It is a bit field: an array argument that is also required is
// ArgumentIsArray|ArgumentRequired.
type ArgumentMode int

const (
	// ArgumentRequired means the command does not run without it.
	ArgumentRequired ArgumentMode = 1
	// ArgumentOptional means it may be left out, and Default stands in.
	ArgumentOptional ArgumentMode = 2
	// ArgumentIsArray means it swallows every remaining operand.
	ArgumentIsArray ArgumentMode = 4
)

// OptionMode is what a --flag accepts.
//
// A signature has no syntax for a value that is required, but the constant
// is here because a definition a command builds by hand can use it.
type OptionMode int

const (
	// OptionValueNone is a boolean flag: --force.
	OptionValueNone OptionMode = 1
	// OptionValueRequired means --queue without a value is an error.
	OptionValueRequired OptionMode = 2
	// OptionValueOptional means --queue may stand alone, and Default stands in.
	OptionValueOptional OptionMode = 4
	// OptionValueIsArray means the flag may be repeated.
	OptionValueIsArray OptionMode = 8
)

// Argument is one positional operand of a command.
//
// The signature "{user}" is one of these, and so is "{ids?*}".
type Argument struct {
	// Name is what argument() is called with.
	Name string

	// Mode is the bit field above.
	Mode ArgumentMode

	// Description is the sentence after " : " in the signature. It is what the
	// help renders next to the name.
	Description string

	// Default is what stands in when the argument was left out.
	//
	// It is a slice and not a string because "{ids=1,2}" has two of them and
	// "{name=guest}" has one: a scalar argument reads Default[0] and an array
	// argument reads all of it.
	Default []string
}

// IsRequired reports whether the command refuses to run without the
// argument.
func (a Argument) IsRequired() bool { return a.Mode&ArgumentRequired != 0 }

// IsArray reports whether the argument swallows every remaining operand.
func (a Argument) IsArray() bool { return a.Mode&ArgumentIsArray != 0 }

// Option is one --flag of a command.
type Option struct {
	// Name is what option() is called with, without the leading dashes.
	Name string

	// Shortcut is the single letter form, without the dash, or empty. It comes
	// from the "Q|queue=" spelling in a signature.
	Shortcut string

	// Mode is the bit field above.
	Mode OptionMode

	// Description is the sentence after " : " in the signature.
	Description string

	// Default is what stands in when the flag was given without a value, for
	// the same reason Argument.Default is a slice.
	Default []string
}

// AcceptValue reports whether the option takes a value at all.
func (o Option) AcceptValue() bool {
	return o.Mode&(OptionValueRequired|OptionValueOptional) != 0
}

// IsValueRequired reports whether --name must be followed by a value.
func (o Option) IsValueRequired() bool { return o.Mode&OptionValueRequired != 0 }

// IsArray reports whether the flag may be repeated and collects every value.
func (o Option) IsArray() bool { return o.Mode&OptionValueIsArray != 0 }

// tokenPattern finds every {...} in a signature.
var tokenPattern = regexp.MustCompile(`\{\s*(.*?)\s*\}`)

// namePattern is the first run of non-space characters: the command name.
var namePattern = regexp.MustCompile(`[^\s]+`)

// optionPattern is what tells an option token from an argument token: two or
// more leading dashes.
var optionPattern = regexp.MustCompile(`^-{2,}(.*)`)

// descriptionPattern is the " : " that separates a token from its sentence. The
// spaces are required, so a token may contain a colon of its own.
var descriptionPattern = regexp.MustCompile(`\s+:\s+`)

// shortcutPattern is the "|" that separates a shortcut from a long name.
var shortcutPattern = regexp.MustCompile(`\s*\|\s*`)

// arrayDefaultPattern matches "name=*a,b": an array with defaults.
var arrayDefaultPattern = regexp.MustCompile(`^(.+)=\*(.+)$`)

// scalarDefaultPattern matches "name=value": a scalar with a default.
var scalarDefaultPattern = regexp.MustCompile(`^(.+)=(.+)$`)

// defaultSplitPattern splits a default list on its commas.
var defaultSplitPattern = regexp.MustCompile(`,\s?`)

// Parse reads a command signature into a name, its arguments and its options.
//
// The syntax is:
//
//	mail:send {user}                  a required argument
//	mail:send {user?}                 an optional one
//	mail:send {user=guest}            optional, with a default
//	mail:send {user*}                 required, and takes every operand left
//	mail:send {user?*}                the same, but may be empty
//	mail:send {user=*a,b}             an array with two defaults
//	mail:send {--queue}               a boolean flag
//	mail:send {--queue=}              a flag that takes a value
//	mail:send {--queue=default}       the same, with a default
//	mail:send {--queue=*}             a flag that may be repeated
//	mail:send {--Q|queue=}            the same, with a shortcut
//	mail:send {user : The user ID}    a description, after a spaced colon
func Parse(expression string) (name string, arguments []Argument, options []Option, err error) {
	name, err = parseName(expression)
	if err != nil {
		return "", nil, nil, err
	}

	for _, match := range tokenPattern.FindAllStringSubmatch(expression, -1) {
		token := match[1]
		if option := optionPattern.FindStringSubmatch(token); option != nil {
			parsed, err := parseOption(option[1])
			if err != nil {
				return "", nil, nil, err
			}
			options = append(options, parsed)
			continue
		}
		parsed, err := parseArgument(token)
		if err != nil {
			return "", nil, nil, err
		}
		arguments = append(arguments, parsed)
	}
	return name, arguments, options, nil
}

// MustParse is Parse for a signature written in source.
//
// It panics, which is right for a constant: a command whose signature does not
// parse must not reach the registry, and there is nobody to hand the error to
// at the point a package-level Command is built.
func MustParse(expression string) (string, []Argument, []Option) {
	name, arguments, options, err := Parse(expression)
	if err != nil {
		panic("console: " + err.Error())
	}
	return name, arguments, options
}

// parseName extracts the command name from the signature.
func parseName(expression string) (string, error) {
	name := namePattern.FindString(expression)
	if name == "" {
		return "", fmt.Errorf("console: unable to determine command name from signature %q", expression)
	}
	// A signature that opens with a token has no name: "{user}" would otherwise
	// be read as a command called "{user}".
	if strings.HasPrefix(name, "{") {
		return "", fmt.Errorf("console: unable to determine command name from signature %q", expression)
	}
	return name, nil
}

// parseArgument reads one argument token.
//
// The order of the cases matters: "{ids?*}" ends with "*" as well as with
// "?*", and the first test has to be the longer one.
func parseArgument(token string) (Argument, error) {
	token, description := extractDescription(token)

	switch {
	case strings.HasSuffix(token, "?*"):
		return Argument{Name: trimAny(token, "?*"), Mode: ArgumentIsArray, Description: description}, nil
	case strings.HasSuffix(token, "*"):
		return Argument{Name: trimAny(token, "*"), Mode: ArgumentIsArray | ArgumentRequired, Description: description}, nil
	case strings.HasSuffix(token, "?"):
		return Argument{Name: trimAny(token, "?"), Mode: ArgumentOptional, Description: description}, nil
	}

	if m := arrayDefaultPattern.FindStringSubmatch(token); m != nil {
		return Argument{
			Name:        m[1],
			Mode:        ArgumentIsArray,
			Description: description,
			Default:     defaultSplitPattern.Split(m[2], -1),
		}, nil
	}
	if m := scalarDefaultPattern.FindStringSubmatch(token); m != nil {
		return Argument{
			Name:        m[1],
			Mode:        ArgumentOptional,
			Description: description,
			Default:     []string{m[2]},
		}, nil
	}

	if token == "" {
		return Argument{}, fmt.Errorf("console: an empty {} is not an argument")
	}
	return Argument{Name: token, Mode: ArgumentRequired, Description: description}, nil
}

// parseOption reads one option token, the leading dashes already stripped.
func parseOption(token string) (Option, error) {
	token, description := extractDescription(token)

	shortcut := ""
	if parts := shortcutPattern.Split(token, 2); len(parts) == 2 {
		shortcut, token = parts[0], parts[1]
	}

	switch {
	case strings.HasSuffix(token, "=*"):
		return Option{
			Name:        trimAny(token, "=*"),
			Shortcut:    shortcut,
			Mode:        OptionValueOptional | OptionValueIsArray,
			Description: description,
		}, nil
	case strings.HasSuffix(token, "="):
		return Option{
			Name:        trimAny(token, "="),
			Shortcut:    shortcut,
			Mode:        OptionValueOptional,
			Description: description,
		}, nil
	}

	if m := arrayDefaultPattern.FindStringSubmatch(token); m != nil {
		return Option{
			Name:        m[1],
			Shortcut:    shortcut,
			Mode:        OptionValueOptional | OptionValueIsArray,
			Description: description,
			Default:     defaultSplitPattern.Split(m[2], -1),
		}, nil
	}
	if m := scalarDefaultPattern.FindStringSubmatch(token); m != nil {
		return Option{
			Name:        m[1],
			Shortcut:    shortcut,
			Mode:        OptionValueOptional,
			Description: description,
			Default:     []string{m[2]},
		}, nil
	}

	if token == "" {
		return Option{}, fmt.Errorf("console: an option needs a name after its dashes")
	}
	return Option{Name: token, Shortcut: shortcut, Mode: OptionValueNone, Description: description}, nil
}

// extractDescription splits a token into the token and the sentence after " : ".
func extractDescription(token string) (string, string) {
	parts := descriptionPattern.Split(strings.TrimSpace(token), 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return token, ""
}

// trimAny strips any of the characters from both ends, which is why
// "{--queue=*}" and "{ids?*}" lose their whole suffix.
func trimAny(token, chars string) string { return strings.Trim(token, chars) }
