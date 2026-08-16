package console

// verbosityMap is the words a command may pass instead of a constant.
var verbosityMap = map[string]Verbosity{
	"v":      VerbosityVerbose,
	"vv":     VerbosityVeryVerbose,
	"vvv":    VerbosityDebug,
	"quiet":  VerbosityQuiet,
	"normal": VerbosityNormal,
}

// ParseVerbosity reads a level written as a word.
//
// An unknown word falls back to the given current level, because a typo in a
// log call must not silence the command.
func ParseVerbosity(level string, current Verbosity) Verbosity {
	if v, found := verbosityMap[level]; found {
		return v
	}
	return current
}
