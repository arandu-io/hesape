package console

import (
	"fmt"
	"strconv"
	"strings"
)

// Value is one argument or option as it was given.
//
// It can be read as a string, a slice or a bool without the caller needing to
// know in advance which shape it holds: asking for the wrong shape returns
// the zero value of it rather than a panic.
type Value struct {
	values  []string
	present bool
	flag    bool
}

// String is the value as one string. An array value is its first element, and
// a value that was never given is empty.
func (v Value) String() string {
	if len(v.values) == 0 {
		return ""
	}
	return v.values[0]
}

// Slice is every value that was given, in order.
func (v Value) Slice() []string { return append([]string(nil), v.values...) }

// Bool is whether a flag was given.
//
// A flag that takes no value is true when it is present. One that takes a value
// is true when that value is not one of the spellings of no, so --force=false
// means what it reads as.
func (v Value) Bool() bool {
	if v.flag {
		return v.present
	}
	switch strings.ToLower(v.String()) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// Int is the value as a number.
func (v Value) Int() (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v.String()))
	if err != nil {
		return 0, fmt.Errorf("console: %q is not a number", v.String())
	}
	return n, nil
}

// Present reports whether the value was given on the command line, as opposed
// to standing in from a default.
func (v Value) Present() bool { return v.present }

// Input is the command line, bound to what a command declared it accepts.
//
// It is here and not in a package of its own because a definition with
// nothing to bind it to is a definition nobody can read back.
type Input struct {
	arguments []Argument
	options   []Option

	argumentValues map[string]*Value
	optionValues   map[string]*Value

	// interactive is false when --no-interaction was given, and a prompt with
	// it false returns its default instead of asking.
	interactive bool
}

// NewInput returns the input for a definition, before anything is parsed.
//
// Every declared argument and option starts on its default, so a command that
// reads one it did not receive gets what the signature said rather than an
// error at the point of use.
func NewInput(arguments []Argument, options []Option) *Input {
	in := &Input{
		arguments:      arguments,
		options:        options,
		argumentValues: map[string]*Value{},
		optionValues:   map[string]*Value{},
		interactive:    true,
	}
	for _, a := range arguments {
		in.argumentValues[a.Name] = &Value{values: append([]string(nil), a.Default...)}
	}
	for _, o := range options {
		in.optionValues[o.Name] = &Value{
			values: append([]string(nil), o.Default...),
			flag:   !o.AcceptValue(),
		}
	}
	return in
}

// Definition returns the arguments and options the input was built from, so
// the help can render them without a second copy of the signature.
func (in *Input) Definition() ([]Argument, []Option) { return in.arguments, in.options }

// Interactive reports whether the command may prompt.
func (in *Input) Interactive() bool { return in.interactive }

// SetInteractive turns prompting on or off, which is what --no-interaction
// does and what a command calling another passes on.
func (in *Input) SetInteractive(interactive bool) { in.interactive = interactive }

// Parse is the token loop that binds a command line to the definition.
//
// argv is what followed the command name. Everything after a bare "--" is an
// operand, however many dashes it starts with, which is how a value that
// looks like a flag reaches the command that wants it.
func (in *Input) Parse(argv []string) error {
	operands := []string{}
	onlyOperands := false

	for i := 0; i < len(argv); i++ {
		token := argv[i]

		switch {
		case onlyOperands:
			operands = append(operands, token)
		case token == "--":
			onlyOperands = true
		case strings.HasPrefix(token, "--"):
			consumed, err := in.parseLongOption(token[2:], argv[i+1:])
			if err != nil {
				return err
			}
			i += consumed
		case len(token) > 1 && strings.HasPrefix(token, "-"):
			consumed, err := in.parseShortOption(token[1:], argv[i+1:])
			if err != nil {
				return err
			}
			i += consumed
		default:
			operands = append(operands, token)
		}
	}

	return in.bindArguments(operands)
}

// parseLongOption binds --name, --name=value or --name value, and returns how
// many of the following tokens it took.
func (in *Input) parseLongOption(token string, rest []string) (int, error) {
	name, value, hasValue := strings.Cut(token, "=")

	option, found := in.findOption(name, "")
	if !found {
		return 0, Exit(1, "unknown option --%s", name)
	}
	if hasValue {
		return 0, in.set(option, value)
	}
	if !option.AcceptValue() {
		return 0, in.set(option, "")
	}
	// A value-taking option given without "=" takes the next token, unless the
	// next token is itself a flag: --queue --force means the queue took its
	// default, not the string "--force".
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		return 1, in.set(option, rest[0])
	}
	if option.IsValueRequired() {
		return 0, Exit(1, "the option --%s needs a value", option.Name)
	}
	return 0, in.setDefault(option)
}

// parseShortOption binds -n, -nvalue or -n value, and the bundle -abc.
func (in *Input) parseShortOption(token string, rest []string) (int, error) {
	shortcut := token[:1]
	option, found := in.findOption("", shortcut)
	if !found {
		return 0, Exit(1, "unknown option -%s", shortcut)
	}

	remainder := token[1:]
	if option.AcceptValue() {
		if remainder != "" {
			return 0, in.set(option, strings.TrimPrefix(remainder, "="))
		}
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			return 1, in.set(option, rest[0])
		}
		if option.IsValueRequired() {
			return 0, Exit(1, "the option -%s needs a value", shortcut)
		}
		return 0, in.setDefault(option)
	}

	if err := in.set(option, ""); err != nil {
		return 0, err
	}
	if remainder == "" {
		return 0, nil
	}
	// -abc is three flags, and it is the one place a short token carries more
	// than one option.
	return in.parseShortOption(remainder, rest)
}

// bindArguments hands the operands to the declared arguments, in order.
func (in *Input) bindArguments(operands []string) error {
	index := 0
	for _, argument := range in.arguments {
		if argument.IsArray() {
			if index < len(operands) {
				in.argumentValues[argument.Name] = &Value{values: operands[index:], present: true}
				index = len(operands)
			}
			if argument.IsRequired() && len(in.argumentValues[argument.Name].values) == 0 {
				return Exit(1, "the argument %s is required", argument.Name)
			}
			continue
		}

		if index < len(operands) {
			in.argumentValues[argument.Name] = &Value{values: []string{operands[index]}, present: true}
			index++
			continue
		}
		if argument.IsRequired() {
			return Exit(1, "the argument %s is required", argument.Name)
		}
	}

	if index < len(operands) {
		return Exit(1, "too many arguments: %s was not declared in the signature", operands[index])
	}
	return nil
}

// set records one value against an option.
func (in *Input) set(option Option, value string) error {
	current := in.optionValues[option.Name]
	if option.IsArray() {
		if !current.present {
			// The defaults are replaced by the first value given, not added to:
			// a flag with a default of a,b and one -q c on the line is c.
			current.values = nil
		}
		current.values = append(current.values, value)
		current.present = true
		return nil
	}
	current.values = []string{value}
	current.present = true
	return nil
}

// setDefault marks a value-taking option as present on its default.
func (in *Input) setDefault(option Option) error {
	current := in.optionValues[option.Name]
	current.present = true
	return nil
}

// findOption looks an option up by long name or by shortcut.
func (in *Input) findOption(name, shortcut string) (Option, bool) {
	for _, o := range in.options {
		if name != "" && o.Name == name {
			return o, true
		}
		if shortcut != "" && o.Shortcut == shortcut {
			return o, true
		}
	}
	return Option{}, false
}

// HasArgument reports whether the argument is declared.
//
// It reports declared, not given.
func (in *Input) HasArgument(name string) bool {
	_, found := in.argumentValues[name]
	return found
}

// Argument returns the value of a declared argument by name, or a zero Value
// if none was declared with that name.
func (in *Input) Argument(name string) Value {
	if v, found := in.argumentValues[name]; found {
		return *v
	}
	return Value{}
}

// Arguments returns every argument, keyed by name.
func (in *Input) Arguments() map[string]Value {
	out := make(map[string]Value, len(in.argumentValues))
	for name, v := range in.argumentValues {
		out[name] = *v
	}
	return out
}

// HasOption reports whether the option is declared in the command signature.
func (in *Input) HasOption(name string) bool {
	_, found := in.optionValues[name]
	return found
}

// Option returns the value of a declared option by name, or a zero Value if
// none was declared with that name.
func (in *Input) Option(name string) Value {
	if v, found := in.optionValues[name]; found {
		return *v
	}
	return Value{}
}

// Options returns every option, keyed by name.
func (in *Input) Options() map[string]Value {
	out := make(map[string]Value, len(in.optionValues))
	for name, v := range in.optionValues {
		out[name] = *v
	}
	return out
}

// setArgument records an answer a prompt supplied for a missing argument.
//
// It is unexported because the only caller is the prompt: a command that
// could rewrite its own arguments is a command whose arguments do not mean
// what the command line says.
func (in *Input) setArgument(name, value string) {
	if current, found := in.argumentValues[name]; found {
		current.values = []string{value}
		current.present = true
	}
}
