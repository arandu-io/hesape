package console

import "flag"

// parseInterspersed parses args into fs wherever the flags appear, and returns
// the positional arguments in the order they were given.
//
// flag.FlagSet.Parse stops at the first argument that is not a flag, so
// `queue:retry j-1 --tenant acme` would leave --tenant unread and refuse the
// command for want of one -- while the same words in the other order run. An
// operator copying an id out of a list types it first, and a command that
// answers "--tenant is required" to a line that carries it is a command that
// is lying about what it was given.
//
// "--" still ends the flags: everything after it is positional, so an id that
// begins with a dash can be passed.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// Parse stopped either at a positional argument, which it leaves in
		// place, or after consuming a "--", in which case what is left is
		// positional whatever it looks like.
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}
