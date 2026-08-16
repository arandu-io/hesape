// Package attributes holds metadata a command applies to itself.
//
// Go has no attributes and no reflection over declarations, so metadata that
// would otherwise be read back off a class is instead a value with an Apply
// method: the declaration stays next to the type it describes, and it is
// checked by the compiler rather than read back at some later point.
package attributes

import "github.com/arandu-io/hesape/console"

// Signature is the name, arguments and options of a command, with its aliases.
type Signature struct {
	// Signature is the expression Parse reads: "mail:send {user} {--queue=}".
	Signature string

	// Aliases are the other names the command answers to. Nil leaves whatever
	// the command already had.
	Aliases []string
}

// Apply writes the signature onto a command.
//
// The aliases are set too, but only when this carries some.
func (s Signature) Apply(c *console.Command) {
	c.Signature = s.Signature
	if s.Aliases != nil {
		c.Aliases = s.Aliases
	}
}

// Description is the line a listing prints beside a command name.
type Description struct {
	// Description is the sentence. One line, imperative, no full stop.
	Description string
}

// Apply writes the description onto a command.
func (d Description) Apply(c *console.Command) { c.Description = d.Description }
