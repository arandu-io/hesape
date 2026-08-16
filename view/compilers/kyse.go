package compilers

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// DirectiveHandler is the callable a custom directive registers.
//
// It receives the expression written between the parentheses, already
// stripped of them, and returns what the directive compiles to.
type DirectiveHandler func(expression string) string

// ConditionHandler is the callable [KyseCompiler.If] registers.
//
// It is variadic so a condition can take any number of parameters, and
// returns bool because that is what [KyseCompiler.Check] calls it for.
type ConditionHandler func(parameters ...any) bool

// AnonymousComponentPath is one entry of the anonymous component search path.
type AnonymousComponentPath struct {
	// Path is the directory the components are read from.
	Path string

	// Prefix is the tag prefix, empty when the path is used unprefixed.
	Prefix string

	// PrefixHash is the namespace the view factory registers the path under.
	PrefixHash string
}

// ErrInvalidDirectiveName is returned by Directive for a name that is not a
// bare word.
var ErrInvalidDirectiveName = errors.New("view/compilers: directive names must contain only alphanumeric characters and underscores")

// ErrConditionMissing is returned by Check for a condition that was never
// registered with If.
var ErrConditionMissing = errors.New("view/compilers: condition is not registered")

var directiveNamePattern = regexp.MustCompile(`^\w+(?:::\w+)?$`)

// KyseCompiler is the concrete view compiler.
//
// The source extension is .kyse.go, and what it compiles into is Go. The
// closed set of built-in directives -- @if, @foreach, @section, @yield -- is
// emitted by a separate front end that owns the grammar. What lives here is
// the machinery around that set: the custom directive registry, the
// condition registry, the component aliases and namespaces, the echo
// format, the raw-block store and the precompiler chain. A directive this
// compiler does not know is left untouched, so that the two halves compose
// instead of racing.
//
// The zero value is not usable; create one with NewKyseCompiler.
type KyseCompiler struct {
	*Compiler

	// extensions are the callables Extend registers.
	extensions []func(string) string

	// customDirectives are the handlers Directive registers.
	customDirectives map[string]DirectiveHandler

	// conditions are the callables If registers.
	conditions map[string]ConditionHandler

	// prepareCallbacks are the callables prepareStringsForCompilationUsing
	// registers.
	prepareCallbacks []func(string) string

	// precompilers are the callables Precompiler registers.
	precompilers []func(string) string

	// echoFormat is the format a regular echo compiles through.
	echoFormat string

	// echoHandlers are the handlers Stringable registers, keyed by the type
	// name the value reports.
	echoHandlers map[string]func(any) any

	// footer holds lines appended after the template body.
	footer []string

	// rawBlocks holds the verbatim and @php blocks lifted out before
	// compilation and put back after it.
	rawBlocks []string

	anonymousComponentPaths      []AnonymousComponentPath
	anonymousComponentNamespaces map[string]string
	classComponentAliases        map[string]string
	classComponentNamespaces     map[string]string

	// compilesComponentTags reports whether <x-...> tags are compiled.
	compilesComponentTags bool

	// tags is the component tag precompile step.
	tags *ComponentTagCompiler
}

// NewKyseCompiler returns a compiler backed by a cache at cachePath.
func NewKyseCompiler(cachePath, basePath string, shouldCache bool) (*KyseCompiler, error) {
	base, err := NewCompiler(cachePath, basePath, shouldCache, "go", true)
	if err != nil {
		return nil, err
	}
	c := &KyseCompiler{
		Compiler:                     base,
		customDirectives:             map[string]DirectiveHandler{},
		conditions:                   map[string]ConditionHandler{},
		echoFormat:                   "Text(%s)",
		echoHandlers:                 map[string]func(any) any{},
		anonymousComponentNamespaces: map[string]string{},
		classComponentAliases:        map[string]string{},
		classComponentNamespaces:     map[string]string{},
		compilesComponentTags:        true,
	}
	c.tags = NewComponentTagCompiler(c.classComponentAliases, c.classComponentNamespaces, c)
	return c, nil
}

// Compile reads path, compiles it, and writes the result to the cache. It
// returns an error if the source cannot be read or the result cannot be
// written.
func (c *KyseCompiler) Compile(path string) error {
	if path != "" {
		c.SetPath(path)
	}
	if c.GetCachePath() == "" {
		return ErrCachePathMissing
	}

	source, err := os.ReadFile(c.GetPath())
	if err != nil {
		return err
	}

	contents := c.CompileString(string(source))

	compiled := c.GetCompiledPath(c.GetPath())
	if err := c.ensureCompiledDirectoryExists(compiled); err != nil {
		return err
	}
	return os.WriteFile(compiled, []byte(contents), 0o644)
}

// CompileString compiles value into Go, in this order: the prepare
// callbacks run, then the raw blocks are lifted out, then comments, then
// component tags, then the precompilers, then the extensions, the
// statements and the echos, and finally the raw blocks and the footer go
// back in.
func (c *KyseCompiler) CompileString(value string) string {
	c.footer = nil
	c.rawBlocks = nil

	for _, callback := range c.prepareCallbacks {
		value = callback(value)
	}

	value = c.storeUncompiledBlocks(value)
	value = c.compileComments(value)

	if c.compilesComponentTags && c.tags != nil {
		value = c.tags.Compile(value)
	}

	for _, precompiler := range c.precompilers {
		value = precompiler(value)
	}

	result := c.compileExtensions(value)
	result = c.compileStatements(result)
	result = c.CompileEchos(result)

	if len(c.rawBlocks) > 0 {
		result = c.restoreRawContent(result)
	}
	if len(c.footer) > 0 {
		result = c.addFooters(result)
	}

	return strings.NewReplacer(
		"##BEGIN-COMPONENT-CLASS##", "",
		"##END-COMPONENT-CLASS##", "",
	).Replace(result)
}

// StripParentheses removes one matching pair of enclosing parentheses.
func (c *KyseCompiler) StripParentheses(expression string) string {
	if strings.HasPrefix(expression, "(") && strings.HasSuffix(expression, ")") && len(expression) >= 2 {
		return expression[1 : len(expression)-1]
	}
	return expression
}

// Extend registers a function that runs over the whole template as part of
// compilation.
func (c *KyseCompiler) Extend(compiler func(string) string) {
	c.extensions = append(c.extensions, compiler)
}

// GetExtensions returns the functions registered with Extend.
func (c *KyseCompiler) GetExtensions() []func(string) string { return c.extensions }

// If registers the condition under name and the four directives that come
// with it: @name, @unlessname, @elsename and @endname. The emitted text
// calls Check, which is what makes the registered callback run at compile
// time.
func (c *KyseCompiler) If(name string, callback ConditionHandler) {
	c.conditions[name] = callback

	_ = c.Directive(name, func(expression string) string {
		if expression != "" {
			return fmt.Sprintf("if Check(%q, %s) {", name, expression)
		}
		return fmt.Sprintf("if Check(%q) {", name)
	})
	_ = c.Directive("unless"+name, func(expression string) string {
		if expression != "" {
			return fmt.Sprintf("if !Check(%q, %s) {", name, expression)
		}
		return fmt.Sprintf("if !Check(%q) {", name)
	})
	_ = c.Directive("else"+name, func(expression string) string {
		if expression != "" {
			return fmt.Sprintf("} else if Check(%q, %s) {", name, expression)
		}
		return fmt.Sprintf("} else if Check(%q) {", name)
	})
	_ = c.Directive("end"+name, func(string) string { return "}" })
}

// Check runs the condition registered under name with parameters, and
// returns an error if none was registered.
func (c *KyseCompiler) Check(name string, parameters ...any) (bool, error) {
	condition, ok := c.conditions[name]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrConditionMissing, name)
	}
	return condition(parameters...), nil
}

// Component registers class under alias, computing a default alias from the
// class name when alias is empty.
//
// If alias looks like a class path (it contains a backslash) while class
// does not, the two are swapped before proceeding, so that a call site
// written with the arguments in either order works the same.
func (c *KyseCompiler) Component(class, alias, prefix string) {
	if alias != "" && strings.Contains(alias, "\\") {
		class, alias = alias, class
	}
	if alias == "" {
		if i := strings.Index(class, "\\View\\Components\\"); i >= 0 {
			segments := strings.Split(class[i+len("\\View\\Components\\"):], "\\")
			for j, segment := range segments {
				segments[j] = kebab(segment)
			}
			alias = strings.Join(segments, ":")
		} else {
			alias = kebab(classBasename(class))
		}
	}
	if prefix != "" {
		alias = prefix + "-" + alias
	}
	c.classComponentAliases[alias] = class
}

// Components registers many components at once from a map of class to
// alias. An empty alias means "guess it from the class name," the same as
// calling Component with an empty alias.
func (c *KyseCompiler) Components(components map[string]string, prefix string) {
	for class, alias := range components {
		c.Component(class, alias, prefix)
	}
}

// GetClassComponentAliases returns the registered class-to-alias map.
func (c *KyseCompiler) GetClassComponentAliases() map[string]string {
	return c.classComponentAliases
}

// AnonymousComponentPath registers a directory of anonymous components
// under prefix.
//
// There is no container to resolve a view factory from and register the
// namespace on automatically, so the entry carries the prefix hash and the
// caller registers the namespace on the factory it already holds.
func (c *KyseCompiler) AnonymousComponentPath(path, prefix string) {
	seed := prefix
	if seed == "" {
		seed = path
	}
	c.anonymousComponentPaths = append(c.anonymousComponentPaths, AnonymousComponentPath{
		Path:       path,
		Prefix:     prefix,
		PrefixHash: shortHash(seed),
	})
}

// AnonymousComponentNamespace maps prefix to a namespace derived from
// directory.
func (c *KyseCompiler) AnonymousComponentNamespace(directory, prefix string) {
	if prefix == "" {
		prefix = directory
	}
	c.anonymousComponentNamespaces[prefix] = strings.Trim(strings.ReplaceAll(directory, "/", "."), ". ")
}

// ComponentNamespace maps prefix to namespace for class-based components.
func (c *KyseCompiler) ComponentNamespace(namespace, prefix string) {
	c.classComponentNamespaces[prefix] = namespace
}

// GetAnonymousComponentPaths returns the registered anonymous component
// directories.
func (c *KyseCompiler) GetAnonymousComponentPaths() []AnonymousComponentPath {
	return c.anonymousComponentPaths
}

// GetAnonymousComponentNamespaces returns the registered prefix-to-namespace
// map for anonymous components.
func (c *KyseCompiler) GetAnonymousComponentNamespaces() map[string]string {
	return c.anonymousComponentNamespaces
}

// GetClassComponentNamespaces returns the registered prefix-to-namespace map
// for class-based components.
func (c *KyseCompiler) GetClassComponentNamespaces() map[string]string {
	return c.classComponentNamespaces
}

// AliasComponent registers a directive pair -- @alias and @endalias -- that
// start and render the component at path.
func (c *KyseCompiler) AliasComponent(path, alias string) {
	if alias == "" {
		alias = lastSegment(path, ".")
	}
	_ = c.Directive(alias, func(expression string) string {
		if expression != "" {
			return fmt.Sprintf("StartComponent(%q, %s)", path, expression)
		}
		return fmt.Sprintf("StartComponent(%q)", path)
	})
	_ = c.Directive("end"+alias, func(string) string { return "RenderComponent()" })
}

// Include is an older spelling of AliasInclude, and delegates to it.
func (c *KyseCompiler) Include(path, alias string) { c.AliasInclude(path, alias) }

// AliasInclude registers a directive that compiles to an Include call for
// path.
func (c *KyseCompiler) AliasInclude(path, alias string) {
	if alias == "" {
		alias = lastSegment(path, ".")
	}
	_ = c.Directive(alias, func(expression string) string {
		expression = c.StripParentheses(expression)
		if expression == "" {
			expression = "nil"
		}
		return fmt.Sprintf("Include(%q, %s)", path, expression)
	})
}

// BindDirective registers a directive whose handler takes the compiler as
// an explicit argument, for a handler that needs it.
//
// A closure could already capture c from its enclosing scope without this,
// but the call site reads the same as it would for a plain
// func(string) string, which is why this variant exists.
func (c *KyseCompiler) BindDirective(name string, handler func(*KyseCompiler, string) string) error {
	return c.Directive(name, func(expression string) string { return handler(c, expression) })
}

// Directive registers handler under name, and returns
// ErrInvalidDirectiveName if name is not a bare word.
func (c *KyseCompiler) Directive(name string, handler DirectiveHandler) error {
	if !directiveNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %s", ErrInvalidDirectiveName, name)
	}
	c.customDirectives[name] = handler
	return nil
}

// GetCustomDirectives returns the registered directive handlers.
func (c *KyseCompiler) GetCustomDirectives() map[string]DirectiveHandler {
	return c.customDirectives
}

// PrepareStringsForCompilationUsing registers a function that transforms
// the template before compilation begins.
func (c *KyseCompiler) PrepareStringsForCompilationUsing(callback func(string) string) *KyseCompiler {
	c.prepareCallbacks = append(c.prepareCallbacks, callback)
	return c
}

// Precompiler registers a function that runs over the template after
// component tags are expanded, before directives and echos are compiled.
func (c *KyseCompiler) Precompiler(precompiler func(string) string) {
	c.precompilers = append(c.precompilers, precompiler)
}

// SetEchoFormat sets the format string a regular echo compiles through.
func (c *KyseCompiler) SetEchoFormat(format string) { c.echoFormat = format }

// WithDoubleEncoding sets the echo format to double-encode entities.
func (c *KyseCompiler) WithDoubleEncoding() { c.SetEchoFormat("Text(%s, true)") }

// WithoutDoubleEncoding sets the echo format to leave entities as they are.
func (c *KyseCompiler) WithoutDoubleEncoding() { c.SetEchoFormat("Text(%s, false)") }

// WithoutComponentTags turns off <x-...> tag expansion.
func (c *KyseCompiler) WithoutComponentTags() { c.compilesComponentTags = false }

// AddFooter appends a line to be emitted after the template body.
//
// Go has no protected field, so this is how code outside this file reaches
// the footer.
func (c *KyseCompiler) AddFooter(line string) { c.footer = append(c.footer, line) }

// commentPattern matches {{-- --}} comments.
var commentPattern = regexp.MustCompile(`(?s)\{\{--.*?--\}\}`)

// compileComments strips {{-- --}} comments from value.
func (c *KyseCompiler) compileComments(value string) string {
	return commentPattern.ReplaceAllString(value, "")
}

var (
	verbatimPattern = regexp.MustCompile(`(?s)(^|[^@])@verbatim(\s*)(.*?)@endverbatim`)
	phpBlockPattern = regexp.MustCompile(`(?s)(^|[^@])@php(.*?)@endphp`)
)

// storeUncompiledBlocks lifts @verbatim and @php block contents out of
// value, replacing each with a placeholder restoreRawContent later resolves.
func (c *KyseCompiler) storeUncompiledBlocks(value string) string {
	if strings.Contains(value, "@verbatim") {
		value = verbatimPattern.ReplaceAllStringFunc(value, func(match string) string {
			groups := verbatimPattern.FindStringSubmatch(match)
			return groups[1] + c.storeRawBlock(groups[3])
		})
	}
	if strings.Contains(value, "@php") {
		value = phpBlockPattern.ReplaceAllStringFunc(value, func(match string) string {
			groups := phpBlockPattern.FindStringSubmatch(match)
			return groups[1] + c.storeRawBlock(groups[2])
		})
	}
	return value
}

// storeRawBlock records value and returns the placeholder that stands in
// for it.
func (c *KyseCompiler) storeRawBlock(value string) string {
	c.rawBlocks = append(c.rawBlocks, value)
	return c.getRawPlaceholder(strconv.Itoa(len(c.rawBlocks) - 1))
}

// getRawPlaceholder returns the placeholder text for the raw block at index
// replace.
func (c *KyseCompiler) getRawPlaceholder(replace string) string {
	return "@__raw_block_" + replace + "__@"
}

var rawPlaceholderPattern = regexp.MustCompile(`@__raw_block_(\d+)__@`)

// restoreRawContent replaces every raw-block placeholder in result with the
// content it stands in for.
func (c *KyseCompiler) restoreRawContent(result string) string {
	result = rawPlaceholderPattern.ReplaceAllStringFunc(result, func(match string) string {
		index, err := strconv.Atoi(rawPlaceholderPattern.FindStringSubmatch(match)[1])
		if err != nil || index >= len(c.rawBlocks) {
			return match
		}
		return c.rawBlocks[index]
	})
	c.rawBlocks = nil
	return result
}

// addFooters appends the footer lines to result, in reverse registration
// order.
func (c *KyseCompiler) addFooters(result string) string {
	reversed := make([]string, 0, len(c.footer))
	for i := len(c.footer) - 1; i >= 0; i-- {
		reversed = append(reversed, c.footer[i])
	}
	return strings.TrimRight(result, "\n") + "\n\n" + strings.Join(reversed, "\n")
}

// compileExtensions runs every function registered with Extend over value,
// in registration order.
func (c *KyseCompiler) compileExtensions(value string) string {
	for _, compiler := range c.extensions {
		value = compiler(value)
	}
	return value
}

// compileStatements expands every @directive in template.
//
// Matching a directive with a balanced parenthesis group needs recursion,
// which RE2 does not support and will not grow. The scan below does it by
// hand: find an @, take the name, and if a parenthesis follows, walk to its
// balanced close counting quotes on the way.
func (c *KyseCompiler) compileStatements(template string) string {
	var out strings.Builder
	for i := 0; i < len(template); {
		if template[i] != '@' {
			out.WriteByte(template[i])
			i++
			continue
		}
		// @@directive escapes to a literal @directive.
		if i+1 < len(template) && template[i+1] == '@' {
			out.WriteString("@")
			i += 2
			continue
		}
		name, next := scanDirectiveName(template, i+1)
		if name == "" {
			out.WriteByte(template[i])
			i++
			continue
		}
		expression, after := scanDirectiveExpression(template, next)
		handler, ok := c.customDirectives[name]
		if !ok {
			// A directive this compiler does not own belongs to a separate
			// front end. It goes through untouched rather than being mangled.
			out.WriteString(template[i:after])
			i = after
			continue
		}
		out.WriteString(handler(strings.TrimSpace(c.StripParentheses(expression))))
		i = after
	}
	return out.String()
}

// scanDirectiveName reads the \w+(::\w+)? that follows an @.
func scanDirectiveName(template string, i int) (string, int) {
	start := i
	for i < len(template) && isWordByte(template[i]) {
		i++
	}
	if i == start {
		return "", start
	}
	if i+1 < len(template) && template[i] == ':' && template[i+1] == ':' {
		j := i + 2
		for j < len(template) && isWordByte(template[j]) {
			j++
		}
		if j > i+2 {
			i = j
		}
	}
	return template[start:i], i
}

// scanDirectiveExpression reads the balanced parenthesis group that may
// follow a directive name, skipping whitespace between the two.
func scanDirectiveExpression(template string, i int) (string, int) {
	j := i
	for j < len(template) && (template[j] == ' ' || template[j] == '\t') {
		j++
	}
	if j >= len(template) || template[j] != '(' {
		return "", i
	}
	depth := 0
	var quote byte
	for k := j; k < len(template); k++ {
		ch := template[k]
		if quote != 0 {
			if ch == '\\' {
				k++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return template[j : k+1], k + 1
			}
		}
	}
	return "", i
}

func isWordByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// kebab converts a name to kebab-case, kept local so this package stays on
// the standard library.
func kebab(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		switch {
		case ch >= 'A' && ch <= 'Z':
			if i > 0 {
				out.WriteByte('-')
			}
			out.WriteByte(ch + ('a' - 'A'))
		case ch == ' ' || ch == '_':
			out.WriteByte('-')
		default:
			out.WriteByte(ch)
		}
	}
	return out.String()
}

// classBasename returns the last backslash-separated segment of class.
func classBasename(class string) string { return lastSegment(class, "\\") }

func lastSegment(value, separator string) string {
	if i := strings.LastIndex(value, separator); i >= 0 {
		return value[i+len(separator):]
	}
	return value
}
