package components

import (
	"regexp"
	"strings"
)

// Mutator rewrites the text of a component before it is rendered.
//
// It answers the classes in View/Components/Mutators, which PHP invokes with
// __invoke and passes as a class-string list. A func is the same thing with the
// language's own spelling.
type Mutator func(string) string

// dynamicContent is the [something] a message highlights.
var dynamicContent = regexp.MustCompile(`\[([^\]]+)\]`)

// EnsureDynamicContentIsHighlighted emboldens every [bracketed] run.
//
// It answers Mutators\EnsureDynamicContentIsHighlighted. The escape sequence is
// written here rather than a markup tag, because there is no formatter between
// this and the terminal.
func EnsureDynamicContentIsHighlighted(s string) string {
	return dynamicContent.ReplaceAllString(s, ansiBold+"[$1]"+ansiReset)
}

// EnsureNoPunctuation drops a trailing . ? ! or : .
//
// It answers Mutators\EnsureNoPunctuation, and it is what keeps a task
// description from reading "migrating the database. ....... DONE".
func EnsureNoPunctuation(s string) string {
	if endsWithPunctuation(s) {
		return s[:len(s)-1]
	}
	return s
}

// EnsurePunctuation adds a full stop when there is none.
//
// It answers Mutators\EnsurePunctuation: a line component is a sentence, and a
// sentence ends.
func EnsurePunctuation(s string) string {
	if s == "" || endsWithPunctuation(s) {
		return s
	}
	return s + "."
}

// EnsureRelativePaths strips the application root from every path in the text.
//
// It answers Mutators\EnsureRelativePaths. PHP reads the root from the
// container; here it is given, because a component that reaches for a global to
// shorten a path is a component no test can pin. Base is set by the Factory.
func EnsureRelativePaths(base string) Mutator {
	return func(s string) string {
		if base == "" {
			return s
		}
		return strings.ReplaceAll(s, strings.TrimSuffix(base, "/")+"/", "")
	}
}

// endsWithPunctuation is the shared test of the two punctuation mutators.
func endsWithPunctuation(s string) bool {
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '.', '?', '!', ':':
		return true
	}
	return false
}

// mutate applies every mutator to the text, in order.
//
// It answers Component::mutate for the scalar half of its argument; the
// iterable half is mutateAll.
func mutate(s string, mutators ...Mutator) string {
	for _, m := range mutators {
		s = m(s)
	}
	return s
}

// mutateAll applies every mutator to every element.
func mutateAll(elements []string, mutators ...Mutator) []string {
	out := make([]string, len(elements))
	for i, e := range elements {
		out[i] = mutate(e, mutators...)
	}
	return out
}
