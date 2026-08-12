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
// It answers the callable BladeCompiler::directive takes: it receives the
// expression written between the parentheses, already stripped of them, and
// returns what the directive compiles to.
type DirectiveHandler func(expression string) string

// ConditionHandler is the callable BladeCompiler::if registers.
//
// It is variadic where PHP is variadic, and returns bool because that is what
// BladeCompiler::check calls it for.
type ConditionHandler func(parameters ...any) bool

// AnonymousComponentPath is one entry of BladeCompiler::$anonymousComponentPaths.
type AnonymousComponentPath struct {
	// Path is the directory the components are read from.
	Path string

	// Prefix is the tag prefix, empty when the path is used unprefixed.
	Prefix string

	// PrefixHash is the namespace the view factory registers the path under.
	PrefixHash string
}

// ErrInvalidDirectiveName is returned by Directive for a name that is not a
// bare word. It answers the InvalidArgumentException PHP throws.
var ErrInvalidDirectiveName = errors.New("view/compilers: directive names must contain only alphanumeric characters and underscores")

// ErrConditionMissing is returned by Check for a condition that was never
// registered with If.
var ErrConditionMissing = errors.New("view/compilers: condition is not registered")

var directiveNamePattern = regexp.MustCompile(`^\w+(?:::\w+)?$`)

// KyseCompiler mirrors Illuminate\View\Compilers\BladeCompiler.
//
// The tool is named kyse and the source extension is .kyse.go, because Blade
// is the PHP tool and this is not it. The method names are Laravel's, because
// what a directive is called and what registering one is called is the
// vocabulary a Laravel developer already has (ADR 0044).
//
// What it compiles into is Go rather than PHP. The closed set of built-in
// directives -- @if, @foreach, @section, @yield -- is emitted by the kyse
// front end in aru/internal/kyse, which owns the grammar (RULE 15). What lives
// here is the machinery around that set: the custom directive registry, the
// condition registry, the component aliases and namespaces, the echo format,
// the raw-block store and the precompiler chain. A directive this compiler
// does not know is left untouched, so that the two halves compose instead of
// racing.
//
// The zero value is not usable; create one with NewKyseCompiler.
type KyseCompiler struct {
	*Compiler

	// extensions are the callables BladeCompiler::extend registers.
	extensions []func(string) string

	// customDirectives are the handlers BladeCompiler::directive registers.
	customDirectives map[string]DirectiveHandler

	// conditions are the callables BladeCompiler::if registers.
	conditions map[string]ConditionHandler

	// prepareCallbacks are the callables prepareStringsForCompilationUsing
	// registers.
	prepareCallbacks []func(string) string

	// precompilers are the callables BladeCompiler::precompiler registers.
	precompilers []func(string) string

	// echoFormat is the format a regular echo compiles through.
	echoFormat string

	// echoHandlers are the handlers BladeCompiler::stringable registers,
	// keyed by the type name the value reports.
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

// NewKyseCompiler is BladeCompiler::__construct.
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

// Compile is BladeCompiler::compile and CompilerInterface::compile.
//
// It returns error where PHP relies on the Filesystem throwing.
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

// CompileString is BladeCompiler::compileString.
//
// The order is PHP's: the prepare callbacks, then the raw blocks are lifted
// out, then comments, then component tags, then the precompilers, then the
// extensions, the statements and the echos, and finally the raw blocks and
// the footer go back in.
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

// StripParentheses is BladeCompiler::stripParentheses.
func (c *KyseCompiler) StripParentheses(expression string) string {
	if strings.HasPrefix(expression, "(") && strings.HasSuffix(expression, ")") && len(expression) >= 2 {
		return expression[1 : len(expression)-1]
	}
	return expression
}

// Extend is BladeCompiler::extend.
func (c *KyseCompiler) Extend(compiler func(string) string) {
	c.extensions = append(c.extensions, compiler)
}

// GetExtensions is BladeCompiler::getExtensions.
func (c *KyseCompiler) GetExtensions() []func(string) string { return c.extensions }

// If is BladeCompiler::if.
//
// It registers the condition under name and the four directives PHP registers
// with it: @name, @unlessname, @elsename and @endname. The emitted text calls
// Check, which is what makes the registered callback run at compile time.
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

// Check is BladeCompiler::check.
//
// It returns (bool, error) where PHP calls a missing key and fatals.
func (c *KyseCompiler) Check(name string, parameters ...any) (bool, error) {
	condition, ok := c.conditions[name]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrConditionMissing, name)
	}
	return condition(parameters...), nil
}

// Component is BladeCompiler::component.
//
// PHP swaps its two arguments when the alias looks like a class name; that
// swap exists because PHP has one untyped argument list, and it is kept here
// so that the same call site works.
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

// Components is BladeCompiler::components.
//
// PHP takes one array whose numeric keys mean "guess the alias" and whose
// string keys mean class => alias. Go has no such array: the map is class to
// alias, and an empty alias is the numeric-key case.
func (c *KyseCompiler) Components(components map[string]string, prefix string) {
	for class, alias := range components {
		c.Component(class, alias, prefix)
	}
}

// GetClassComponentAliases is BladeCompiler::getClassComponentAliases.
func (c *KyseCompiler) GetClassComponentAliases() map[string]string {
	return c.classComponentAliases
}

// AnonymousComponentPath is BladeCompiler::anonymousComponentPath.
//
// PHP finishes by resolving the view factory out of the container and calling
// addNamespace on it. There is no container here (ADR 0001), so the entry
// carries the prefix hash and the caller registers the namespace on the
// factory it already holds.
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

// AnonymousComponentNamespace is BladeCompiler::anonymousComponentNamespace.
func (c *KyseCompiler) AnonymousComponentNamespace(directory, prefix string) {
	if prefix == "" {
		prefix = directory
	}
	c.anonymousComponentNamespaces[prefix] = strings.Trim(strings.ReplaceAll(directory, "/", "."), ". ")
}

// ComponentNamespace is BladeCompiler::componentNamespace.
func (c *KyseCompiler) ComponentNamespace(namespace, prefix string) {
	c.classComponentNamespaces[prefix] = namespace
}

// GetAnonymousComponentPaths is BladeCompiler::getAnonymousComponentPaths.
func (c *KyseCompiler) GetAnonymousComponentPaths() []AnonymousComponentPath {
	return c.anonymousComponentPaths
}

// GetAnonymousComponentNamespaces is BladeCompiler::getAnonymousComponentNamespaces.
func (c *KyseCompiler) GetAnonymousComponentNamespaces() map[string]string {
	return c.anonymousComponentNamespaces
}

// GetClassComponentNamespaces is BladeCompiler::getClassComponentNamespaces.
func (c *KyseCompiler) GetClassComponentNamespaces() map[string]string {
	return c.classComponentNamespaces
}

// AliasComponent is BladeCompiler::aliasComponent.
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

// Include is BladeCompiler::include.
//
// It is the older spelling of AliasInclude and delegates to it, as PHP does.
func (c *KyseCompiler) Include(path, alias string) { c.AliasInclude(path, alias) }

// AliasInclude is BladeCompiler::aliasInclude.
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

// BindDirective is BladeCompiler::bindDirective.
//
// PHP binds the handler closure to the compiler so that the handler can call
// $this. A Go handler that needs the compiler closes over it, so the two
// spellings differ only in that this one exists for the call site to keep
// reading the same.
func (c *KyseCompiler) BindDirective(name string, handler func(*KyseCompiler, string) string) error {
	return c.Directive(name, func(expression string) string { return handler(c, expression) })
}

// Directive is BladeCompiler::directive.
//
// It returns error where PHP throws InvalidArgumentException for a name that
// is not a bare word.
func (c *KyseCompiler) Directive(name string, handler DirectiveHandler) error {
	if !directiveNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %s", ErrInvalidDirectiveName, name)
	}
	c.customDirectives[name] = handler
	return nil
}

// GetCustomDirectives is BladeCompiler::getCustomDirectives.
func (c *KyseCompiler) GetCustomDirectives() map[string]DirectiveHandler {
	return c.customDirectives
}

// PrepareStringsForCompilationUsing is
// BladeCompiler::prepareStringsForCompilationUsing.
func (c *KyseCompiler) PrepareStringsForCompilationUsing(callback func(string) string) *KyseCompiler {
	c.prepareCallbacks = append(c.prepareCallbacks, callback)
	return c
}

// Precompiler is BladeCompiler::precompiler.
func (c *KyseCompiler) Precompiler(precompiler func(string) string) {
	c.precompilers = append(c.precompilers, precompiler)
}

// SetEchoFormat is BladeCompiler::setEchoFormat.
func (c *KyseCompiler) SetEchoFormat(format string) { c.echoFormat = format }

// WithDoubleEncoding is BladeCompiler::withDoubleEncoding.
func (c *KyseCompiler) WithDoubleEncoding() { c.SetEchoFormat("Text(%s, true)") }

// WithoutDoubleEncoding is BladeCompiler::withoutDoubleEncoding.
func (c *KyseCompiler) WithoutDoubleEncoding() { c.SetEchoFormat("Text(%s, false)") }

// WithoutComponentTags is BladeCompiler::withoutComponentTags.
func (c *KyseCompiler) WithoutComponentTags() { c.compilesComponentTags = false }

// AddFooter appends a line to be emitted after the template body.
//
// PHP writes $this->footer[] from inside CompilesLayouts; Go has no
// protected, so the concerns reach it through this.
func (c *KyseCompiler) AddFooter(line string) { c.footer = append(c.footer, line) }

// compileComments is Concerns\CompilesComments::compileComments.
var commentPattern = regexp.MustCompile(`(?s)\{\{--.*?--\}\}`)

func (c *KyseCompiler) compileComments(value string) string {
	return commentPattern.ReplaceAllString(value, "")
}

var (
	verbatimPattern = regexp.MustCompile(`(?s)(^|[^@])@verbatim(\s*)(.*?)@endverbatim`)
	phpBlockPattern = regexp.MustCompile(`(?s)(^|[^@])@php(.*?)@endphp`)
)

// storeUncompiledBlocks is BladeCompiler::storeUncompiledBlocks.
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

// storeRawBlock is BladeCompiler::storeRawBlock.
func (c *KyseCompiler) storeRawBlock(value string) string {
	c.rawBlocks = append(c.rawBlocks, value)
	return c.getRawPlaceholder(strconv.Itoa(len(c.rawBlocks) - 1))
}

// getRawPlaceholder is BladeCompiler::getRawPlaceholder.
func (c *KyseCompiler) getRawPlaceholder(replace string) string {
	return "@__raw_block_" + replace + "__@"
}

var rawPlaceholderPattern = regexp.MustCompile(`@__raw_block_(\d+)__@`)

// restoreRawContent is BladeCompiler::restoreRawContent.
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

// addFooters is BladeCompiler::addFooters.
func (c *KyseCompiler) addFooters(result string) string {
	reversed := make([]string, 0, len(c.footer))
	for i := len(c.footer) - 1; i >= 0; i-- {
		reversed = append(reversed, c.footer[i])
	}
	return strings.TrimRight(result, "\n") + "\n\n" + strings.Join(reversed, "\n")
}

// compileExtensions is BladeCompiler::compileExtensions.
func (c *KyseCompiler) compileExtensions(value string) string {
	for _, compiler := range c.extensions {
		value = compiler(value)
	}
	return value
}

// compileStatements is BladeCompiler::compileStatements.
//
// PHP matches directives with a recursive regular expression, which RE2 has
// no equivalent for and will not grow one. The scan below is that regex read
// out loud: find an @, take the name, and if a parenthesis follows, walk to
// its balanced close counting quotes on the way.
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
			// A directive this compiler does not own belongs to the kyse front
			// end. It goes through untouched rather than being mangled.
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

// kebab is Str::kebab, kept local so this package stays on the standard
// library.
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

// classBasename is class_basename: the last backslash-separated segment.
func classBasename(class string) string { return lastSegment(class, "\\") }

func lastSegment(value, separator string) string {
	if i := strings.LastIndex(value, separator); i >= 0 {
		return value[i+len(separator):]
	}
	return value
}
