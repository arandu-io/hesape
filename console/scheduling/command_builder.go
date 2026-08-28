package scheduling

import (
	"fmt"
	"os"
	"strings"
)

// CommandBuilder turns the command line one event carries into the program and
// arguments it is started with.
//
// It is a type with one method and no state, kept as a name because splitting a
// line and putting it behind sudo are two separate decisions and each is worth
// reading on its own.
type CommandBuilder struct{}

// BuildCommand renders the program and its arguments.
//
// Nothing here builds a line for a shell. The event's command line is split into
// words, and every character a shell would act on -- a semicolon, an ampersand,
// a pipe, a redirection, a dollar sign, a backquote, an asterisk -- is an
// ordinary character of the word it appears in. That is the whole difference
// between this and a shell, and it is the difference between an argument and a
// second command.
//
// A command line of nothing but spaces builds nothing, which is what an event
// with no command means.
func (b CommandBuilder) BuildCommand(event *Event) ([]string, error) {
	command, err := splitCommand(event.Command)
	if err != nil {
		return nil, err
	}
	if len(command) == 0 {
		return nil, nil
	}
	return b.ensureCorrectUser(event, command), nil
}

// ensureCorrectUser puts the command behind sudo, and only where there is one.
//
// The command follows the -- as arguments, so sudo runs the program itself and
// there is no second line for anything to parse. Wrapping it in a shell instead
// would put a shell back in the path of a line assembled from an event's
// configuration, which is the one thing this package does not do.
func (b CommandBuilder) ensureCorrectUser(event *Event, command []string) []string {
	if event.user == "" || os.PathSeparator == '\\' {
		return command
	}
	return append([]string{"sudo", "-u", event.user, "--"}, command...)
}

// splitCommand splits a command line into the program and its arguments.
//
// It is the inverse of escapeArgument and nothing more. Whitespace outside
// quotes separates words; a single-quoted run is taken literally; inside double
// quotes a doubled quote and a backslashed one both stand for a quote, because
// escapeArgument writes the first on Windows and the second everywhere else; and
// a backslash outside quotes escapes whatever follows it.
//
// Every other character is itself. There is no expansion, no substitution, no
// globbing and no operator: a semicolon is a semicolon.
//
// A quote that is never closed is an error rather than a word that swallows the
// rest of the line. A command line that cannot be read is a command that must
// not run.
func splitCommand(line string) ([]string, error) {
	const (
		bare = iota
		single
		double
	)

	var (
		words []string
		word  strings.Builder
		state = bare
		// begun says a word has been started, so that '' and "" are an empty
		// argument rather than no argument at all.
		begun bool
	)

	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]

		switch state {
		case single:
			if r == '\'' {
				state = bare
				continue
			}
			word.WriteRune(r)

		case double:
			switch {
			case r == '"' && i+1 < len(runes) && runes[i+1] == '"':
				word.WriteRune('"')
				i++
			case r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
				word.WriteRune(runes[i+1])
				i++
			case r == '"':
				state = bare
			default:
				word.WriteRune(r)
			}

		default:
			switch {
			case r == ' ' || r == '\t' || r == '\n' || r == '\r':
				if begun {
					words = append(words, word.String())
					word.Reset()
					begun = false
				}
			case r == '\'':
				state, begun = single, true
			case r == '"':
				state, begun = double, true
			case r == '\\' && i+1 < len(runes):
				word.WriteRune(runes[i+1])
				i++
				begun = true
			default:
				word.WriteRune(r)
				begun = true
			}
		}
	}

	if state != bare {
		return nil, fmt.Errorf("scheduling: the command line %q ends inside a quoted word", line)
	}
	if begun {
		words = append(words, word.String())
	}
	return words, nil
}

// escapeArgument quotes a value so it survives being written into a command line
// and read back out of it by splitCommand.
//
// A path with a space in it is the ordinary case, and a path with a quote in it
// is the one this exists for. The two are a pair: a value that goes through this
// and back through splitCommand is the value it started as.
func escapeArgument(value string) string {
	if value == "" {
		return `""`
	}
	if os.PathSeparator == '\\' {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
