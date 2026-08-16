package compilers

import (
	"fmt"
	"html"
	"reflect"
	"regexp"
	"strings"
)

// escapedEchoPattern answers the {{{ }}} tags: the legacy escaped echo.
var escapedEchoPattern = regexp.MustCompile(`(?s)\{\{\{\s*(.+?)\s*\}\}\}`)

// rawEchoPattern answers the {!! !!} tags.
var rawEchoPattern = regexp.MustCompile(`(?s)\{!!\s*(.+?)\s*!!\}`)

// regularEchoPattern answers the {{ }} tags.
var regularEchoPattern = regexp.MustCompile(`(?s)\{\{\s*(.+?)\s*\}\}`)

// CompileEchos expands {!! !!}, {{{ }}} and {{ }} echo tags, in that order:
// raw first, then escaped, then regular, because {{{ }}} and {!! !!} both
// contain a {{ }} and matching the regular tag first would eat them.
func (c *KyseCompiler) CompileEchos(value string) string {
	value = c.compileRawEchos(value)
	value = c.compileEscapedEchos(value)
	return c.compileRegularEchos(value)
}

// compileRawEchos expands {!! !!} tags into Raw(...) calls.
func (c *KyseCompiler) compileRawEchos(value string) string {
	return rawEchoPattern.ReplaceAllStringFunc(value, func(match string) string {
		return "Raw(" + rawEchoPattern.FindStringSubmatch(match)[1] + ")"
	})
}

// compileEscapedEchos expands {{{ }}} tags into Text(...) calls.
func (c *KyseCompiler) compileEscapedEchos(value string) string {
	return escapedEchoPattern.ReplaceAllStringFunc(value, func(match string) string {
		return "Text(" + escapedEchoPattern.FindStringSubmatch(match)[1] + ")"
	})
}

// compileRegularEchos expands {{ }} tags using the configured echo format.
func (c *KyseCompiler) compileRegularEchos(value string) string {
	return regularEchoPattern.ReplaceAllStringFunc(value, func(match string) string {
		expression := regularEchoPattern.FindStringSubmatch(match)[1]
		if strings.Contains(c.echoFormat, "%s") {
			return fmt.Sprintf(c.echoFormat, expression)
		}
		return c.echoFormat
	})
}

// Stringable registers a handler for values of the given type name.
//
// Go has no closure reflection worth the trouble, so the type name is always
// an explicit argument rather than inferred from a callback's signature. The
// name is what reflect reports for the value, or "iterable" for the
// catch-all case.
func (c *KyseCompiler) Stringable(class string, handler func(any) any) {
	c.echoHandlers[class] = handler
}

// ApplyEchoHandler runs the handler registered for value's type, or the
// "iterable" handler if value is a slice, array or map, or returns value
// unchanged.
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

// CompileClassComponentOpening returns the Go that opens a component block:
// it saves and restores __component and __attributes, because a nested
// component that clobbers the outer one is the bug those lines exist to
// prevent.
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

// CompileEndComponentClass returns the Go that closes a component block
// opened by CompileClassComponentOpening.
//
// hash is an argument rather than popped off a shared stack, because a
// package-level stack shared by every compiler in the process is state that
// two concurrent builds would corrupt.
func CompileEndComponentClass(hash string) string {
	return strings.Join([]string{
		"__env.RenderComponent()",
		"}",
		"__attributes = __attributesOriginal" + hash,
		"__component = __componentOriginal" + hash,
	}, "\n")
}

// CompileEndOnce returns the Go that closes an @once block.
func CompileEndOnce() string { return "}" }

// SanitizeComponentAttribute escapes value if it is a string, and leaves it
// alone if it already renders its own HTML -- the attribute bag, anything
// with ToHTML -- because escaping it twice is how the markup ends up on the
// page as text.
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
