// Package attributes mirrors Illuminate\Console\Attributes.
//
// The files it answers to, in the clone at
// laravel_illuminate/console/Attributes:
//
//	Description.php
//	Signature.php
//
// A PHP attribute is metadata written above a class and read back by
// reflection, which is how a command declares its signature without a
// constructor. Go has no attributes and no reflection over declarations, so the
// same two pieces of metadata are values a command applies to itself: the
// declaration stays next to the type it describes, and it is checked by the
// compiler rather than at the moment the class is instantiated.
package attributes

import "github.com/arandu-io/hesape/console"

// Signature is the name, arguments and options of a command, with its aliases.
//
// It answers Attributes\Signature.
type Signature struct {
	// Signature is the expression Parse reads: "mail:send {user} {--queue=}".
	Signature string

	// Aliases are the other names the command answers to. Nil leaves whatever
	// the command already had, which is the ?array default in the PHP.
	Aliases []string
}

// Apply writes the signature onto a command.
//
// It answers what Command::configureFromAttributes does with the attribute it
// found: the signature is set, and the aliases only when the attribute carried
// some.
func (s Signature) Apply(c *console.Command) {
	c.Signature = s.Signature
	if s.Aliases != nil {
		c.Aliases = s.Aliases
	}
}

// Description is the line a listing prints beside a command name.
//
// It answers Attributes\Description.
type Description struct {
	// Description is the sentence. One line, imperative, no full stop.
	Description string
}

// Apply writes the description onto a command.
func (d Description) Apply(c *console.Command) { c.Description = d.Description }
