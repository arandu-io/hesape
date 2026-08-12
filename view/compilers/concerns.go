package compilers

import (
	"fmt"
	"html"
	"reflect"
	"regexp"
	"strings"
)

// This file answers Illuminate\View\Compilers\Concerns.
//
// PHP composes twenty-one traits into BladeCompiler. Go has no traits, so the
// trait methods are methods on the compiler, which is where the trait put them
// anyway: `$this` inside CompilesEchos is the BladeCompiler. The names are
// unchanged.

// escapedEchoPattern answers the {{{ }}} tags: the legacy escaped echo.
var escapedEchoPattern = regexp.MustCompile(`(?s)\{\{\{\s*(.+?)\s*\}\}\}`)

// rawEchoPattern answers the {!! !!} tags.
var rawEchoPattern = regexp.MustCompile(`(?s)\{!!\s*(.+?)\s*!!\}`)

// regularEchoPattern answers the {{ }} tags.
var regularEchoPattern = regexp.MustCompile(`(?s)\{\{\s*(.+?)\s*\}\}`)

// CompileEchos is Concerns\CompilesEchos::compileEchos.
//
// The order is PHP's -- raw, then escaped, then regular -- because {{{ }}} and
// {!! !!} both contain a {{ }} and matching the regular tag first would eat
// them.
func (c *KyseCompiler) CompileEchos(value string) string {
	value = c.compileRawEchos(value)
	value = c.compileEscapedEchos(value)
	return c.compileRegularEchos(value)
}

// compileRawEchos is Concerns\CompilesEchos::compileRawEchos.
func (c *KyseCompiler) compileRawEchos(value string) string {
	return rawEchoPattern.ReplaceAllStringFunc(value, func(match string) string {
		return "Raw(" + rawEchoPattern.FindStringSubmatch(match)[1] + ")"
	})
}

// compileEscapedEchos is Concerns\CompilesEchos::compileEscapedEchos.
func (c *KyseCompiler) compileEscapedEchos(value string) string {
	return escapedEchoPattern.ReplaceAllStringFunc(value, func(match string) string {
		return "Text(" + escapedEchoPattern.FindStringSubmatch(match)[1] + ")"
	})
}

// compileRegularEchos is Concerns\CompilesEchos::compileRegularEchos.
func (c *KyseCompiler) compileRegularEchos(value string) string {
	return regularEchoPattern.ReplaceAllStringFunc(value, func(match string) string {
		expression := regularEchoPattern.FindStringSubmatch(match)[1]
		if strings.Contains(c.echoFormat, "%s") {
			return fmt.Sprintf(c.echoFormat, expression)
		}
		return c.echoFormat
	})
}

// Stringable is Concerns\CompilesEchos::stringable.
//
// PHP reads the handled type off the closure's first parameter when only a
// closure is given; Go has no closure reflection worth the trouble, so the
// type name is the argument it always was in the two-argument form. The name
// is what reflect reports for the value, or "iterable" for the catch-all PHP
// spells the same way.
func (c *KyseCompiler) Stringable(class string, handler func(any) any) {
	c.echoHandlers[class] = handler
}

// ApplyEchoHandler is Concerns\CompilesEchos::applyEchoHandler.
func (c *KyseCompiler) ApplyEchoHandler(value any) any {
	if value == nil {
		return value
	}
	if handler, ok := c.echoHandlers[reflect.TypeOf(value).String()]; ok {
		return handler(value)
	}
	if handler, ok := c.echoHandlers["iterable"]; ok && isIterable(value) {
		return handler(value)
	}
	return value
}

func isIterable(value any) bool {
	switch reflect.TypeOf(value).Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return true
	default:
		return false
	}
}

// CompileClassComponentOpening is
// Concerns\CompilesComponents::compileClassComponentOpening.
//
// PHP emits eight lines of PHP that save $component and $attributes, resolve
// the class and start the component. The Go it emits saves and restores the
// same two, because a nested component that clobbers the outer one is the bug
// those lines exist to prevent.
func CompileClassComponentOpening(component, alias, data, hash string) string {
	if data == "" {
		data = "nil"
	}
	return strings.Join([]string{
		"__componentOriginal" + hash + " := __component",
		"__attributesOriginal" + hash + " := __attributes",
		"__component = " + component + ".Resolve(" + data + ")",
		"__component.WithName(" + alias + ")",
		"if __component.ShouldRender() {",
		"__env.StartComponent(view.ResolveView(__component), __component.Data())",
	}, "\n")
}

// CompileEndComponentClass is
// Concerns\CompilesComponents::compileEndComponentClass.
//
// PHP pops the hash off a static stack; the hash is an argument here, because
// a package-level stack shared by every compiler in the process is state that
// two concurrent builds would corrupt.
func CompileEndComponentClass(hash string) string {
	return strings.Join([]string{
		"__env.RenderComponent()",
		"}",
		"__attributes = __attributesOriginal" + hash,
		"__component = __componentOriginal" + hash,
	}, "\n")
}

// CompileEndOnce is Concerns\CompilesConditionals::compileEndOnce.
func CompileEndOnce() string { return "}" }

// SanitizeComponentAttribute is
// Concerns\CompilesComponents::sanitizeComponentAttribute.
//
// A string is escaped; a value that renders its own HTML -- the attribute bag,
// anything with ToHTML -- is left alone, because escaping it twice is how the
// markup ends up on the page as text.
func SanitizeComponentAttribute(value any) any {
	if value == nil {
		return value
	}
	switch v := value.(type) {
	case interface{ EscapeWhenCastingToString() any }:
		return v.EscapeWhenCastingToString()
	case interface{ ToHTML() string }:
		return value
	case string:
		return html.EscapeString(v)
	case fmt.Stringer:
		return html.EscapeString(v.String())
	default:
		return value
	}
}
