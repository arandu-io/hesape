package components

import (
	"regexp"
	"strings"
)

// Mutator rewrites the text of a component before it is rendered.
type Mutator func(string) string

// dynamicContent is the [something] a message highlights.
var dynamicContent = regexp.MustCompile(`\[([^\]]+)\]`)

// EnsureDynamicContentIsHighlighted emboldens every [bracketed] run.
//
// The escape sequence is written here rather than a markup tag, because
// there is no formatter between this and the terminal.
func EnsureDynamicContentIsHighlighted(s string) string {
	return dynamicContent.ReplaceAllString(s, ansiBold+"[$1]"+ansiReset)
}

// EnsureNoPunctuation drops a trailing . ? ! or : .
//
// It is what keeps a task description from reading "migrating the database.
// ....... DONE".
func EnsureNoPunctuation(s string) string {
	if endsWithPunctuation(s) {
		return s[:len(s)-1]
	}
	return s
}

// EnsurePunctuation adds a full stop when there is none.
//
// A line component is a sentence, and a sentence ends. The empty string is
// the exception: it returns empty rather than a bare full stop.
func EnsurePunctuation(s string) string {
	if s == "" || endsWithPunctuation(s) {
		return s
	}
	return s + "."
}

// EnsureRelativePaths strips the application root from every path in the
// text, curried over the root.
//
// The root is given here rather than read from a global, because a
// component that reaches for a global to shorten a path is a component no
// test can pin. Base is set by the Factory.
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
// mutateAll is the same thing over every element of a slice.
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
