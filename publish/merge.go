package publish

import (
	"bytes"
	"regexp"
	"strings"
)

// The expressions that match the region a republication carries forward.
//
// The marker is written in the syntax of whatever the file is, because a
// comment is not portable between the two: "//" below the package clause of a
// .kyse.go is markup, and would be printed to the reader of the page.
//
// Two expressions, chosen by the file, and never both against one file. A
// single alternation would happily pair a Go begin with a view end -- RE2 has
// no backreference to stop it -- and the region between them would be whatever
// happened to be in the middle.
var (
	goBlock   = regexp.MustCompile(`(?s)// arandu:begin custom\n(.*?)// arandu:end custom`)
	viewBlock = regexp.MustCompile(`(?s)\{\{-- arandu:begin custom --\}\}\n(.*?)\{\{-- arandu:end custom --\}\}`)
)

// markerFor picks the expression that matches the markers a file carries.
//
// The choice is by the syntax the file is written in and not by what the file
// is for: a view carries its block in view comments because a Go comment below
// the package clause of one is text on the page.
func markerFor(path string) *regexp.Regexp {
	if strings.HasSuffix(path, ".kyse.go") {
		return viewBlock
	}
	return goBlock
}

// Merge carries the custom blocks of the file on disk into the one about to
// replace it, in order.
//
// This is the escape hatch that makes republishing possible at all: what does
// not fit the standard shape is written between the markers and survives, and
// without it the mechanism is a one-time tool, because nobody runs a command
// twice after it has eaten their work once.
//
// Blocks are matched by position and not by name, which is the honest
// limitation: reordering the generated file would shuffle them. A template puts
// one block per file, at the end, where a new one is appended rather than
// inserted.
func Merge(path string, existing, generated []byte) []byte {
	marker := markerFor(path)
	old := marker.FindAllSubmatch(existing, -1)
	if len(old) == 0 {
		return generated
	}

	i := 0
	return marker.ReplaceAllFunc(generated, func(match []byte) []byte {
		if i >= len(old) {
			return match
		}
		// The markers are reused verbatim out of the match rather than written
		// again here, so neither syntax needs a second literal in this file to
		// drift from the expressions above.
		body := old[i][1]
		i++
		return concat(head(match), body, tail(match))
	})
}

// canonical is the file with the body of every custom block emptied.
//
// It is what a publication is compared against, and the whole of why a custom
// block can be edited without the next publication calling it a conflict:
// everything outside the markers is the mechanism's to write, everything inside
// belongs to whoever wrote it, and only the first half is compared.
func canonical(path string, content []byte) []byte {
	marker := markerFor(path)
	return marker.ReplaceAllFunc(content, func(match []byte) []byte {
		return concat(head(match), tail(match))
	})
}

// head is the opening marker line of a match, newline included.
func head(match []byte) []byte { return match[:bytes.IndexByte(match, '\n')+1] }

// tail is the closing marker, whichever syntax it is written in: the last line
// of the match, from the start of the line the words sit on.
func tail(match []byte) []byte {
	at := bytes.LastIndex(match, []byte("arandu:end custom"))
	for at > 0 && match[at-1] != '\n' {
		at--
	}
	return match[at:]
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
