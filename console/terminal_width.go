package console

import (
	"os"
	"strconv"
)

// defaultTerminalWidth is what a run with no terminal fills.
//
// It is fixed rather than unbounded so a run piped into a file and a run on a
// terminal that happens to be eighty columns wide produce the same bytes, which
// is what makes the output of two runs comparable.
const defaultTerminalWidth = 80

// terminalWidthResolver is what ResolveTerminalWidthUsing installed, if
// anything.
var terminalWidthResolver func() int

// ResolveTerminalWidthUsing tells the package how wide the terminal is.
//
// It answers resolveTerminalWidthUsing. It exists for the test that has to pin
// the width: a two column line and a task line both size themselves to it, and
// a test whose assertions move with the window it was run in is a test nobody
// trusts. Passing nil puts the default back.
func ResolveTerminalWidthUsing(resolver func() int) { terminalWidthResolver = resolver }

// GetTerminalWidth is how many columns there are to fill.
//
// It answers getTerminalWidth. The resolver wins when one is installed;
// otherwise it is COLUMNS, which is what a shell exports and what a CI runner
// sets, and the default when that is missing or nonsense.
func (o *IO) GetTerminalWidth() int {
	if terminalWidthResolver != nil {
		if width := terminalWidthResolver(); width > 0 {
			return width
		}
	}
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width > 0 {
		return width
	}
	return defaultTerminalWidth
}
