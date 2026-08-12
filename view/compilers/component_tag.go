package compilers

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrComponentNotFound is returned by ComponentClass when neither an alias, a
// namespace, a guessed class nor a view answers for the tag. It is the
// InvalidArgumentException PHP throws.
var ErrComponentNotFound = errors.New("view/compilers: unable to locate a class or view for component")

// ComponentTagCompiler mirrors Illuminate\View\Compilers\ComponentTagCompiler.
//
// It is the precompile step that turns <x-alert type="error"> into the
// directives the main compiler understands. PHP resolves the class behind a
// tag by asking the container and the autoloader whether a class exists; Go
// links its types at build time and has no class_exists, so the two lookups
// are hooks the caller fills: ClassExists and ViewExists. The kyse generator
// fills them from the set of components it has actually seen, which is the
// same question asked at the only time Go can answer it.
type ComponentTagCompiler struct {
	// aliases maps a tag name to a component class.
	aliases map[string]string

	// namespaces maps a tag prefix to a component namespace.
	namespaces map[string]string

	// compiler is the compiler whose aliases and namespaces this one reads.
	compiler *KyseCompiler

	// ClassExists reports whether a component type is known to the build.
	// It stands in for PHP's class_exists.
	ClassExists func(class string) bool

	// ViewExists reports whether a view of that name is registered. It stands
	// in for $viewFactory->exists.
	ViewExists func(name string) bool

	// Namespace is the application namespace GuessClassName prefixes with.
	// PHP reads it off the Application out of the container.
	Namespace string

	// ComponentParameters returns the constructor parameter names of a
	// component class. PHP reflects the constructor; Go has no type by name at
	// run time, so the generator hands the list over.
	ComponentParameters func(class string) []string
}

// NewComponentTagCompiler is ComponentTagCompiler::__construct.
func NewComponentTagCompiler(aliases, namespaces map[string]string, compiler *KyseCompiler) *ComponentTagCompiler {
	if aliases == nil {
		aliases = map[string]string{}
	}
	if namespaces == nil {
		namespaces = map[string]string{}
	}
	return &ComponentTagCompiler{
		aliases:    aliases,
		namespaces: namespaces,
		compiler:   compiler,
		Namespace:  "App\\",
	}
}

// Compile is ComponentTagCompiler::compile.
func (c *ComponentTagCompiler) Compile(value string) string {
	value = c.CompileSlots(value)
	return c.CompileTags(value)
}

// CompileTags is ComponentTagCompiler::compileTags.
func (c *ComponentTagCompiler) CompileTags(value string) string {
	value = c.compileSelfClosingTags(value)
	value = c.compileOpeningTags(value)
	return c.compileClosingTags(value)
}

var (
	selfClosingTagPattern = regexp.MustCompile(`<\s*x[-:]([\w\-:.]*)\s*((?:\s[^>]*?)?)/>`)
	openingTagPattern     = regexp.MustCompile(`<\s*x[-:]([\w\-:.]*)\s*((?:\s[^>]*?)?)(?:\s*)>`)
	closingTagPattern     = regexp.MustCompile(`</\s*x[-:][\w\-:.]*\s*>`)
	slotOpenPattern       = regexp.MustCompile(`<\s*x[-:]slot(?::([\w-]+))?\s*((?:\s[^>]*?)?)(/)?>`)
	slotClosePattern      = regexp.MustCompile(`</\s*x[-:]slot[^>]*>`)
	attributePattern      = regexp.MustCompile(`([\w\-:.@]+)(?:=("[^"]*"|'[^']*'|[^'"=<>\s]+))?`)
)

// compileSelfClosingTags is ComponentTagCompiler::compileSelfClosingTags.
func (c *ComponentTagCompiler) compileSelfClosingTags(value string) string {
	return selfClosingTagPattern.ReplaceAllStringFunc(value, func(match string) string {
		groups := selfClosingTagPattern.FindStringSubmatch(match)
		return c.componentString(groups[1], groups[2]) + "\n@endComponentClass##END-COMPONENT-CLASS##"
	})
}

// compileOpeningTags is ComponentTagCompiler::compileOpeningTags.
func (c *ComponentTagCompiler) compileOpeningTags(value string) string {
	return openingTagPattern.ReplaceAllStringFunc(value, func(match string) string {
		groups := openingTagPattern.FindStringSubmatch(match)
		if strings.HasPrefix(groups[1], "slot") {
			return match
		}
		return c.componentString(groups[1], groups[2])
	})
}

// compileClosingTags is ComponentTagCompiler::compileClosingTags.
func (c *ComponentTagCompiler) compileClosingTags(value string) string {
	return closingTagPattern.ReplaceAllString(value, " @endComponentClass##END-COMPONENT-CLASS##")
}

// componentString is ComponentTagCompiler::componentString.
func (c *ComponentTagCompiler) componentString(name, attributeString string) string {
	class, err := c.ComponentClass(name)
	if err != nil {
		class = name
	}
	attributes := c.getAttributesFromAttributeString(attributeString)
	data, rest := c.PartitionDataAndAttributes(class, attributes)

	return fmt.Sprintf(
		"##BEGIN-COMPONENT-CLASS##@componentClass(%q, %q, %s, %s)",
		class, name, mapLiteral(data), mapLiteral(rest),
	)
}

// getAttributesFromAttributeString is
// ComponentTagCompiler::getAttributesFromAttributeString.
func (c *ComponentTagCompiler) getAttributesFromAttributeString(attributeString string) map[string]string {
	attributes := map[string]string{}
	for _, groups := range attributePattern.FindAllStringSubmatch(attributeString, -1) {
		name := groups[1]
		value := c.StripQuotes(groups[2])
		if groups[2] == "" {
			value = "true"
		}
		attributes[name] = value
	}
	return attributes
}

// ComponentClass is ComponentTagCompiler::componentClass.
//
// It returns (string, error) where PHP throws InvalidArgumentException.
func (c *ComponentTagCompiler) ComponentClass(component string) (string, error) {
	if alias, ok := c.aliases[component]; ok {
		if c.classExists(alias) || c.viewExists(alias) {
			return alias, nil
		}
		return "", fmt.Errorf("%w: alias %s for component %s", ErrComponentNotFound, alias, component)
	}

	if class := c.FindClassByComponent(component); class != "" {
		return class, nil
	}

	if class := c.GuessClassName(component); c.classExists(class) {
		return class, nil
	}

	if view := c.GuessViewName(component, "components."); c.viewExists(view) {
		return view, nil
	}

	if strings.HasPrefix(component, "mail::") {
		return component, nil
	}

	return "", fmt.Errorf("%w: %s", ErrComponentNotFound, component)
}

// FindClassByComponent is ComponentTagCompiler::findClassByComponent.
//
// It returns "" where PHP returns null.
func (c *ComponentTagCompiler) FindClassByComponent(component string) string {
	segments := strings.SplitN(component, "::", 2)
	if len(segments) < 2 {
		return ""
	}
	namespace, ok := c.namespaces[segments[0]]
	if !ok {
		return ""
	}
	class := namespace + "\\" + c.FormatClassName(segments[1])
	if c.classExists(class) {
		return class
	}
	return ""
}

// GuessClassName is ComponentTagCompiler::guessClassName.
func (c *ComponentTagCompiler) GuessClassName(component string) string {
	return c.Namespace + "View\\Components\\" + c.FormatClassName(component)
}

// FormatClassName is ComponentTagCompiler::formatClassName.
func (c *ComponentTagCompiler) FormatClassName(component string) string {
	pieces := strings.Split(component, ".")
	for i, piece := range pieces {
		pieces[i] = ucfirst(camel(piece))
	}
	return strings.Join(pieces, "\\")
}

// GuessViewName is ComponentTagCompiler::guessViewName.
//
// An empty prefix means the PHP default, "components.".
func (c *ComponentTagCompiler) GuessViewName(name, prefix string) string {
	if prefix == "" {
		prefix = "components."
	}
	if !strings.HasSuffix(prefix, ".") {
		prefix += "."
	}
	// ViewFinderInterface::HINT_PATH_DELIMITER.
	const delimiter = "::"
	if strings.Contains(name, delimiter) {
		return strings.Replace(name, delimiter, delimiter+prefix, 1)
	}
	return prefix + name
}

// PartitionDataAndAttributes is
// ComponentTagCompiler::partitionDataAndAttributes.
//
// It returns the two halves as separate values where PHP returns a two-element
// array. A class the build does not know about yields both halves whole, which
// is the class-less component case PHP documents.
func (c *ComponentTagCompiler) PartitionDataAndAttributes(class string, attributes map[string]string) (data, rest map[string]string) {
	if c.ComponentParameters == nil || !c.classExists(class) {
		return attributes, attributes
	}

	parameters := map[string]bool{}
	for _, name := range c.ComponentParameters(class) {
		parameters[name] = true
	}

	data, rest = map[string]string{}, map[string]string{}
	for key, value := range attributes {
		if parameters[camel(key)] {
			data[key] = value
		} else {
			rest[key] = value
		}
	}
	return data, rest
}

// CompileSlots is ComponentTagCompiler::compileSlots.
func (c *ComponentTagCompiler) CompileSlots(value string) string {
	value = slotOpenPattern.ReplaceAllStringFunc(value, func(match string) string {
		groups := slotOpenPattern.FindStringSubmatch(match)
		name := groups[1]
		attributes := c.getAttributesFromAttributeString(groups[2])
		if name == "" {
			name = c.StripQuotes(attributes["name"])
			delete(attributes, "name")
		}
		open := fmt.Sprintf("@slot(%q, nil, %s)", name, mapLiteral(attributes))
		if groups[3] == "/" {
			return open + " @endslot"
		}
		return open
	})
	return slotClosePattern.ReplaceAllString(value, "@endslot")
}

// StripQuotes is ComponentTagCompiler::stripQuotes.
func (c *ComponentTagCompiler) StripQuotes(value string) string {
	if len(value) >= 2 && (strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`)) {
		return value[1 : len(value)-1]
	}
	return value
}

func (c *ComponentTagCompiler) classExists(class string) bool {
	return c.ClassExists != nil && c.ClassExists(class)
}

func (c *ComponentTagCompiler) viewExists(name string) bool {
	return c.ViewExists != nil && c.ViewExists(name)
}

// mapLiteral renders a string map as the Go composite literal the compiled
// view carries. Keys are sorted so that two builds of the same source produce
// the same bytes.
func mapLiteral(values map[string]string) string {
	if len(values) == 0 {
		return "nil"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)

	var out strings.Builder
	out.WriteString("map[string]any{")
	for i, key := range keys {
		if i > 0 {
			out.WriteString(", ")
		}
		fmt.Fprintf(&out, "%q: %q", key, values[key])
	}
	out.WriteString("}")
	return out.String()
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// camel is Str::camel.
func camel(value string) string {
	var out strings.Builder
	upper := false
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch == '-' || ch == '_' || ch == ' ' {
			upper = true
			continue
		}
		if upper {
			out.WriteByte(upperByte(ch))
			upper = false
			continue
		}
		if i == 0 {
			out.WriteByte(lowerByte(ch))
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func ucfirst(value string) string {
	if value == "" {
		return value
	}
	return string(upperByte(value[0])) + value[1:]
}

func upperByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - ('a' - 'A')
	}
	return b
}

func lowerByte(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
