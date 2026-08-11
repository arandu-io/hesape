package support

import "strings"

// processUtilsFacade is Illuminate\Support\ProcessUtils, reached through the
// [ProcessUtils] value because every method of the PHP class is static.
type processUtilsFacade struct{}

// ProcessUtils answers to Illuminate\Support\ProcessUtils, which the PHP copied
// from Symfony 3. support.ProcessUtils.EscapeArgument(...) reads as
// ProcessUtils::escapeArgument.
var ProcessUtils processUtilsFacade

// EscapeArgument answers to ProcessUtils::escapeArgument: a string made safe to
// hand a shell as one argument.
//
// The PHP branches on DIRECTORY_SEPARATOR; this branches on [Windows_os], which
// is the same question. Everywhere but Windows the argument is single quoted
// and every single quote inside it is closed, escaped and reopened, which no
// shell can read as the end of the argument.
func (processUtilsFacade) EscapeArgument(argument string) string {
	if !Windows_os() {
		return "'" + strings.ReplaceAll(argument, "'", `'\''`) + "'"
	}
	if argument == "" {
		return `""`
	}

	escaped := strings.Builder{}
	quote := false
	for _, part := range splitKeepingQuotes(argument) {
		switch {
		case part == `"`:
			escaped.WriteString(`\"`)
		case isSurroundedBy(part, '%'):
			// Avoid environment variable expansion.
			escaped.WriteString(`^%"` + part[1:len(part)-1] + `"^%`)
		default:
			// Escape a trailing backslash, which would otherwise escape the
			// closing quote.
			if strings.HasSuffix(part, `\`) {
				part += `\`
			}
			quote = true
			escaped.WriteString(part)
		}
	}
	if quote {
		return `"` + escaped.String() + `"`
	}
	return escaped.String()
}

// splitKeepingQuotes is preg_split('/(")/', $argument, -1, PREG_SPLIT_NO_EMPTY
// | PREG_SPLIT_DELIM_CAPTURE): the runs between double quotes, with the quotes
// themselves kept as their own parts and nothing empty.
func splitKeepingQuotes(argument string) []string {
	parts := []string{}
	current := strings.Builder{}
	for i := 0; i < len(argument); i++ {
		if argument[i] != '"' {
			current.WriteByte(argument[i])
			continue
		}
		if current.Len() > 0 {
			parts = append(parts, current.String())
			current.Reset()
		}
		parts = append(parts, `"`)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// isSurroundedBy answers to the protected ProcessUtils::isSurroundedBy.
func isSurroundedBy(argument string, char byte) bool {
	return len(argument) > 2 && argument[0] == char && argument[len(argument)-1] == char
}
